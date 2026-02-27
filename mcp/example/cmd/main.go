package main

import (
	"flag"
	"fmt"
	"os"
	"time"

	"ergo.services/application/mcp"
	"ergo.services/ergo"
	"ergo.services/ergo/act"
	"ergo.services/ergo/gen"
	"ergo.services/ergo/lib"
	"ergo.services/ergo/net/edf"
)

// Example message types for MCP reflection (send_message, call_process tools)
type MessagePing struct {
	Text string `json:"text"`
}

type StatusRequest struct {
	Verbose bool `json:"verbose"`
}

type StatusResponse struct {
	Status string `json:"status"`
	Uptime int64  `json:"uptime"`
}

func init() {
	types := []any{
		MessagePing{},
		StatusRequest{},
		StatusResponse{},
	}
	for _, t := range types {
		err := edf.RegisterTypeOf(t)
		if err == nil || err == gen.ErrTaken {
			continue
		}
		panic(err)
	}
}

func main() {
	httpFlag := flag.Bool("http", false, "start HTTP listener on port 9922")
	flag.Parse()

	prefix := lib.RandomString(4)
	nodeName := gen.Atom(fmt.Sprintf("%s@localhost", prefix))
	cookie := "mcptest"

	var mcpPort uint16 // agent mode by default
	if *httpFlag {
		mcpPort = 9922
	}

	options := gen.NodeOptions{
		Applications: []gen.ApplicationBehavior{
			mcp.CreateApp(mcp.Options{
				Port: mcpPort,
				Host: "localhost",
			}),
		},
	}
	options.Network.Cookie = cookie

	node, err := ergo.StartNode(nodeName, options)
	if err != nil {
		fmt.Printf("failed to start node: %s\n", err)
		os.Exit(1)
	}

	node.Log().Info("Node started: %s (cookie: %s)", node.Name(), cookie)
	if *httpFlag {
		node.Log().Info("MCP HTTP at http://localhost:9922/mcp")
	} else {
		node.Log().Info("MCP agent mode (no HTTP)")
	}

	// Spawn worker processes for inspection
	for i := 0; i < 3; i++ {
		name := gen.Atom(fmt.Sprintf("worker_%d", i))
		if _, err := node.SpawnRegister(name, factoryWorker, gen.ProcessOptions{}); err != nil {
			node.Log().Error("failed to spawn %s: %s", name, err)
		}
	}

	node.Log().Info("Spawned 3 worker processes")
	node.Wait()
}

func factoryWorker() gen.ProcessBehavior {
	return &exampleWorker{}
}

type exampleWorker struct {
	act.Actor
	startedAt time.Time
}

func (w *exampleWorker) Init(args ...any) error {
	w.startedAt = time.Now()

	// worker_0 registers a test event
	if w.Name() == "worker_0" {
		w.Send(w.PID(), "register_event")
	}

	// periodic self-tick to keep the process active
	w.SendAfter(w.PID(), "tick", 5*time.Second)
	return nil
}

func (w *exampleWorker) HandleMessage(from gen.PID, message any) error {
	switch message {
	case "register_event":
		if _, err := w.RegisterEvent("test_event", gen.EventOptions{
			Notify: true,
			Buffer: 5,
		}); err != nil {
			w.Log().Error("failed to register event: %s", err)
		}

	case "tick":
		w.Log().Info("got tick")
		time.Sleep(3 * time.Second)
		w.SendAfter(w.PID(), "tick", 5*time.Second)

	default:
		w.Log().Info("received message: %#v", message)
	}
	return nil
}

func (w *exampleWorker) HandleCall(from gen.PID, ref gen.Ref, request any) (any, error) {
	switch r := request.(type) {
	case StatusRequest:
		uptime := int64(time.Since(w.startedAt).Seconds())
		resp := StatusResponse{Status: "running", Uptime: uptime}
		if r.Verbose {
			w.Log().Info("status request from %s (verbose)", from)
		}
		return resp, nil
	}
	return nil, nil
}

func (w *exampleWorker) HandleInspect(from gen.PID, item ...string) map[string]string {
	return map[string]string{
		"started_at": w.startedAt.Format(time.RFC3339),
		"uptime":     time.Since(w.startedAt).String(),
	}
}

func (w *exampleWorker) Terminate(reason error) {
	w.Log().Info("worker %s terminated: %s", w.Name(), reason)
}
