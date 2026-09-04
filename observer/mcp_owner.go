package observer

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"ergo.services/ergo/act"
	"ergo.services/ergo/app/system/inspect"
	"ergo.services/ergo/gen"
)

func factory_uriOwner() gen.ProcessBehavior {
	return &uriOwner{}
}

const (
	ownerIdlePeriod  = 30 * time.Second
	ownerKeyedIdle   = 10 * time.Minute
	ownerCallTimeout = 5 // seconds
)

type ownerSpec struct {
	uri     mcpURI
	subject string
}

type (
	messageOwnerShutdown struct{ generation int64 }
)

type ownerReadRequest struct {
	PID   gen.PID
	Ref   gen.Ref
	Since string
}

type ownerReadResponse struct {
	URI     string
	Seq     int64
	At      time.Time
	Value   any
	Batches []any
	NextSeq string
	Dropped bool
	Error   string
}

type ownerBatch struct {
	seq   int64
	at    time.Time
	value any
}

const ownerRingBatches = 64

var lensAccumulates = map[string]bool{
	"log":           true,
	"tracing_spans": true,
	"event_stream":  true,
}

type uriOwner struct {
	act.Actor

	spec  ownerSpec
	event gen.Atom
	token gen.Ref

	source    gen.Event
	monitored bool

	value any
	seq   int64
	at    time.Time

	accumulating bool
	ring         []ownerBatch
	epoch        string

	pending []ownerReadRequest // readers waiting for the first value

	subscribed bool
	generation int64
	idle       time.Duration
	started    time.Time
	dropped    map[string]int64
}

func (o *uriOwner) Init(args ...any) error {
	spec, ok := args[0].(ownerSpec)
	if ok == false {
		return fmt.Errorf("owner spec expected, got %T", args[0])
	}
	o.spec = spec
	o.idle = ownerIdlePeriod
	if spec.uri.Key != "" {
		o.idle = ownerKeyedIdle
	}
	o.dropped = make(map[string]int64)
	o.started = time.Now()
	o.accumulating = lensAccumulates[lensOf(spec.uri.Lens)]
	o.epoch = epochOf(o.PID())

	o.Log().SetLogger("default")
	o.SetProcessKind(gen.ProcessKindMonitor)

	name, err := o.RegisterEvent(o.eventName(), gen.EventOptions{Notify: true, Buffer: 1})
	if err != nil {
		return err
	}
	o.event, o.token = o.eventName(), name

	o.armShutdown()
	return nil
}

func (o *uriOwner) eventName() gen.Atom {
	return ownerEventName(o.spec.uri, o.spec.subject)
}

func ownerEventName(uri mcpURI, subject string) gen.Atom {
	return uri.ownerName(subject) + "_updates"
}

func (o *uriOwner) HandleMessage(from gen.PID, message any) error {
	switch m := message.(type) {
	case gen.MessageEventStart:
		o.subscribed = true
		o.Log().Debug("owner %s: first consumer, starting", o.spec.uri.Canonical())
		return o.produce()

	case gen.MessageEventStop:
		o.subscribed = false
		if len(o.pending) == 0 {
			o.rest()
		}

	case ownerReadRequest:
		return o.read(m)

	case gen.MessageDownEvent:
		o.monitored = false
		o.dropped["source_down"]++
		o.Log().Warning("owner %s: source %s is gone", o.spec.uri.Canonical(), m.Event)
		o.failPending(fmt.Errorf("node %s stopped publishing %s",
			string(o.spec.uri.Node), o.spec.uri.Lens))
		return gen.TerminateReasonNormal

	case messageOwnerShutdown:
		if m.generation != o.generation || o.demanded() {
			break
		}
		return gen.TerminateReasonNormal

	default:
		o.dropped["message_unexpected"]++
		o.Log().Warning("owner %s: unexpected message %#v", o.spec.uri.Canonical(), message)
	}
	return nil
}

func (o *uriOwner) HandleCall(from gen.PID, ref gen.Ref, request any) (any, error) {
	read, ok := request.(ownerReadRequest)
	if ok == false {
		o.dropped["request_unsupported"]++
		return gen.ErrUnsupported, nil
	}

	if read.PID == (gen.PID{}) {
		read.PID, read.Ref = from, ref
	}

	if o.readable() {
		answer := o.readingFor(read.Since)
		o.armShutdown()
		return answer, nil
	}
	return nil, o.read(read)
}

func (o *uriOwner) read(request ownerReadRequest) error {
	if o.readable() {
		o.SendResponse(request.PID, request.Ref, o.readingFor(request.Since))
		o.armShutdown()
		return nil
	}
	o.pending = append(o.pending, request)
	if err := o.produce(); err != nil {
		return err
	}

	if o.accumulating {
		o.answerPending()
	}
	return nil
}

