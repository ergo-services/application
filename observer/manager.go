package observer

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"ergo.services/ergo/act"
	"ergo.services/ergo/gen"
	"ergo.services/meta/sse"
)

func factory_manager() gen.ProcessBehavior {
	return &manager{}
}

type manager struct {
	act.Actor

	ceiling Ceiling

	jobLimit int

	enrollment  EnrollmentOptions
	enrolled    bool
	lastEnroll  time.Time
	spawned     int64
	owners      int64
	jobs        int64
	streams     int64
	disconnects int64
	lastConnect time.Time
	lastSession gen.Atom
	dropped     map[string]int64
}

func (m *manager) Init(args ...any) error {
	m.Log().SetLogger("default")
	m.dropped = make(map[string]int64)

	if v, exist := m.Env(envCeiling); exist {
		if c, ok := v.(Ceiling); ok {
			m.ceiling = c
		}
	}
	if v, exist := m.Env(envEnrollment); exist {
		if e, ok := v.(EnrollmentOptions); ok {
			m.enrollment = e
		}
	}
	m.jobLimit = defaultJobLimit
	if v, exist := m.Env(envJobLimit); exist {
		if limit, ok := v.(int); ok && limit > 0 {
			m.jobLimit = limit
		}
	}

	m.Log().Info("session manager started")
	return nil
}

func (m *manager) HandleMessage(from gen.PID, message any) error {
	switch msg := message.(type) {
	case sse.MessageConnect:
		if msg.Request != nil && strings.HasPrefix(msg.Request.URL.Path, "/mcp") {
			m.handleListen(msg)
			break
		}
		m.handleConnect(msg)

	case sse.MessageDisconnect:
		m.disconnects++
		m.Log().Debug("SSE disconnect: %s", msg.ID)

	case sse.MessageLastEventID:
		m.dropped["last_event_id_ignored"]++
		m.Log().Debug("SSE last event ID: %s (ignored)", msg.LastEventID)

	default:
		m.dropped["message_unexpected"]++
		m.Log().Warning("unknown message from %s: %#v", from, message)
	}
	return nil
}

func (m *manager) HandleCall(from gen.PID, ref gen.Ref, request any) (any, error) {
	if read, ok := request.(ownerReadForward); ok {
		return m.forwardRead(from, ref, read)
	}
	if ensure, ok := request.(jobEnsureRequest); ok {
		return m.ensureJob(ensure), nil
	}

	req, ok := request.(EnrollRequest)
	if ok == false {
		m.dropped["request_unsupported"]++
		return gen.ErrUnsupported, nil
	}

	switch {
	case m.enrolled:
		m.dropped["enroll_burned"]++
		m.Log().Warning("enrollment attempt after the secret was spent")
		return EnrollResponse{Burned: true}, nil

	case m.enrollment.Token == "":
		m.dropped["enroll_unconfigured"]++
		return EnrollResponse{Error: errors.New("enrollment is not configured")}, nil

	case subtle.ConstantTimeCompare([]byte(req.Token), []byte(m.enrollment.Token)) != 1:
		m.dropped["enroll_failed"]++
		m.Log().Warning("enrollment attempt with a wrong secret")
		return EnrollResponse{Error: errors.New("enrollment refused")}, nil
	}

	m.enrolled = true
	m.lastEnroll = time.Now()
	m.enrollment.Token = ""
	m.Log().Info("enrolled as cluster %q, the secret is spent", m.enrollment.ClusterID)
	return EnrollResponse{ClusterID: m.enrollment.ClusterID}, nil
}

const managerInspectHelp = "summary keys: spawned, owners, jobs, disconnects, last_connect, " +
	"last_session, enrollment, enrolled, last_enroll, dropped"

func (m *manager) HandleInspect(from gen.PID, item ...string) map[string]string {
	if len(item) == 0 {
		return map[string]string{
			"spawned":      fmt.Sprintf("%d", m.spawned),
			"owners":       fmt.Sprintf("%d", m.owners),
			"jobs":         fmt.Sprintf("%d", m.jobs),
			"disconnects":  fmt.Sprintf("%d", m.disconnects),
			"last_connect": inspectAge(m.lastConnect),
			"last_session": string(m.lastSession),
			"enrollment":   yesno(m.enrollment.Token != "" || m.enrolled),
			"enrolled":     yesno(m.enrolled),
			"last_enroll":  inspectAge(m.lastEnroll),
			"dropped":      inspectCounters(m.dropped),
			"items":        "help",
		}
	}

	result := map[string]string{}
	for _, q := range item {
		switch q {
		case "help":
			result["help"] = managerInspectHelp
		case "dropped":
			result[q] = inspectCounters(m.dropped)
		default:
			result[q] = "<unknown item>"
		}
	}
	return result
}

