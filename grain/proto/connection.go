package proto

import (
	"net"
	"sync"

	"ergo.services/ergo/gen"
)

var _ gen.Connection = (*grainConn)(nil)

// grainConn is the virtual connection for one grain domain. Its routing methods
// (added in later steps) resolve the target key to the live grain PID via host
// and re-route locally by PID; it never encodes or moves bytes. mu guards the
// per-key resolve/activation (Step 3). done keeps Serve parked until Terminate.
type grainConn struct {
	core     gen.Core
	vnode    gen.Atom
	realNode gen.Atom
	creation int64
	host     Host
	log      gen.Log

	mu   sync.Mutex
	done chan struct{}
	term sync.Once
}

func (c *grainConn) Node() gen.RemoteNode { return &remoteNode{conn: c} }

// Join is invoked by connect() (node/network.go:1128); a non-nil return aborts
// connection setup, so it MUST return nil. The socket is vestigial.
func (c *grainConn) Join(_ net.Conn, _ string, _ gen.NetworkDial, _ []byte) error { return nil }

// Terminate releases Serve. Idempotent.
func (c *grainConn) Terminate(_ error) { c.term.Do(func() { close(c.done) }) }

//
// routing methods — stubbed in Step 1; SendProcessID/CallProcessID/monitors get
// their resolve-and-route-by-PID bodies in Steps 3-5.
//

func (c *grainConn) SendProcessID(from gen.PID, to gen.ProcessID, opts gen.MessageOptions, message any) error {
	return gen.ErrUnsupported
}
func (c *grainConn) CallProcessID(from gen.PID, to gen.ProcessID, opts gen.MessageOptions, message any) error {
	return gen.ErrUnsupported
}
func (c *grainConn) MonitorProcessID(pid gen.PID, target gen.ProcessID) error   { return nil }
func (c *grainConn) DemonitorProcessID(pid gen.PID, target gen.ProcessID) error { return nil }
func (c *grainConn) LinkProcessID(pid gen.PID, target gen.ProcessID) error      { return nil }
func (c *grainConn) UnlinkProcessID(pid gen.PID, target gen.ProcessID) error    { return nil }

// The rest of gen.Connection is unsupported: a grain domain is only ever
// addressed as ProcessID{Node: vnode, Name: key}.
func (c *grainConn) SendPID(gen.PID, gen.PID, gen.MessageOptions, any) error {
	return gen.ErrUnsupported
}
func (c *grainConn) SendAlias(gen.PID, gen.Alias, gen.MessageOptions, any) error {
	return gen.ErrUnsupported
}
func (c *grainConn) SendEvent(gen.PID, gen.MessageOptions, gen.MessageEvent) error {
	return gen.ErrUnsupported
}
func (c *grainConn) SendExit(gen.PID, gen.PID, error) error { return gen.ErrUnsupported }
func (c *grainConn) SendResponse(gen.PID, gen.PID, gen.MessageOptions, any) error {
	return gen.ErrUnsupported
}
func (c *grainConn) SendResponseError(gen.PID, gen.PID, gen.MessageOptions, error) error {
	return gen.ErrUnsupported
}
func (c *grainConn) SendTerminatePID(gen.PID, error) error             { return gen.ErrUnsupported }
func (c *grainConn) SendTerminateProcessID(gen.ProcessID, error) error { return gen.ErrUnsupported }
func (c *grainConn) SendTerminateAlias(gen.Alias, error) error         { return gen.ErrUnsupported }
func (c *grainConn) SendTerminateEvent(gen.Event, error) error         { return gen.ErrUnsupported }
func (c *grainConn) CallPID(gen.PID, gen.PID, gen.MessageOptions, any) error {
	return gen.ErrUnsupported
}
func (c *grainConn) CallAlias(gen.PID, gen.Alias, gen.MessageOptions, any) error {
	return gen.ErrUnsupported
}
func (c *grainConn) LinkPID(gen.PID, gen.PID) error       { return gen.ErrUnsupported }
func (c *grainConn) UnlinkPID(gen.PID, gen.PID) error     { return gen.ErrUnsupported }
func (c *grainConn) LinkAlias(gen.PID, gen.Alias) error   { return gen.ErrUnsupported }
func (c *grainConn) UnlinkAlias(gen.PID, gen.Alias) error { return gen.ErrUnsupported }
func (c *grainConn) LinkEvent(gen.PID, gen.Event) ([]gen.MessageEvent, error) {
	return nil, gen.ErrUnsupported
}
func (c *grainConn) UnlinkEvent(gen.PID, gen.Event) error    { return gen.ErrUnsupported }
func (c *grainConn) MonitorPID(gen.PID, gen.PID) error       { return gen.ErrUnsupported }
func (c *grainConn) DemonitorPID(gen.PID, gen.PID) error     { return gen.ErrUnsupported }
func (c *grainConn) MonitorAlias(gen.PID, gen.Alias) error   { return gen.ErrUnsupported }
func (c *grainConn) DemonitorAlias(gen.PID, gen.Alias) error { return gen.ErrUnsupported }
func (c *grainConn) MonitorEvent(gen.PID, gen.Event) ([]gen.MessageEvent, error) {
	return nil, gen.ErrUnsupported
}
func (c *grainConn) DemonitorEvent(gen.PID, gen.Event) error { return gen.ErrUnsupported }
func (c *grainConn) RemoteSpawn(gen.Atom, gen.ProcessOptionsExtra) (gen.PID, error) {
	return gen.PID{}, gen.ErrUnsupported
}

var _ gen.RemoteNode = (*remoteNode)(nil)

// remoteNode presents the vnode as a gen.RemoteNode. Only Name/Creation are
// meaningful (Name is read at node/network.go:1141; stable Creation keeps
// monitors valid); Disconnect tears the connection down.
type remoteNode struct{ conn *grainConn }

func (r *remoteNode) Name() gen.Atom  { return r.conn.vnode }
func (r *remoteNode) Creation() int64 { return r.conn.creation }
func (r *remoteNode) Disconnect()     { r.conn.Terminate(nil) }

func (r *remoteNode) Uptime() int64            { return 0 }
func (r *remoteNode) ConnectionUptime() int64  { return 0 }
func (r *remoteNode) Version() gen.Version     { return ProtoVersion }
func (r *remoteNode) Info() gen.RemoteNodeInfo { return gen.RemoteNodeInfo{Node: r.conn.vnode} }
func (r *remoteNode) Spawn(gen.Atom, gen.ProcessOptions, ...any) (gen.PID, error) {
	return gen.PID{}, gen.ErrUnsupported
}
func (r *remoteNode) SpawnRegister(gen.Atom, gen.Atom, gen.ProcessOptions, ...any) (gen.PID, error) {
	return gen.PID{}, gen.ErrUnsupported
}
func (r *remoteNode) ApplicationStart(gen.Atom, gen.ApplicationOptions) error {
	return gen.ErrUnsupported
}
func (r *remoteNode) ApplicationStartTemporary(gen.Atom, gen.ApplicationOptions) error {
	return gen.ErrUnsupported
}
func (r *remoteNode) ApplicationStartTransient(gen.Atom, gen.ApplicationOptions) error {
	return gen.ErrUnsupported
}
func (r *remoteNode) ApplicationStartPermanent(gen.Atom, gen.ApplicationOptions) error {
	return gen.ErrUnsupported
}
func (r *remoteNode) ApplicationInfo(gen.Atom) (gen.ApplicationInfo, error) {
	return gen.ApplicationInfo{}, gen.ErrUnsupported
}