func (o *uriOwner) readable() bool {
	if o.accumulating {
		return len(o.ring) > 0
	}
	return o.value != nil
}

func (o *uriOwner) HandleEvent(message gen.MessageEvent) error {
	o.absorb(message.Message)
	o.publish()
	o.answerPending()
	return nil
}

func (o *uriOwner) absorb(value any) {
	o.value = value
	o.seq++
	o.at = time.Now()

	if o.accumulating == false {
		return
	}
	o.ring = append(o.ring, ownerBatch{seq: o.seq, at: o.at, value: value})
	if len(o.ring) > ownerRingBatches {
		o.dropped["batches_evicted"] += int64(len(o.ring) - ownerRingBatches)
		o.ring = o.ring[len(o.ring)-ownerRingBatches:]
	}
}

func (o *uriOwner) publish() {
	if err := o.SendEvent(o.event, o.token, messageOwnerUpdated{
		URI: o.spec.uri.Canonical(),
		Seq: o.seq,
		At:  o.at,
	}); err != nil {
		o.dropped["publish_failed"]++
		o.Log().Error("owner %s: publish: %s", o.spec.uri.Canonical(), err)
	}
}

type messageOwnerUpdated struct {
	URI string
	Seq int64
	At  time.Time
}

func (o *uriOwner) produce() error {
	if o.monitored {
		return nil
	}
	args, err := lensArgs(o.spec.uri)
	if err != nil {
		o.failPending(err)
		return nil
	}
	request, _, err := buildInspectRequest(lensOf(o.spec.uri.Lens), args)
	if err != nil {
		o.failPending(err)
		return nil
	}

	target := gen.ProcessID{Name: inspect.Name, Node: o.spec.uri.Node}
	result, err := o.CallWithTimeout(target, request, ownerCallTimeout)
	if err != nil {
		o.dropped["source_call_failed"]++
		o.failPending(err)
		return nil
	}

	event, err := extractEvent(result)
	if err != nil {
		o.dropped["source_event_missing"]++
		o.failPending(err)
		return nil
	}

	if why, absent := mcpAbsent(result, o.spec.uri); absent {
		o.dropped["source_absent"]++
		o.failPending(errors.New(why))
		return nil
	}

	buffered, err := o.MonitorEvent(event)
	if err != nil {
		o.dropped["source_monitor_failed"]++
		o.failPending(err)
		return nil
	}
	o.source, o.monitored = event, true
	o.Log().Debug("owner %s: watching %s", o.spec.uri.Canonical(), event)

	for _, held := range buffered {
		o.absorb(held.Message)
	}
	if len(buffered) > 0 {
		o.publish()
		o.answerPending()
	}
	return nil
}

func (o *uriOwner) rest() {
	if o.monitored {
		o.DemonitorEvent(o.source)
		o.monitored = false
	}
	o.armShutdown()
}

func (o *uriOwner) armShutdown() {
	o.generation++
	o.SendAfter(o.PID(), messageOwnerShutdown{generation: o.generation}, o.idle)
}

func (o *uriOwner) demanded() bool {
	return o.subscribed || len(o.pending) > 0
}

func (o *uriOwner) reading() ownerReadResponse {
	return ownerReadResponse{
		URI:   o.spec.uri.Canonical(),
		Seq:   o.seq,
		At:    o.at,
		Value: o.value,
	}
}

func (o *uriOwner) readingFor(since string) ownerReadResponse {
	out := o.reading()
	if o.accumulating == false {
		return out
	}
	out.Value = nil
	out.Batches = []any{}
	out.NextSeq = fmt.Sprintf("%s.%d", o.epoch, o.seq)

	from, ok := o.cursor(since)
	if ok == false && since != "" {
		out.Dropped = true
	}
	for _, batch := range o.ring {
		if batch.seq > from {
			out.Batches = append(out.Batches, batch.value)
		}
	}
	if since != "" && ok && len(o.ring) > 0 && from < o.ring[0].seq-1 {
		out.Dropped = true
	}
	return out
}

func (o *uriOwner) cursor(since string) (int64, bool) {
	if since == "" {
		return 0, true
	}
	epoch, seq, found := strings.Cut(since, ".")
	if found == false {
		return 0, false
	}
	if epoch != o.epoch {
		return 0, false
	}
	value, err := strconv.ParseInt(seq, 10, 64)
	if err != nil {
		return 0, false
	}
	return value, true
}

func (o *uriOwner) answerPending() {
	if len(o.pending) == 0 {
		return
	}
	for _, read := range o.pending {
		o.SendResponse(read.PID, read.Ref, o.readingFor(read.Since))
	}
	o.pending = nil
	o.armShutdown()
}

