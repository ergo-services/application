package mcp

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"ergo.services/ergo/gen"
)

func registerEventTools(r *toolRegistry) {
	r.register(ToolDefinition{
		Name:        "event_list",
		Description: "Returns registered events with statistics (event name, producer PID, subscribers count, messages published/local sent/remote sent, buffer size/current, notify mode). Supports filtering by all fields. Numeric filters use minimum threshold.",
		InputSchema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"limit": {
					"type": "integer",
					"description": "Maximum number of events to return (default: 100, 0 = all)"
				},
				"name": {
					"type": "string",
					"description": "Filter by event name (substring match)"
				},
				"notify": {
					"type": "boolean",
					"description": "Filter by Notify mode (true = only events with Notify enabled)"
				},
				"min_subscribers": {
					"type": "integer",
					"description": "Minimum subscriber count"
				},
				"max_subscribers": {
					"type": "integer",
					"description": "Maximum subscriber count (e.g. 0 to find events with no subscribers)"
				},
				"min_published": {
					"type": "integer",
					"description": "Minimum MessagesPublished (cumulative)"
				},
				"min_local_sent": {
					"type": "integer",
					"description": "Minimum MessagesLocalSent (cumulative local deliveries)"
				},
				"min_remote_sent": {
					"type": "integer",
					"description": "Minimum MessagesRemoteSent (cumulative remote sends)"
				},
				"has_buffer": {
					"type": "boolean",
					"description": "Filter by buffer presence (true = only buffered events)"
				},
				"max_published": {
					"type": "integer",
					"description": "Maximum MessagesPublished (e.g. 0 to find events that never published)"
				},
				"producer": {
					"type": "string",
					"description": "Filter by producer process: registered name (substring match) or PID string"
				},
				"utilization_state": {
					"type": "string",
					"description": "Filter by derived utilization state: active (published>0 and subscribers>0), on_demand (Notify enabled, waiting), idle (no Notify, no publishes, no subscribers), no_subscribers (published>0, subscribers=0, no Notify), no_publishing (subscribers>0, published=0, no Notify)",
					"enum": ["active", "on_demand", "idle", "no_subscribers", "no_publishing"]
				},
				"sort_by": {
					"type": "string",
					"description": "Sort results by field (descending): subscribers, published, local_sent, remote_sent",
					"enum": ["subscribers", "published", "local_sent", "remote_sent"]
				}
			}
		}`),
		handler: toolEventList,
	})

	r.register(ToolDefinition{
		Name:        "event_info",
		Description: "Returns detailed information about a specific registered event.",
		InputSchema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"name": {
					"type": "string",
					"description": "Event name"
				}
			},
			"required": ["name"]
		}`),
		handler: toolEventInfo,
	})
}

type eventListParams struct {
	Limit            int    `json:"limit"`
	Name             string `json:"name"`
	Producer         string `json:"producer"`
	Notify           *bool  `json:"notify"`
	MinSubscribers   *int64 `json:"min_subscribers"`
	MaxSubscribers   *int64 `json:"max_subscribers"`
	MinPublished     int64  `json:"min_published"`
	MaxPublished     *int64 `json:"max_published"`
	MinLocalSent     int64  `json:"min_local_sent"`
	MinRemoteSent    int64  `json:"min_remote_sent"`
	HasBuffer        *bool  `json:"has_buffer"`
	UtilizationState string `json:"utilization_state"`
	SortBy           string `json:"sort_by"`
}

func eventUtilizationState(info gen.EventInfo) string {
	if info.MessagesPublished > 0 && info.Subscribers > 0 {
		return "active"
	}
	if info.Notify {
		return "on_demand"
	}
	if info.MessagesPublished == 0 && info.Subscribers == 0 {
		return "idle"
	}
	if info.MessagesPublished > 0 && info.Subscribers == 0 {
		return "no_subscribers"
	}
	if info.Subscribers > 0 && info.MessagesPublished == 0 {
		return "no_publishing"
	}
	return "unknown"
}

