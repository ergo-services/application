package observer

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"ergo.services/ergo/act"
	"ergo.services/ergo/gen"
)

func factory_job() gen.ProcessBehavior {
	return &job{}
}

func factory_fanoutPool() gen.ProcessBehavior {
	return &fanoutPool{}
}

func factory_fanoutWorker() gen.ProcessBehavior {
	return &fanoutWorker{}
}

const (
	defaultJobMaxRetention = 5 * time.Minute
	defaultJobLimit        = 32

	jobFanoutWorkers = 64

	jobNodeTimeout = 5 // seconds

	jobMinDeadline = 30 * time.Second
)

const (
	jobWorking   = "working"
	jobCompleted = "completed"
	jobFailed    = "failed"
	jobCancelled = "cancelled"
)

type jobStep struct {
	ID   string
	Node gen.Atom
	Tool string
	Args map[string]any
}

type jobPlanRequest struct{}

type jobPlanResponse struct {
	Plan  string
	Steps int
	State string
}

type jobSpec struct {
	key     string
	subject string
	steps   []jobStep
	plan    string

	refused map[string]string

	// travels with the run: an empty one permits everything
	ceiling Ceiling

	retain time.Duration
}

type (
	messageJobStart    struct{}
	messageJobRetire   struct{ generation int64 }
	messageJobDeadline struct{}
	messageJobAsk      struct{ step jobStep }
	messageJobFinished struct {
		Step   jobStep
		Status string
		Value  any
		Error  string
	}
)

type jobResult struct {
	ID     string `json:"id"`
	Node   string `json:"node"`
	Tool   string `json:"tool"`
	Status string `json:"status"`
	Value  any    `json:"value,omitempty"`
	Error  string `json:"error,omitempty"`
}

type job struct {
	act.Actor

	spec  jobSpec
	event gen.Atom
	token gen.Ref
	epoch string

	pool     gen.PID
	state    string
	seq      int64
	at       time.Time
	ring     []jobResult
	answered int
	failed   int

	generation int64
	started    time.Time
	dropped    map[string]int64
}

func (j *job) Init(args ...any) error {
	spec, ok := args[0].(jobSpec)
	if ok == false {
		return fmt.Errorf("job spec expected, got %T", args[0])
	}
	j.spec = spec
	j.state = jobWorking
	j.epoch = epochOf(j.PID())
	j.dropped = make(map[string]int64)
	j.started = time.Now()

	j.Log().SetLogger("default")

	event := jobEventName(spec.key, spec.subject)
	// no Notify: a run does not wait to be watched, and the buffer tells a late follower at
	// once that there is something to read
	token, err := j.RegisterEvent(event, gen.EventOptions{Buffer: 1})
	if err != nil {
		return err
	}
	j.event, j.token = event, token

	return j.Send(j.PID(), messageJobStart{})
}

func jobEventName(key string, subject string) gen.Atom {
	return jobName(key, subject) + "_updates"
}

func jobName(key string, subject string) gen.Atom {
	uri := mcpURI{Lens: uriWordJob, Key: key}
	return uri.ownerName(subject)
}

func (j *job) HandleMessage(from gen.PID, message any) error {
	switch m := message.(type) {
	case messageJobStart:
		return j.start()

	case messageJobFinished:
		j.collect(m)

	case messageJobCancel:
		j.cancel()

	case messageJobDeadline:
		j.expire()

	case messageJobRetire:
		if m.generation != j.generation {
			break
		}
		return gen.TerminateReasonNormal

	default:
		j.dropped["message_unexpected"]++
		j.Log().Warning("job %s: unexpected message %#v", j.spec.key, message)
	}
	return nil
}

func (j *job) HandleCall(from gen.PID, ref gen.Ref, request any) (any, error) {
	if _, asking := request.(jobPlanRequest); asking {
		return jobPlanResponse{Plan: j.spec.plan, Steps: len(j.spec.steps), State: j.state}, nil
	}

	read, ok := request.(ownerReadRequest)
	if ok == false {
		j.dropped["request_unsupported"]++
		return gen.ErrUnsupported, nil
	}
	if j.state != jobWorking {
		j.armRetire()
	}
	return j.readingFor(read.Since), nil
}

