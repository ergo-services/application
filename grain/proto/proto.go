// Package proto is grain's virtual network stack. It presents a grain domain as
// a remote Ergo node (atom "grain_<domain>") over a private loopback connection,
// so any process can address a grain as gen.ProcessID{Node: "grain_<domain>",
// Name: key} with plain Send/Call. The connection never moves bytes: every
// routing call resolves the key to the live grain PID (via a Host) and re-routes
// to that PID locally.
//
// Step 1 scope: the stack identity (proto + handshake + versions) and the
// connection skeleton (lifecycle + stubbed routing). Bring-up wiring and the
// resolve/route logic land in later steps.
package proto

import (
	"encoding/binary"
	"io"
	"net"
	"strconv"
	"sync"
	"time"

	"ergo.services/ergo/gen"
)

// Distinct versions so this stack coexists with the default EDF proto/handshake
// (which are "ENP:R1"/"EHS:R1"); the network keys handlers by Version().Str().
var (
	ProtoVersion     = gen.Version{Name: "GRAINP", Release: "R1"}
	HandshakeVersion = gen.Version{Name: "GRAINH", Release: "R1"}
)

// Host is what a grain domain provides to its virtual connection: resolution of
// a key to the live grain PID on this node. Implemented in package grain,
// registered per vnode with RegisterHost. Whereis is a lock-free read; Activate
// ensures the grain is live (activating on demand) and returns its PID.
type Host interface {
	Whereis(key string) (gen.PID, bool)
	Activate(key string) (gen.PID, error)
}

// hosts maps a vnode atom ("grain_<domain>") to its Host.
var hosts sync.Map // gen.Atom -> Host

// RegisterHost binds a Host to a vnode. Called by grain when standing up a domain.
func RegisterHost(vnode gen.Atom, h Host) { hosts.Store(vnode, h) }

// UnregisterHost removes a vnode's Host on teardown.
func UnregisterHost(vnode gen.Atom) { hosts.Delete(vnode) }

func hostFor(vnode gen.Atom) (Host, bool) {
	v, ok := hosts.Load(vnode)
	if ok == false {
		return nil, false
	}
	return v.(Host), true
}

// Proto returns the grain NetworkProto to register with gen.Network.
func Proto() gen.NetworkProto { return grainProto{} }

// Handshake returns the grain NetworkHandshake to register with gen.Network.
func Handshake() gen.NetworkHandshake { return grainHandshake{} }

var (
	_ gen.NetworkProto     = grainProto{}
	_ gen.NetworkHandshake = grainHandshake{}
)

// grainProto builds and serves grain connections. It carries no bytes: Serve
// parks the vestigial socket until the connection is torn down.
type grainProto struct{}

func (grainProto) Version() gen.Version { return ProtoVersion }

func (grainProto) NewConnection(core gen.Core, result gen.HandshakeResult, log gen.Log) (gen.Connection, error) {
	host, ok := hostFor(result.Peer)
	if ok == false {
		return nil, gen.ErrNoRoute
	}
	c := &grainConn{
		core:     core,
		vnode:    result.Peer,
		realNode: core.Name(),
		creation: result.PeerCreation,
		host:     host,
		log:      log,
		done:     make(chan struct{}),
	}
	return c, nil
}

// Serve is immortal: it parks until Terminate closes done. Returning would make
// the node fire RouteNodeDown for the whole vnode (node/network.go:1157,1770),
// tearing every by-name monitor/link in the domain. It ignores dial (no pool).
func (grainProto) Serve(conn gen.Connection, _ gen.NetworkDial) error {
	<-conn.(*grainConn).done
	return nil
}

// grainHandshake completes a loopback handshake. Only Start (the dial side) is
// exercised: grain runs its own listener that writes the vnode intro, and Start
// reads it. The acceptor-side steps are never called by the framework here.
type grainHandshake struct{}

func (grainHandshake) Version() gen.Version { return HandshakeVersion }

func (grainHandshake) NetworkFlags() gen.NetworkFlags {
	return gen.NetworkFlags{Enable: true, EnableImportantDelivery: true}
}

// Start reads the length-prefixed vnode intro written by the domain's listener
// and reports it as the peer. Custom is left nil so no framework keepalive is
// attached (liveness is the Serve done-channel).
func (grainHandshake) Start(node gen.NodeHandshake, conn net.Conn, _ gen.HandshakeOptions) (gen.HandshakeResult, error) {
	var n uint16
	if err := binary.Read(conn, binary.BigEndian, &n); err != nil {
		return gen.HandshakeResult{}, err
	}
	buf := make([]byte, n)
	if _, err := io.ReadFull(conn, buf); err != nil {
		return gen.HandshakeResult{}, err
	}
	vnode := gen.Atom(buf)
	flags := gen.NetworkFlags{Enable: true, EnableImportantDelivery: true}
	return gen.HandshakeResult{
		HandshakeVersion: HandshakeVersion,
		ConnectionID:     vnode.String() + "-" + strconv.FormatInt(time.Now().UnixNano(), 36),
		Peer:             vnode,
		PeerCreation:     node.Creation(), // stable, non-zero for the domain lifetime
		PeerVersion:      ProtoVersion,
		PeerFlags:        flags,
		NodeFlags:        flags,
		// PoolSize/PoolDSN zero: no connection pool, so Join/redial never run.
	}, nil
}

// Acceptor-side steps: unused (our own listener answers the dial).
func (grainHandshake) Negotiate(gen.NodeHandshake, net.Conn, gen.HandshakeOptions) (gen.HandshakeResult, error) {
	return gen.HandshakeResult{}, gen.ErrUnsupported
}
func (grainHandshake) Accept(gen.NodeHandshake, net.Conn, gen.HandshakeOptions, gen.HandshakeResult) (gen.HandshakeResult, error) {
	return gen.HandshakeResult{}, gen.ErrUnsupported
}
func (grainHandshake) Join(gen.NodeHandshake, net.Conn, string, gen.HandshakeOptions) ([]byte, error) {
	return nil, nil
}
func (grainHandshake) Reject(net.Conn, string) error { return nil }
