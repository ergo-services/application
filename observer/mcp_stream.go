package observer

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"

	"ergo.services/ergo/act"
	"ergo.services/ergo/gen"
	"ergo.services/meta/sse"
)

func factory_mcpStream() gen.ProcessBehavior {
	return &mcpStream{}
}

const streamCoalescePeriod = 500 * time.Millisecond

type streamSpec struct {
	sse     gen.Alias
	id      any
	subject string
	uris    []mcpURI
	refused map[string]string
}

type (
	messageStreamReady struct{}
	messageStreamFlush struct{}
)

type mcpStream struct {
	act.Actor

	id      string
	spec    streamSpec
	watched map[string]string
	moved   map[string]struct{}
	pending bool
	closed  bool

	frames  int64
	dropped map[string]int64
	started time.Time
}

func (s *mcpStream) Init(args ...any) error {
	spec, ok := args[0].(streamSpec)
	if ok == false {
		return fmt.Errorf("stream spec expected, got %T", args[0])
	}
	s.spec = spec
	s.id = newSubscriptionID()
	s.watched = make(map[string]string)
	s.moved = make(map[string]struct{})
	s.dropped = make(map[string]int64)
	s.started = time.Now()

	s.Log().SetLogger("default")
	if err := s.LinkAlias(spec.sse); err != nil {
		return err
	}

	return s.Send(s.PID(), messageStreamReady{})
}