func (j *job) start() error {
	if len(j.spec.steps) == 0 {
		j.state = jobFailed
		j.publish()
		j.armRetire()
		return nil
	}

	size := len(j.spec.steps)
	if size > jobFanoutWorkers {
		size = jobFanoutWorkers
	}

	spec := fanoutSpec{back: j.PID(), workers: size, ceiling: j.spec.ceiling}
	pool, err := j.Spawn(factory_fanoutPool, gen.ProcessOptions{LinkParent: true}, spec)
	if err != nil {
		j.state = jobFailed
		j.dropped["pool_spawn_failed"]++
		j.Log().Error("job %s: pool: %s", j.spec.key, err)
		j.publish()
		j.armRetire()
		return nil
	}
	j.pool = pool

	j.SendAfter(j.PID(), messageJobDeadline{}, j.deadline())

	for _, step := range j.spec.steps {
		if err := j.Send(pool, messageJobAsk{step: step}); err != nil {
			j.collect(messageJobFinished{Step: step, Status: "refused", Error: err.Error()})
		}
	}
	j.Log().Info("job %s: %d steps through %d workers, deadline %s",
		j.spec.key, len(j.spec.steps), size, j.deadline())
	return nil
}

func (j *job) expire() {
	if j.state != jobWorking {
		return
	}
	j.state = jobFailed
	j.dropped["deadline"]++
	j.stopPool()
	j.seq++
	j.at = time.Now()
	j.publish()
	j.armRetire()
	j.Log().Error("job %s gave up after %s with %d of %d answered",
		j.spec.key, j.deadline(), j.answered, len(j.spec.steps))
}

func (j *job) deadline() time.Duration {
	waves := (len(j.spec.steps) + jobFanoutWorkers - 1) / jobFanoutWorkers
	if waves < 1 {
		waves = 1
	}
	out := time.Duration(waves) * jobNodeTimeout * time.Second * 2
	if out < jobMinDeadline {
		return jobMinDeadline
	}
	return out
}

func (j *job) cancel() {
	if j.state != jobWorking {
		return
	}
	j.state = jobCancelled
	j.stopPool()
	j.seq++
	j.at = time.Now()
	j.publish()
	j.armRetire()
	j.Log().Info("job %s cancelled with %d of %d answered", j.spec.key, j.answered, len(j.spec.steps))
}

func (j *job) stopPool() {
	if j.pool == (gen.PID{}) {
		return
	}
	j.SendExit(j.pool, gen.TerminateReasonNormal)
	j.pool = gen.PID{}
}

func (j *job) collect(m messageJobFinished) {
	if j.state != jobWorking {
		j.dropped["answer_after_end"]++
		return
	}

	j.answered++
	if m.Status != "ok" {
		j.failed++
	}
	j.seq++
	j.at = time.Now()
	j.ring = append(j.ring, jobResult{
		ID:     m.Step.ID,
		Node:   string(m.Step.Node),
		Tool:   m.Step.Tool,
		Status: m.Status,
		Value:  m.Value,
		Error:  m.Error,
	})

	if j.answered >= len(j.spec.steps) {
		j.state = jobCompleted
		if j.failed == len(j.spec.steps) {
			j.state = jobFailed
		}
		j.stopPool()
		j.armRetire()
	}

	j.publish()
}

func (j *job) publish() {
	if err := j.SendEvent(j.event, j.token, messageOwnerUpdated{
		URI: jobURI(j.spec.key),
		Seq: j.seq,
		At:  j.at,
	}); err != nil {
		j.dropped["publish_failed"]++
	}
}

func jobURI(key string) string {
	return mcpScheme + uriWordJob + "/" + key
}

func (j *job) readingFor(since string) ownerReadResponse {
	out := ownerReadResponse{
		URI:     jobURI(j.spec.key),
		Seq:     j.seq,
		At:      j.at,
		NextSeq: fmt.Sprintf("%s.%d", j.epoch, j.seq),
	}

	from, ok := j.cursor(since)
	if ok == false && since != "" {
		out.Dropped = true
	}

	reading := jobReading{
		jobStatus: jobStatus{
			State:     j.state,
			Steps:     len(j.spec.steps),
			Answered:  j.answered,
			Failed:    j.failed,
			Pending:   len(j.spec.steps) - j.answered,
			Refused:   j.spec.refused,
			RetainSec: int(j.retention().Seconds()),
		},
		// an empty list, never null: nothing new must parse the same way as something new
		Results: []jobResult{},
	}
	for i, result := range j.ring {
		if int64(i)+1 > from {
			reading.Results = append(reading.Results, result)
		}
	}
	out.Value = reading
	return out
}

type jobReading struct {
	jobStatus
	Results []jobResult `json:"results"`
}

func (j *job) cursor(since string) (int64, bool) {
	if since == "" {
		return 0, true
	}
	epoch, seq, found := strings.Cut(since, ".")
	if found == false || epoch != j.epoch {
		return 0, false
	}
	value, err := strconv.ParseInt(seq, 10, 64)
	if err != nil {
		return 0, false
	}
	return value, true
}

