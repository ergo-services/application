package observer

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"ergo.services/application/observer/access"
	"ergo.services/ergo/gen"
)

// waiting here would hold one HTTP request for as long as the slowest node takes
func (w *postWorker) mcpFanout(writer http.ResponseWriter, request *http.Request,
	envelope mcpEnvelope, tool mcpTool, arguments map[string]any) {

	identity, _ := identityOf(request)
	if identity.Subject == "" {
		w.mcpToolError(writer, request, envelope, errNeedsCaller.Error())
		return
	}

	key, _ := arguments["key"].(string)
	if key == "" {
		w.mcpToolError(writer, request, envelope,
			"key is required: it names the run and makes a repeated call idempotent")
		return
	}
	if err := (mcpURI{Key: key}).validKey(); err != nil {
		w.mcpToolError(writer, request, envelope, err.Error())
		return
	}

	steps, refused, err := w.planSteps(tool.Name, arguments, identity)
	if err != nil {
		w.mcpToolError(writer, request, envelope, err.Error())
		return
	}
	if len(steps) == 0 {
		empty := map[string]any{
			"steps": 0,
			"note":  "no step to run: read " + clusterURI + " for what is reachable",
		}
		if len(refused) > 0 {
			empty["refused"] = refused
		}
		value, _ := json.Marshal(empty)

		writeJSON(writer, request, http.StatusOK, mcpOK(envelope.ID, mcpToolResult{
			ResultType: mcpResultComplete,
			Content:    []any{mcpText(string(value))},
			IsError:    true,
			Meta:       mcpAnswered(),
		}))
		return
	}

	plan := planDigest(tool.Name, arguments)
	result, err := w.CallWithTimeout(managerName, jobEnsureRequest{Spec: jobSpec{
		key:     key,
		subject: qualified(identity),
		steps:   steps,
		plan:    plan,
		refused: refused,
		ceiling: identity.Ceiling,
		retain:  w.retention(arguments),
	}}, mcpReadTimeout)
	if err != nil {
		w.mcpFail(writer, request, envelope.ID, mcpInternalError, err.Error(), nil)
		return
	}

	answer, ok := result.(jobEnsureResponse)
	if ok == false {
		w.mcpFail(writer, request, envelope.ID, mcpInternalError,
			"the run could not be started", nil)
		return
	}
	if answer.Error != "" {
		w.mcpToolError(writer, request, envelope, answer.Error)
		return
	}

	running := len(steps)
	if answer.Started == false {
		held, err := w.joined(key, qualified(identity), plan)
		if err != "" {
			w.mcpToolError(writer, request, envelope, err)
			return
		}
		running = held
	}

	said := map[string]any{
		"uri":     answer.URI,
		"steps":   running,
		"started": answer.Started,
		"read":    "resources/read the uri, and pass since= to take only what is new",
	}
	if len(refused) > 0 {
		said["refused"] = refused
	}
	value, _ := json.Marshal(said)
	writeJSON(writer, request, http.StatusOK, mcpOK(envelope.ID, mcpToolResult{
		ResultType: mcpResultComplete,
		Content:    []any{mcpText(string(value)), mcpLink(uriWordJob, answer.URI)},
		Meta:       mcpAnswered(),
	}))
}

// the same key is a retry only while it asks the same question; asking a different one under a
// key already taken would read answers to somebody else's question
func (w *postWorker) joined(key string, subject string, plan string) (int, string) {
	result, err := w.CallWithTimeout(jobName(key, subject), jobPlanRequest{}, mcpReadTimeout)
	if err != nil {
		return 0, fmt.Sprintf("the run under %q could not be reached: %s", key, err)
	}
	held, ok := result.(jobPlanResponse)
	if ok == false {
		return 0, fmt.Sprintf("the run under %q answered %T", key, result)
	}
	if held.Plan != plan {
		return 0, fmt.Sprintf("the key %q is already running a different question, %s with %d "+
			"steps: read %s for it, or start yours under another key",
			key, held.State, held.Steps, jobURI(key))
	}
	return held.Steps, ""
}

func planDigest(tool string, arguments map[string]any) string {
	asked := map[string]any{"tool": tool}
	for name, value := range arguments {
		if name == "key" || name == "retain" {
			continue
		}
		asked[name] = value
	}
	encoded, err := json.Marshal(asked)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:12])
}

func (w *postWorker) retention(arguments map[string]any) time.Duration {
	ceiling := defaultJobMaxRetention
	if v, exist := w.Env(envJobMaxRetention); exist {
		if configured, ok := v.(time.Duration); ok && configured > 0 {
			ceiling = configured
		}
	}
	return clampRetention(ceiling, arguments)
}

func clampRetention(ceiling time.Duration, arguments map[string]any) time.Duration {
	if ceiling <= 0 {
		ceiling = defaultJobMaxRetention
	}
	asked, _ := arguments["retain"].(float64)
	if asked <= 0 {
		return ceiling
	}
	if requested := time.Duration(asked) * time.Second; requested < ceiling {
		return requested
	}
	return ceiling
}

func (w *postWorker) planSteps(name string, arguments map[string]any, identity Identity) (
	[]jobStep, map[string]string, error) {

	if name == "cluster_batch" {
		steps, err := w.batchSteps(arguments, identity)
		return steps, nil, err
	}
	return w.uniformSteps(arguments, identity)
}

