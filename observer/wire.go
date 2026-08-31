package observer

import (
	"ergo.services/ergo/app/system/inspect"
	"ergo.services/ergo/gen"
)

// The browser contract; the frontend side of it is src/lib/wire.ts.
//
// Nothing from gen or inspect reaches the browser as it stands: every payload is mapped to a
// wire type here or in a sibling file, so a change in the framework cannot silently change
// what the frontend receives.

// anything without a mapping yet is passed through untouched
func wireFrom(payload any) any {
	switch m := payload.(type) {
	case inspect.MessageInspectProcessList:
		return wireProcessListFrom(m)
	case inspect.MessageInspectLog:
		return wireLogFrom(m)
	case inspect.MessageInspectTracing:
		return wireTracingFrom(m)
	case inspect.MessageInspectEventList:
		return wireEventListFrom(m)
	case inspect.MessageInspectEvent:
		return wireEventStreamFrom(m)
	case inspect.MessageInspectConnectionList:
		return wireConnectionListFrom(m)
	case inspect.MessageInspectConnection:
		return wireConnectionInfoFrom(m)
	case inspect.MessageInspectApplicationList:
		return wireApplicationListFrom(m)
	case inspect.MessageInspectNode:
		return wireNodeInfoFrom(m)
	case inspect.MessageInspectNetwork:
		return wireNetworkInfoFrom(m)
	case inspect.MessageInspectProcess:
		return wireProcessInfoFrom(m)
	case inspect.MessageInspectMeta:
		return wireMetaInfoFrom(m)
	}
	return payload
}

// bumped on every incompatible change; absent means the contract before versioning
const wireContractVersion = 2

type wireSubscribed struct {
	Key string `json:"key"`
}

type wireSubscriptionDown struct {
	Keys []string `json:"keys"`
	Type string   `json:"type"`
}

// the field names are the browser contract, not the layout of the framework struct they came from
type wirePID struct {
	Node     string `json:"Node"`
	ID       uint64 `json:"ID"`
	Creation int64  `json:"Creation"`
}

type wireRef struct {
	Node     string    `json:"Node"`
	Creation int64     `json:"Creation"`
	ID       [3]uint64 `json:"ID"`
}

type wireAlias = wireRef

type wireProcessID struct {
	Name string `json:"Name"`
	Node string `json:"Node"`
}

type wireEventRef struct {
	Name string `json:"Name"`
	Node string `json:"Node"`
}

func wirePIDFrom(pid gen.PID) wirePID {
	return wirePID{Node: string(pid.Node), ID: pid.ID, Creation: pid.Creation}
}

func wireRefFrom(ref gen.Ref) wireRef {
	return wireRef{Node: string(ref.Node), Creation: ref.Creation, ID: ref.ID}
}

func wireAliasFrom(alias gen.Alias) wireAlias {
	return wireRefFrom(gen.Ref(alias))
}

func wireProcessIDFrom(id gen.ProcessID) wireProcessID {
	return wireProcessID{Name: string(id.Name), Node: string(id.Node)}
}

func wireEventRefFrom(event gen.Event) wireEventRef {
	return wireEventRef{Name: string(event.Name), Node: string(event.Node)}
}

func wirePIDsFrom(list []gen.PID) []wirePID {
	if list == nil {
		return nil
	}
	out := make([]wirePID, len(list))
	for i, pid := range list {
		out[i] = wirePIDFrom(pid)
	}
	return out
}

func wireAliasesFrom(list []gen.Alias) []wireAlias {
	if list == nil {
		return nil
	}
	out := make([]wireAlias, len(list))
	for i, alias := range list {
		out[i] = wireAliasFrom(alias)
	}
	return out
}

func wireProcessIDsFrom(list []gen.ProcessID) []wireProcessID {
	if list == nil {
		return nil
	}
	out := make([]wireProcessID, len(list))
	for i, id := range list {
		out[i] = wireProcessIDFrom(id)
	}
	return out
}

func wireEventRefsFrom(list []gen.Event) []wireEventRef {
	if list == nil {
		return nil
	}
	out := make([]wireEventRef, len(list))
	for i, event := range list {
		out[i] = wireEventRefFrom(event)
	}
	return out
}

func pidOrNil(pid gen.PID) *wirePID {
	if pid == (gen.PID{}) {
		return nil
	}
	out := wirePIDFrom(pid)
	return &out
}

func aliasOrNil(alias gen.Alias) *wireAlias {
	if alias == (gen.Alias{}) {
		return nil
	}
	out := wireAliasFrom(alias)
	return &out
}