func newSubscriptionID() string {
	b := make([]byte, 12)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func (s *mcpStream) HandleMessage(from gen.PID, message any) error {
	switch m := message.(type) {
	case messageStreamReady:
		s.open()

	case messageStreamFlush:
		s.flush()

	case gen.MessageDownEvent:
		s.sourceGone(m.Event)

	default:
		s.dropped["message_unexpected"]++
		s.Log().Warning("stream %s: unexpected message %#v", s.id, message)
	}
	return nil
}

func (s *mcpStream) open() {
	accepted := make([]string, 0, len(s.spec.uris))

	refused := map[string]string{}
	for uri, why := range s.spec.refused {
		refused[uri] = why
	}

	for _, uri := range s.spec.uris {
		event, err := s.raise(uri)
		if err != nil {
			s.dropped["raise_failed"]++
			refused[uri.Canonical()] = err.Error()
			continue
		}
		buffered, err := s.MonitorEvent(event)
		if err != nil {
			s.dropped["monitor_failed"]++
			refused[uri.Canonical()] = err.Error()
			continue
		}
		s.watched[event.String()] = uri.Canonical()
		accepted = append(accepted, uri.Canonical())

		if len(buffered) > 0 {
			s.mark(uri.Canonical())
		}
	}

	meta := s.frameMeta()
	if len(refused) > 0 {
		meta.Refused = refused
	}
	s.notify(mcpNotify(mcpSubscriptionsAck, mcpAcknowledged{
		Meta:          meta,
		Notifications: mcpFilter{ResourceSubscriptions: accepted},
	}))

	if len(accepted) == 0 {
		s.Log().Warning("stream %s: nothing to follow, closing", s.id)
		s.closing("nothing could be followed")
		s.SendExitMeta(s.spec.sse, gen.TerminateReasonNormal)
	}
}

func (s *mcpStream) frameMeta() mcpFrameMeta {
	return mcpFrameMeta{SubscriptionID: s.spec.id}
}

func (s *mcpStream) closing(reason string) {
	if s.closed {
		return
	}
	s.closed = true

	s.notify(mcpNotify(mcpCancelled, mcpCancelledParams{
		Meta:      s.frameMeta(),
		RequestID: s.spec.id,
		Reason:    reason,
	}))
	s.notify(mcpResult{
		JSONRPC: "2.0",
		ID:      s.spec.id,
		Result:  mcpListenResult{ResultType: mcpResultComplete, Meta: s.frameMeta()},
	})
}

func (s *mcpStream) raise(uri mcpURI) (gen.Event, error) {
	if uri.Lens == uriWordJob {
		name := jobName(uri.Key, s.spec.subject)
		result, err := s.CallWithTimeout(name, ownerReadRequest{}, ownerCallTimeout)
		if err != nil {
			return gen.Event{}, fmt.Errorf("there is no run under this key")
		}
		if err := readingRefused(result); err != nil {
			return gen.Event{}, err
		}
		return gen.Event{Name: jobEventName(uri.Key, s.spec.subject), Node: s.Node().Name()}, nil
	}

	if uri.Node != s.Node().Name() {
		if _, err := s.Node().Network().Node(uri.Node); err != nil {
			return gen.Event{}, fmt.Errorf("node %s is not connected", string(uri.Node))
		}
	}

	event := gen.Event{Name: ownerEventName(uri, s.spec.subject), Node: s.Node().Name()}
	name := uri.ownerName(s.spec.subject)

	result, err := s.CallWithTimeout(name, ownerReadRequest{}, ownerCallTimeout)
	if err == gen.ErrProcessUnknown {
		result, err = s.CallWithTimeout(managerName,
			ownerReadForward{URI: uri, Subject: s.spec.subject}, mcpReadTimeout)
	}
	if err != nil {
		return gen.Event{}, err
	}
	if err := readingRefused(result); err != nil {
		return gen.Event{}, err
	}
	return event, nil
}

func readingRefused(result any) error {
	reading, ok := result.(ownerReadResponse)
	switch {
	case ok == false:
		return fmt.Errorf("unexpected answer %T", result)
	case reading.Error != "":
		return errors.New(reading.Error)
	}
	return nil
}

func (s *mcpStream) HandleEvent(message gen.MessageEvent) error {
	uri, following := s.watched[message.Event.String()]
	if following == false {
		s.dropped["event_unknown"]++
		return nil
	}

	s.mark(uri)
	return nil
}

func (s *mcpStream) mark(uri string) {
	s.moved[uri] = struct{}{}

	if s.pending == false {
		s.pending = true
		s.SendAfter(s.PID(), messageStreamFlush{}, streamCoalescePeriod)
	}
}

func (s *mcpStream) flush() {
	s.pending = false

	held := make(map[string]struct{})
	for uri := range s.moved {
		err := s.notify(mcpNotify(mcpResourceUpdated,
			mcpResourceRef{Meta: s.frameMeta(), URI: uri}))
		if errors.Is(err, errFrameNotSent) {
			held[uri] = struct{}{}
		}
	}
	s.moved = held

	if len(held) > 0 {
		s.pending = true
		s.SendAfter(s.PID(), messageStreamFlush{}, streamCoalescePeriod)
	}
}

func (s *mcpStream) sourceGone(event gen.Event) {
	key := event.String()
	uri, following := s.watched[key]
	if following == false {
		return
	}
	delete(s.watched, key)
	delete(s.moved, uri)
	s.Log().Info("stream %s: %s is no longer held", s.id, uri)

	meta := s.frameMeta()
	meta.Refused = map[string]string{uri: "the node stopped publishing it"}
	s.notify(mcpNotify(mcpSubscriptionsAck, mcpAcknowledged{
		Meta:          meta,
		Notifications: mcpFilter{ResourceSubscriptions: s.following()},
	}))

	if len(s.watched) == 0 {
		s.Log().Info("stream %s: nothing left to follow, closing", s.id)
		s.closing("every resource this stream followed is gone")
		s.SendExitMeta(s.spec.sse, gen.TerminateReasonNormal)
	}
}

func (s *mcpStream) following() []string {
	out := make([]string, 0, len(s.watched))
	for _, uri := range s.watched {
		out = append(out, uri)
	}
	sort.Strings(out)
	return out
}

var errFrameNotSent = errors.New("the frame did not reach the connection")

func (s *mcpStream) notify(payload any) error {
	data, err := json.Marshal(payload)
	if err != nil {
		s.dropped["marshal_failed"]++
		return err
	}
	if err := s.SendAlias(s.spec.sse, sse.Message{Data: data}); err != nil {
		s.dropped["frame_dropped"]++
		return errFrameNotSent
	}
	s.frames++
	return nil
}

func (s *mcpStream) Terminate(reason error) {
	s.Log().Info("stream %s terminated: %s", s.id, reason)

	s.closing(reason.Error())
	s.SendExitMeta(s.spec.sse, reason)
}

const streamInspectHelp = "summary keys: id, subscription, subject, watching, moved, frames, uptime, dropped"

func (s *mcpStream) HandleInspect(from gen.PID, item ...string) map[string]string {
	if len(item) == 0 {
		return map[string]string{
			"id":           s.id,
			"subscription": fmt.Sprintf("%v", s.spec.id),
			"subject":      s.spec.subject,
			"watching":     fmt.Sprintf("%d", len(s.watched)),
			"moved":        fmt.Sprintf("%d", len(s.moved)),
			"frames":       fmt.Sprintf("%d", s.frames),
			"uptime":       inspectAge(s.started),
			"dropped":      inspectCounters(s.dropped),
			"items":        "help",
		}
	}

	result := map[string]string{}
	for _, q := range item {
		switch q {
		case "help":
			result[q] = streamInspectHelp
		case "dropped":
			result[q] = inspectCounters(s.dropped)
		default:
			result[q] = "<unknown item>"
		}
	}
	return result
}
