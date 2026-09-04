package observer

import (
	"fmt"

	"ergo.services/ergo/act"
	"ergo.services/ergo/app/system/inspect"
	"ergo.services/ergo/gen"
)

const (
	sessionPoolSize      int64 = 5
	sessionWorkerTimeout int   = 4
)

type sessionWorkerSpec struct {
	back gen.PID
	id   string
}

type jobAction struct {
	from    gen.PID
	ref     gen.Ref
	action  string
	target  gen.ProcessID
	request any
	ceiling Ceiling
}

type jobCluster struct {
	from   gen.PID
	ref    gen.Ref
	target gen.ProcessID
}

type jobSubscribe struct {
	from    gen.PID
	ref     gen.Ref
	subType string
	handle  string
	target  gen.ProcessID
	request any
}

type subscribeResolved struct {
	from    gen.PID
	ref     gen.Ref
	subType string
	handle  string
	result  any
	failed  string
}

type jobSwitch struct {
	from   gen.PID
	ref    gen.Ref
	node   gen.Atom
	target gen.ProcessID
	args   map[string]any
}

type switchResolved struct {
	from     gen.PID
	ref      gen.Ref
	node     gen.Atom
	creation int64
	failed   string
}

func factory_sessionPool() gen.ProcessBehavior {
	return &sessionPool{}
}

type sessionPool struct {
	act.Pool
}

func (p *sessionPool) Init(args ...any) (act.PoolOptions, error) {
	spec, ok := args[0].(sessionWorkerSpec)
	if ok == false {
		return act.PoolOptions{}, fmt.Errorf("session worker spec expected, got %T", args[0])
	}
	return act.PoolOptions{
		PoolSize:      sessionPoolSize,
		WorkerFactory: factory_sessionWorker,
		WorkerArgs:    []any{spec},
	}, nil
}

func factory_sessionWorker() gen.ProcessBehavior {
	return &sessionWorker{}
}

type sessionWorker struct {
	act.Actor

	spec sessionWorkerSpec
}

func (w *sessionWorker) Init(args ...any) error {
	spec, ok := args[0].(sessionWorkerSpec)
	if ok == false {
		return fmt.Errorf("session worker spec expected, got %T", args[0])
	}
	w.spec = spec
	w.Log().SetLogger("default")
	return nil
}

func (w *sessionWorker) HandleMessage(from gen.PID, message any) error {
	switch job := message.(type) {
	case jobAction:
		w.runAction(job)
	case jobCluster:
		w.runCluster(job)
	case jobSubscribe:
		w.runSubscribe(job)
	case jobSwitch:
		w.runSwitch(job)
	}
	return nil
}

func (w *sessionWorker) runAction(job jobAction) {
	result, err := w.CallWithTimeout(job.target, job.request, sessionWorkerTimeout)
	if err != nil {
		w.answer(job.from, job.ref, apiResponse{Error: fmt.Sprintf("action %s: %s", job.action, err)})
		return
	}
	w.answer(job.from, job.ref, actionResponse(result, job.ceiling))
}

func (w *sessionWorker) runCluster(job jobCluster) {
	result, err := w.CallWithTimeout(job.target, RequestClusterInfo{}, sessionWorkerTimeout)
	if err != nil {
		w.answer(job.from, job.ref, apiResponse{Error: fmt.Sprintf("cluster info: %s", err)})
		return
	}
	info, ok := result.(ClusterInfo)
	if ok == false {
		w.answer(job.from, job.ref, apiResponse{Error: fmt.Sprintf("unexpected response %T", result)})
		return
	}
	w.answer(job.from, job.ref, apiResponse{OK: true, Data: wireClusterFrom(info)})
}

func (w *sessionWorker) runSubscribe(job jobSubscribe) {
	resolved := subscribeResolved{
		from: job.from, ref: job.ref, subType: job.subType, handle: job.handle,
	}
	result, err := w.CallWithTimeout(job.target, job.request, sessionWorkerTimeout)
	if err != nil {
		resolved.failed = fmt.Sprintf("inspect call: %s", err)
	} else {
		resolved.result = result
	}
	w.handBack(resolved, job.from, job.ref)
}

func (w *sessionWorker) runSwitch(job jobSwitch) {
	resolved := switchResolved{from: job.from, ref: job.ref, node: job.node}

	if err := connectTo(w.Node(), job.node, job.args); err != nil {
		resolved.failed = fmt.Sprintf("connect to %s: %s", job.node, err)
		w.handBack(resolved, job.from, job.ref)
		return
	}

	result, err := w.CallWithTimeout(job.target, inspect.RequestInspectNode{}, sessionWorkerTimeout)
	if err != nil {
		resolved.failed = fmt.Sprintf("inspect %s: %s", job.node, err)
		w.handBack(resolved, job.from, job.ref)
		return
	}
	r, ok := result.(inspect.ResponseInspectNode)
	if ok == false {
		resolved.failed = "unexpected response from remote inspect"
		w.handBack(resolved, job.from, job.ref)
		return
	}
	resolved.creation = r.Creation
	w.handBack(resolved, job.from, job.ref)
}

func (w *sessionWorker) handBack(resolved any, from gen.PID, ref gen.Ref) {
	if err := w.Send(w.spec.back, resolved); err != nil {
		w.Log().Error("session worker %s: the session is gone: %s", w.spec.id, err)
		w.answer(from, ref, apiResponse{Error: fmt.Sprintf("session %s is gone", w.spec.id)})
	}
}

func (w *sessionWorker) answer(to gen.PID, ref gen.Ref, response any) {
	if err := w.SendResponse(to, ref, response); err != nil {
		w.Log().Error("session worker %s: reply to %s failed: %s", w.spec.id, to, err)
	}
}