func (w *postWorker) uniformSteps(arguments map[string]any, identity Identity) (
	[]jobStep, map[string]string, error) {

	tool, _ := arguments["tool"].(string)
	if tool == "" {
		return nil, nil, fmt.Errorf("tool is required")
	}

	args := map[string]any{}
	if given, ok := arguments["args"].(map[string]any); ok {
		for k, v := range given {
			args[k] = v
		}
	}
	if err := allowedStep(tool, args, identity); err != nil {
		return nil, nil, err
	}

	nodes, refused, err := w.stepNodes(arguments, identity)
	if err != nil {
		return nil, nil, err
	}

	steps := make([]jobStep, 0, len(nodes))
	for _, node := range nodes {
		stepArgs := map[string]any{nodeArgument: string(node)}
		for k, v := range args {
			stepArgs[k] = v
		}
		steps = append(steps, jobStep{
			ID:   string(node),
			Node: node,
			Tool: tool,
			Args: stepArgs,
		})
	}
	return steps, refused, nil
}

func (w *postWorker) batchSteps(arguments map[string]any, identity Identity) ([]jobStep, error) {
	list, ok := arguments["steps"].([]any)
	if ok == false || len(list) == 0 {
		return nil, fmt.Errorf("steps is required")
	}
	if len(list) > jobStepsMax {
		return nil, fmt.Errorf("a run takes at most %d steps", jobStepsMax)
	}

	steps := make([]jobStep, 0, len(list))
	seen := map[string]bool{}
	for i, entry := range list {
		fields, ok := entry.(map[string]any)
		if ok == false {
			return nil, fmt.Errorf("step %d is not an object", i)
		}

		node, _ := fields["node"].(string)
		tool, _ := fields["tool"].(string)
		if node == "" || tool == "" {
			return nil, fmt.Errorf("step %d needs a node and a tool", i)
		}

		id, _ := fields["id"].(string)
		if id == "" {
			id = fmt.Sprintf("%d", i)
		}
		if seen[id] {
			return nil, fmt.Errorf("step id %q is used twice", id)
		}
		seen[id] = true

		args := map[string]any{nodeArgument: node}
		if given, ok := fields["args"].(map[string]any); ok {
			for k, v := range given {
				args[k] = v
			}
		}

		target := gen.Atom(node)
		if err := allowedStep(tool, args, identity); err != nil {
			return nil, fmt.Errorf("step %q: %w", id, err)
		}
		if err := w.reachable(target, identity); err != nil {
			return nil, fmt.Errorf("step %q: %w", id, err)
		}

		steps = append(steps, jobStep{ID: id, Node: target, Tool: tool, Args: args})
	}
	return steps, nil
}

// answers, all held in memory until the run is read
const jobStepsMax = 256

// refused at submit time or not at all: the workers run when there is nobody to answer to
func allowedStep(name string, args map[string]any, identity Identity) error {
	tool, served := toolByName(name)
	if served == false {
		return fmt.Errorf("there is no tool %q to run", name)
	}
	if tool.Fanout {
		return fmt.Errorf("%s starts a run of its own and cannot be a step", name)
	}
	if tool.servesAction() == false {
		return fmt.Errorf("%s is answered here and cannot be a step", name)
	}

	action, built, err := toolAction(tool, args)
	if err != nil {
		return err
	}
	_, capability, err := buildActionRequest(action, built)
	if err != nil {
		return err
	}
	if access.Mutating(capability) {
		return fmt.Errorf("%s writes, and a run is only for reading", name)
	}
	if identity.Ceiling.Allows(capability) == false {
		return fmt.Errorf("%s is not permitted here", capability)
	}
	return nil
}

func (w *postWorker) reachable(node gen.Atom, identity Identity) error {
	if identity.Ceiling.AllowsNode(node) == false {
		return fmt.Errorf("node %s is not permitted here", string(node))
	}
	if node == w.Node().Name() {
		return nil
	}
	if _, err := w.Node().Network().Node(node); err != nil {
		return fmt.Errorf("node %s is not connected", string(node))
	}
	return nil
}

func (w *postWorker) stepNodes(arguments map[string]any, identity Identity) (
	[]gen.Atom, map[string]string, error) {

	var asked []gen.Atom
	if list, ok := arguments["nodes"].([]any); ok {
		for _, entry := range list {
			if name, ok := entry.(string); ok && name != "" {
				asked = append(asked, gen.Atom(name))
			}
		}
	}

	if len(asked) == 0 {
		result, err := w.CallWithTimeout(gen.ProcessID{Name: clusterName, Node: w.Node().Name()},
			RequestClusterInfo{}, mcpReadTimeout)
		if err != nil {
			return nil, nil, err
		}
		info, ok := result.(ClusterInfo)
		if ok == false {
			return nil, nil, fmt.Errorf("unexpected answer %T", result)
		}
		for _, node := range narrowCluster(info, identity.Ceiling).Nodes {
			asked = append(asked, node.Info.Name)
		}
	}
	if len(asked) > jobStepsMax {
		return nil, nil, fmt.Errorf("a run takes at most %d nodes", jobStepsMax)
	}

	out := make([]gen.Atom, 0, len(asked))
	refused := map[string]string{}
	for _, node := range asked {
		if err := w.reachable(node, identity); err != nil {
			refused[string(node)] = err.Error()
			continue
		}
		out = append(out, node)
	}
	return out, refused, nil
}