func toolEventList(w gen.Process, params json.RawMessage) (any, error) {
	var p eventListParams
	if len(params) > 0 {
		json.Unmarshal(params, &p)
	}
	if p.Limit == 0 {
		p.Limit = 100
	}

	collectAll := p.SortBy != ""
	var events []gen.EventInfo

	// build producer name cache for filtering
	var producerNames map[gen.PID]gen.Atom
	if p.Producer != "" {
		producerNames = make(map[gen.PID]gen.Atom)
		w.Node().ProcessRangeShortInfo(func(si gen.ProcessShortInfo) bool {
			if si.Name != "" {
				producerNames[si.PID] = si.Name
			}
			return true
		})
	}

	w.Node().EventRangeInfo(func(info gen.EventInfo) bool {
		// string filters
		if p.Name != "" && strings.Contains(string(info.Event.Name), p.Name) == false {
			return true
		}

		// producer filter: match by PID string or by registered name (substring)
		if p.Producer != "" {
			pidStr := info.Producer.String()
			nameStr := string(producerNames[info.Producer])
			if strings.Contains(pidStr, p.Producer) == false && strings.Contains(nameStr, p.Producer) == false {
				return true
			}
		}

		// utilization state filter
		if p.UtilizationState != "" && eventUtilizationState(info) != p.UtilizationState {
			return true
		}

		// bool filters
		if p.Notify != nil && info.Notify != *p.Notify {
			return true
		}
		if p.HasBuffer != nil {
			hasBuffer := info.BufferSize > 0
			if hasBuffer != *p.HasBuffer {
				return true
			}
		}

		// numeric filters
		if p.MinSubscribers != nil && info.Subscribers < *p.MinSubscribers {
			return true
		}
		if p.MaxSubscribers != nil && info.Subscribers > *p.MaxSubscribers {
			return true
		}
		if p.MinPublished > 0 && info.MessagesPublished < p.MinPublished {
			return true
		}
		if p.MaxPublished != nil && info.MessagesPublished > *p.MaxPublished {
			return true
		}
		if p.MinLocalSent > 0 && info.MessagesLocalSent < p.MinLocalSent {
			return true
		}
		if p.MinRemoteSent > 0 && info.MessagesRemoteSent < p.MinRemoteSent {
			return true
		}

		events = append(events, info)
		if collectAll == false && p.Limit > 0 && len(events) >= p.Limit {
			return false
		}
		return true
	})

	// Sort descending
	if p.SortBy != "" && len(events) > 0 {
		sort.Slice(events, func(i, j int) bool {
			return eventSortValue(events[i], p.SortBy) > eventSortValue(events[j], p.SortBy)
		})
		if p.Limit > 0 && len(events) > p.Limit {
			events = events[:p.Limit]
		}
	}

	text, err := marshalResult(events)
	if err != nil {
		return nil, err
	}
	return textResult(text), nil
}

func eventSortValue(info gen.EventInfo, field string) float64 {
	switch field {
	case "subscribers":
		return float64(info.Subscribers)
	case "published":
		return float64(info.MessagesPublished)
	case "local_sent":
		return float64(info.MessagesLocalSent)
	case "remote_sent":
		return float64(info.MessagesRemoteSent)
	}
	return 0
}

type eventInfoParams struct {
	Name string `json:"name"`
}

func toolEventInfo(w gen.Process, params json.RawMessage) (any, error) {
	var p eventInfoParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, fmt.Errorf("invalid params: %w", err)
	}

	event := gen.Event{Name: gen.Atom(p.Name), Node: w.Node().Name()}
	info, err := w.Node().EventInfo(event)
	if err != nil {
		return nil, fmt.Errorf("event_info: %w", err)
	}
	text, err := marshalResult(info)
	if err != nil {
		return nil, err
	}
	return textResult(text), nil
}
