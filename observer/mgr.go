package observer

import (
	"crypto/rand"
	"encoding/hex"

	"ergo.services/ergo/act"
	"ergo.services/ergo/gen"
	"ergo.services/meta/sse"
)

func factory_mgr() gen.ProcessBehavior {
	return &mgr{}
}

// mgr receives SSE connect/disconnect and manages session actor lifecycle.
// SSE handler has ProcessPool: [mgrName], so all SSE messages come here.
type mgr struct {
	act.Actor
}

func (m *mgr) Init(args ...any) error {
	m.Log().SetLogger("default")
	m.Log().Info("session manager started")
	return nil
}

func (m *mgr) HandleMessage(from gen.PID, message any) error {
	switch msg := message.(type) {
	case sse.MessageConnect:
		m.handleConnect(msg)

	case sse.MessageDisconnect:
		// no-op: session terminates via LinkAlias when SSE meta dies
		m.Log().Debug("SSE disconnect: %s", msg.ID)

	case sse.MessageLastEventID:
		// ignored in v2: each reconnect is a fresh session
		m.Log().Debug("SSE last event ID: %s (ignored)", msg.LastEventID)

	default:
		m.Log().Warning("unknown message from %s: %#v", from, message)
	}
	return nil
}

func (m *mgr) handleConnect(msg sse.MessageConnect) {
	sessionID := generateSessionID()
	sessionName := gen.Atom("observer_session_" + sessionID)

	m.Log().Info("SSE connect: %s → %s", msg.ID, sessionName)

	opts := gen.ProcessOptions{
		LinkParent: true,
	}
	_, err := m.SpawnRegister(sessionName, factory_session, opts, sessionID, msg.ID)
	if err != nil {
		m.Log().Error("failed to spawn session %s: %s", sessionName, err)
		return
	}
}

func (m *mgr) Terminate(reason error) {
	m.Log().Info("session manager terminated: %s", reason)
}

func generateSessionID() string {
	b := make([]byte, 16)
	rand.Read(b)
	return hex.EncodeToString(b)
}