func (o *uriOwner) failPending(reason error) {
	if len(o.pending) == 0 {
		return
	}
	for _, read := range o.pending {
		o.SendResponse(read.PID, read.Ref, ownerReadResponse{
			URI:   o.spec.uri.Canonical(),
			Error: reason.Error(),
		})
	}
	o.pending = nil
	if o.subscribed == false {
		o.rest()
	}
}

var lensSubscription = map[string]string{
	"node":         "node_info",
	"processes":    "process_list",
	"process":      "process_info",
	"meta":         "meta_info",
	"network":      "network_info",
	"connections":  "connection_list",
	"connection":   "connection_info",
	"events":       "event_list",
	"event":        "event_info",
	"stream":       "event_stream",
	"applications": "application_list",
	"log":          "log",
	"tracing":      "tracing_spans",
}

func lensOf(lens string) string {
	return lensSubscription[lens]
}

func lensArgs(uri mcpURI) (map[string]any, error) {
	args := map[string]any{}

	if uri.Target != "" {
		key, value, err := lensTarget(uri)
		if err != nil {
			return nil, err
		}
		args[key] = value
	}

	for name, values := range uri.Scope {
		if len(values) == 0 {
			continue
		}
		value := values[0]
		switch {
		case lensArgNumber[name]:
			number, err := strconv.ParseFloat(value, 64)
			if err != nil {
				return nil, fmt.Errorf("%s=%q is not a number", name, value)
			}
			args[name] = number
		case lensArgBool[name]:
			flag, err := strconv.ParseBool(value)
			if err != nil {
				return nil, fmt.Errorf("%s=%q is not true or false", name, value)
			}
			args[name] = flag
		case lensArgList[name]:
			entries := []any{}
			for _, entry := range strings.Split(value, ",") {
				if entry = strings.TrimSpace(entry); entry != "" {
					entries = append(entries, entry)
				}
			}
			if len(entries) == 0 {
				return nil, fmt.Errorf("%s=%q names nothing", name, value)
			}
			args[name] = entries
		default:
			args[name] = value
		}
	}
	return args, nil
}

var (
	lensArgNumber = map[string]bool{
		"limit": true, "timestamp": true, "rate": true, "pidStart": true, "pidLimit": true,
		"minSubscribers": true, "minMailbox": true, "threshold": true, "points": true,
		"minWait": true, "minBytes": true, "kinds": true,
	}
	lensArgBool = map[string]bool{
		"messageExclude": true, "force": true, "verbose": true, "order": true,
		"important": true, "enabled": true,
	}

	lensArgList = map[string]bool{"levels": true}
)

func lensTarget(uri mcpURI) (string, any, error) {
	switch lensOf(uri.Lens) {
	case "process_info":
		pid, err := mcpParsePID(uri.Target, uri.Node)
		return "pid", pid, err
	case "meta_info":
		alias, err := mcpParseAlias(uri.Target, uri.Node)
		return "alias", alias, err
	case "event_info", "event_stream":
		event, err := mcpParseEvent(uri.Target, uri.Node)
		return "name", string(event.Name), err
	case "connection_info":
		return nodeArgument, uri.Target, nil
	}
	return "", nil, fmt.Errorf("lens %q takes no target", uri.Lens)
}

func parseUint(text string, into *uint64) error {
	value, err := strconv.ParseUint(text, 10, 64)
	if err != nil {
		return errors.New("identifiers are decimal")
	}
	*into = value
	return nil
}

func (o *uriOwner) Terminate(reason error) {
	o.Log().Debug("owner %s terminated: %s", o.spec.uri.Canonical(), reason)
	o.failPending(errors.New("the resource is no longer held"))
}

const ownerInspectHelp = "summary keys: uri, subject, event, source, watching, subscribed, " +
	"pending, seq, last_value, idle, uptime, dropped"

func (o *uriOwner) HandleInspect(from gen.PID, item ...string) map[string]string {
	if len(item) == 0 {
		last := "never"
		if o.seq > 0 {
			last = time.Since(o.at).Round(time.Millisecond).String()
		}
		source := "none"
		if o.monitored {
			source = o.source.String()
		}
		return map[string]string{
			"uri":        o.spec.uri.Canonical(),
			"subject":    o.spec.subject,
			"event":      string(o.event),
			"source":     source,
			"watching":   yesno(o.monitored),
			"subscribed": yesno(o.subscribed),
			"pending":    fmt.Sprintf("%d", len(o.pending)),
			"seq":        fmt.Sprintf("%d", o.seq),
			"last_value": last,
			"idle":       o.idle.String(),
			"uptime":     inspectAge(o.started),
			"dropped":    inspectCounters(o.dropped),
			"items":      "help",
		}
	}

	result := map[string]string{}
	for _, q := range item {
		switch q {
		case "help":
			result[q] = ownerInspectHelp
		case "dropped":
			result[q] = inspectCounters(o.dropped)
		default:
			result[q] = "<unknown item>"
		}
	}
	return result
}
