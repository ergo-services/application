package observer

import (
	"ergo.services/ergo/app/system/inspect"
	"ergo.services/ergo/gen"
)

type wireEvent struct {
	Name               string  `json:"n"`
	Node               string  `json:"nd,omitempty"`
	CreatedAt          int64   `json:"c,omitempty"`
	Producer           wirePID `json:"pr"`
	BufferSize         int     `json:"bs,omitempty"`
	CurrentBuffer      int     `json:"cb,omitempty"`
	Notify             bool    `json:"nt,omitempty"`
	Open               bool    `json:"o,omitempty"`
	Subscribers        int64   `json:"s,omitempty"`
	MessagesPublished  int64   `json:"mp,omitempty"`
	MessagesLocalSent  int64   `json:"mls,omitempty"`
	MessagesRemoteSent int64   `json:"mrs,omitempty"`
	LastPublishedAt    int64   `json:"lp,omitempty"`
}

type wireEventList struct {
	Node   string      `json:"nd"`
	Events []wireEvent `json:"ev"`
}

type wireEventEntry struct {
	Timestamp int64  `json:"t"`
	Type      string `json:"ty,omitempty"`
	Message   string `json:"msg,omitempty"`
	Verbose   string `json:"vb,omitempty"`
}

type wireEventStream struct {
	Node        string           `json:"nd"`
	Info        wireEvent        `json:"in"`
	Entries     []wireEventEntry `json:"en,omitempty"`
	Suppressed  int64            `json:"sup,omitempty"`
	Closed      bool             `json:"cl,omitempty"`
	Reason      string           `json:"rs,omitempty"`
	Watching    bool             `json:"w,omitempty"`
	WatchReason string           `json:"wr,omitempty"`
}

func wireEventFrom(info gen.EventInfo) wireEvent {
	return wireEvent{
		Name:               string(info.Event.Name),
		Node:               string(info.Event.Node),
		CreatedAt:          info.CreatedAt,
		Producer:           wirePIDFrom(info.Producer),
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

func wireEventListFrom(m inspect.MessageInspectEventList) wireEventList {
	out := wireEventList{Node: string(m.Node), Events: make([]wireEvent, 0, len(m.Events))}
	for _, info := range m.Events {
		out.Events = append(out.Events, wireEventFrom(info))
	}
	return out
}

func wireEventStreamFrom(m inspect.MessageInspectEvent) wireEventStream {
	out := wireEventStream{
		Node:        string(m.Node),
		Info:        wireEventFrom(m.Info),
		Suppressed:  m.Suppressed,
		Closed:      m.Closed,
		Reason:      m.Reason,
		Watching:    m.Watching,
		WatchReason: m.WatchReason,
	}
	for _, entry := range m.Entries {
		out.Entries = append(out.Entries, wireEventEntry{
			Timestamp: entry.Timestamp,
			Type:      entry.Type,
			Message:   entry.Message,
			Verbose:   entry.Verbose,
		})
	}
	return out
}
