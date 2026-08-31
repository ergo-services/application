package observer

import (
	"ergo.services/ergo/app/system/inspect"
	"ergo.services/ergo/gen"
)

type wireMCPEventInfo struct {
	CreatedAt          int64
	Event              string
	Producer           string
	BufferSize         int `unit:"messages"`
	CurrentBuffer      int
	Notify             bool
	Open               bool
	Subscribers        int64
	MessagesPublished  int64
	MessagesLocalSent  int64
	MessagesRemoteSent int64
	LastPublishedAt    int64
}

func wireMCPEventInfoOf(info gen.EventInfo) wireMCPEventInfo {
	return wireMCPEventInfo{
		CreatedAt:          info.CreatedAt,
		Event:              mcpEventText(info.Event),
		Producer:           mcpPIDText(info.Producer),
		BufferSize:         info.BufferSize,
		CurrentBuffer:      info.CurrentBuffer,
		Notify:             info.Notify,
		Open:               info.Open,
		Subscribers:        info.Subscribers,
		MessagesPublished:  info.MessagesPublished,
		MessagesLocalSent:  info.MessagesLocalSent,
		MessagesRemoteSent: info.MessagesRemoteSent,
		LastPublishedAt:    info.LastPublishedAt,
	}
}

func wireMCPEventRows(list []gen.EventInfo) []wireMCPEventInfo {
	if list == nil {
		return nil
	}
	out := make([]wireMCPEventInfo, len(list))
	for i, info := range list {
		out[i] = wireMCPEventInfoOf(info)
	}
	return out
}

type wireMCPEvent struct {
	Node        gen.Atom
	Info        wireMCPEventInfo
	Entries     []wireMCPEventEntry
	Suppressed  int64
	Closed      bool
	Reason      string
	Watching    bool
	WatchReason string
}

type wireMCPEventList struct {
	Node   gen.Atom
	Events []wireMCPEventInfo
}

type wireMCPGetEvent struct {
	Node  gen.Atom
	Info  wireMCPEventInfo
	Error string `json:"Error,omitempty"`
}

type wireMCPGetEventList struct {
	Node   gen.Atom
	Events []wireMCPEventInfo
	Error  string `json:"Error,omitempty"`
}

func init() {
	mcpRegisterView(inspect.MessageInspectEvent{}, func(value any) any {
		m, ok := value.(inspect.MessageInspectEvent)
		if ok == false {
			return value
		}
		return wireMCPEvent{
			Node: m.Node, Info: wireMCPEventInfoOf(m.Info),
			Entries:    wireMCPEventEntries(m.Entries),
			Suppressed: m.Suppressed, Closed: m.Closed, Reason: m.Reason,
			Watching: m.Watching, WatchReason: m.WatchReason,
		}
	})

	mcpRegisterView(inspect.MessageInspectEventList{}, func(value any) any {
		m, ok := value.(inspect.MessageInspectEventList)
		if ok == false {
			return value
		}
		return wireMCPEventList{Node: m.Node, Events: wireMCPEventRows(m.Events)}
	})

	mcpRegisterView(inspect.ResponseGetEvent{}, func(value any) any {
		r, ok := value.(inspect.ResponseGetEvent)
		if ok == false {
			return value
		}
		return wireMCPGetEvent{
			Node: r.Node, Info: wireMCPEventInfoOf(r.Info), Error: mcpErrorText(r.Error),
		}
	})

	mcpRegisterView(inspect.ResponseGetEventList{}, func(value any) any {
		r, ok := value.(inspect.ResponseGetEventList)
		if ok == false {
			return value
		}
		return wireMCPGetEventList{
			Node: r.Node, Events: wireMCPEventRows(r.Events), Error: mcpErrorText(r.Error),
		}
	})
}