type jobStatus struct {
	State string `json:"state"`

	Steps    int `json:"steps"`
	Answered int `json:"answered"`
	Failed   int `json:"failed"`

	Pending int `json:"pending"`

	Refused map[string]string `json:"refused,omitempty"`

	// what was granted, not what was asked for
	RetainSec int `json:"retainSec"`
}

func (j *job) armRetire() {
	j.generation++
	j.SendAfter(j.PID(), messageJobRetire{generation: j.generation}, j.retention())
}

func (j *job) retention() time.Duration {
	if j.spec.retain > 0 {
		return j.spec.retain
	}
	return defaultJobMaxRetention
}

func (j *job) Terminate(reason error) {
	j.Log().Info("job %s terminated in state %s: %s", j.spec.key, j.state, reason)
}

const jobInspectHelp = "summary keys: key, subject, state, steps, answered, failed, " +
	"seq, uptime, dropped"

func (j *job) HandleInspect(from gen.PID, item ...string) map[string]string {
	if len(item) == 0 {
		return map[string]string{
			"key":      j.spec.key,
			"subject":  j.spec.subject,
			"state":    j.state,
			"steps":    fmt.Sprintf("%d", len(j.spec.steps)),
			"answered": fmt.Sprintf("%d", j.answered),
			"failed":   fmt.Sprintf("%d", j.failed),
			"seq":      fmt.Sprintf("%d", j.seq),
			"uptime":   inspectAge(j.started),
			"dropped":  inspectCounters(j.dropped),
			"items":    "help",
		}
	}

	result := map[string]string{}
	for _, q := range item {
		switch q {
		case "help":
			result[q] = jobInspectHelp
		case "dropped":
			result[q] = inspectCounters(j.dropped)
		default:
			result[q] = "<unknown item>"
		}
	}
	return result
}

type fanoutSpec struct {
	back    gen.PID
	workers int

	ceiling Ceiling
}

type fanoutPool struct {
	act.Pool
}

func (f *fanoutPool) Init(args ...any) (act.PoolOptions, error) {
	spec, ok := args[0].(fanoutSpec)
	if ok == false {
		return act.PoolOptions{}, fmt.Errorf("fanout spec expected, got %T", args[0])
	}
	return act.PoolOptions{
		PoolSize:      int64(spec.workers),
		WorkerFactory: factory_fanoutWorker,
		WorkerArgs:    []any{spec},
	}, nil
}

// blocking here is the point: the worker exists so the job does not wait
type fanoutWorker struct {
	act.Actor

	spec fanoutSpec
}

func (f *fanoutWorker) Init(args ...any) error {
	spec, ok := args[0].(fanoutSpec)
	if ok == false {
		return fmt.Errorf("fanout spec expected, got %T", args[0])
	}
	f.spec = spec
	return nil
}

func (f *fanoutWorker) HandleMessage(from gen.PID, message any) error {
	ask, ok := message.(messageJobAsk)
	if ok == false {
		return nil
	}

	answer := messageJobFinished{Step: ask.step, Status: "ok"}
	tool, served := toolByName(ask.step.Tool)
	if served == false || tool.servesAction() == false {
		answer.Status, answer.Error = "refused", "there is no tool "+ask.step.Tool
		f.Send(f.spec.back, answer)
		return nil
	}

	action, built, err := toolAction(tool, ask.step.Args)
	if err != nil {
		answer.Status, answer.Error = "refused", err.Error()
		f.Send(f.spec.back, answer)
		return nil
	}

	request, capability, err := buildActionRequest(action, built)
	if err != nil {
		answer.Status, answer.Error = "refused", err.Error()
		f.Send(f.spec.back, answer)
		return nil
	}

	target := gen.ProcessID{Name: plane(capability), Node: ask.step.Node}
	result, err := f.CallWithTimeout(target, request, jobNodeTimeout)
	switch {
	case err == gen.ErrTimeout:
		answer.Status, answer.Error = "timeout", err.Error()
	case err != nil:
		answer.Status, answer.Error = "unreachable", err.Error()
	default:
		answer.Value, answer.Status, answer.Error = jobDigest(result, action, capability, f.spec.ceiling)
	}

	f.Send(f.spec.back, answer)
	return nil
}

// the ceiling of whoever started the run: an empty one would let a read-only caller learn the
// whole manage plane of every node
func jobDigest(result any, action string, capability string, ceiling Ceiling) (any, string, string) {
	if failure := actionError(result); failure != nil {
		return nil, "refused", failure.Error()
	}
	return agentPayload(result, action, capability, ceiling), "ok", ""
}