func (m *manager) handleConnect(msg sse.MessageConnect) {
	sessionID := generateSessionID()
	sessionName := gen.Atom("observer_session_" + sessionID)

	spec := sessionSpec{
		id:               sessionID,
		sse:              msg.ID,
		ceiling:          m.ceiling,
		maxSubscriptions: maxSubscriptionsOf(msg.Request),
	}
	if identity, ok := identityOf(msg.Request); ok {
		spec.subject = qualified(identity)
		spec.ceiling = identity.Ceiling
	} else {
		m.dropped["identity_missing"]++
	}

	m.Log().Info("SSE connect: %s → %s", msg.ID, sessionName)

	opts := gen.ProcessOptions{
		LinkParent:  true,
		InitTimeout: sessionInitTimeout,
	}
	_, err := m.SpawnRegister(sessionName, factory_session, opts, spec)
	if err != nil {
		m.dropped["session_spawn_failed"]++
		m.Log().Error("failed to spawn session %s: %s", sessionName, err)
		if err := m.SendExitMeta(msg.ID, err); err != nil {
			m.Log().Error("unable to close the stream of %s: %s", msg.ID, err)
		}
		return
	}

	m.spawned++
	m.lastConnect = time.Now()
	m.lastSession = sessionName
}

type ownerReadForward struct {
	URI     mcpURI
	Subject string
	Read    ownerReadRequest
}

func (m *manager) handleListen(msg sse.MessageConnect) {
	filter, ok := listenFilterOf(msg.Request)
	if ok == false || len(filter.uris) == 0 {
		m.dropped["listen_without_filter"]++
		m.Log().Error("MCP listen without a filter: %s", msg.ID)
		m.closeMeta(msg.ID, errors.New("no resource to follow"))
		return
	}

	identity, _ := identityOf(msg.Request)
	spec := streamSpec{
		sse:     msg.ID,
		id:      filter.id,
		subject: qualified(identity),
		uris:    filter.uris,
		refused: filter.refused,
	}

	name := gen.Atom("observer_stream_" + newSubscriptionID())
	if _, err := m.SpawnRegister(name, factory_mcpStream, gen.ProcessOptions{
		LinkParent:  true,
		InitTimeout: sessionInitTimeout,
	}, spec); err != nil {
		m.dropped["stream_spawn_failed"]++
		m.Log().Error("failed to spawn stream %s: %s", name, err)
		m.closeMeta(msg.ID, err)
		return
	}

	m.streams++
	m.Log().Info("MCP listen: %s → %s, following %d", msg.ID, name, len(filter.uris))
}

func (m *manager) closeMeta(alias gen.Alias, reason error) {
	if err := m.SendExitMeta(alias, reason); err != nil {
		m.Log().Error("unable to close the stream of %s: %s", alias, err)
	}
}

type jobEnsureRequest struct {
	Spec jobSpec
}

type jobEnsureResponse struct {
	URI     string
	Started bool
	Error   string
}

func (m *manager) ensureJob(ensure jobEnsureRequest) jobEnsureResponse {
	name := jobName(ensure.Spec.key, ensure.Spec.subject)
	uri := jobURI(ensure.Spec.key)

	if held := m.runsHeld(); held >= m.jobLimit {
		m.dropped["job_limit_reached"]++
		return jobEnsureResponse{URI: uri, Error: fmt.Sprintf(
			"this observer already holds %d runs, which is its limit: cancel one or wait for "+
				"a finished one to expire", held)}
	}

	_, err := m.SpawnRegister(name, factory_job, gen.ProcessOptions{
		LinkParent:  true,
		InitTimeout: sessionInitTimeout,
	}, ensure.Spec)

	switch {
	case err == gen.ErrTaken:
		return jobEnsureResponse{URI: uri}
	case err != nil:
		m.dropped["job_spawn_failed"]++
		m.Log().Error("unable to spawn job %s: %s", ensure.Spec.key, err)
		return jobEnsureResponse{URI: uri, Error: err.Error()}
	}

	m.jobs++
	m.Log().Info("job %s started with %d steps", ensure.Spec.key, len(ensure.Spec.steps))
	return jobEnsureResponse{URI: uri, Started: true}
}

func (m *manager) runsHeld() int {
	held := 0
	m.Node().ProcessRangeShortInfo(func(info gen.ProcessShortInfo) bool {
		if info.Application == appName && strings.HasPrefix(string(info.Name), ownerPrefixJob) {
			held++
		}
		return true
	})
	return held
}

func (m *manager) forwardRead(from gen.PID, ref gen.Ref, read ownerReadForward) (any, error) {
	name := read.URI.ownerName(read.Subject)
	spec := ownerSpec{uri: read.URI, subject: read.Subject}

	_, err := m.SpawnRegister(name, factory_uriOwner, gen.ProcessOptions{
		LinkParent:  true,
		InitTimeout: sessionInitTimeout,
	}, spec)
	if err != nil && err != gen.ErrTaken {
		m.dropped["owner_spawn_failed"]++
		m.Log().Error("unable to spawn owner of %s: %s", read.URI.Canonical(), err)
		return ownerReadResponse{URI: read.URI.Canonical(), Error: err.Error()}, nil
	}
	if err == nil {
		m.owners++
	}

	read.Read.PID, read.Read.Ref = from, ref
	if err := m.Send(name, read.Read); err != nil {
		m.dropped["owner_forward_failed"]++
		return ownerReadResponse{URI: read.URI.Canonical(), Error: err.Error()}, nil
	}
	return nil, nil
}

func (m *manager) Terminate(reason error) {
	m.Log().Info("session manager terminated: %s", reason)
}

func generateSessionID() string {
	b := make([]byte, 16)
	rand.Read(b)
	return hex.EncodeToString(b)
}
