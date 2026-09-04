package observer

import (
	"fmt"

	"ergo.services/ergo/app/system/manage"
)

var mutationTools = []mcpTool{
	{
		Name: "send",
		Capabilities: []string{manage.CapSend,
			manage.CapSendMeta},
		Title:         "Send a message",
		Mutating:      true,
		NotIdempotent: true,
		Build:         buildSend,
		Description: "Deliver one message to a process or to a meta. Give pid for a process, " +
			"alias for a meta. Only a single value goes through, because a message of a type " +
			"the observer does not know cannot be built here: name the type and the receiver " +
			"gets exactly that Go type. The receiver decides what it accepts, and a message it " +
			"does not read is dropped or takes it down, so read its state first.",
		Schema: mcpSchema{
			Type:     "object",
			Required: []string{nodeArgument, "type", "value"},
			Properties: map[string]mcpSchemaItem{
				nodeArgument: nodeProperty(),
				"pid":        pidProperty("Process id."),
				"alias":      aliasProperty("Meta alias, instead of pid."),
				"type": {Type: "string", Enum: sendTypes,
					Description: "The Go type the receiver gets."},
				"value": {Type: "string",
					Description: "The value as text, always: 42 for a number, true or false " +
						"for bool, base64 for binary, RFC3339 for time, and an identifier as " +
						"any answer writes it. Text keeps a large integer exact."},
				"priority": {Type: "string", Enum: priorityNames,
					Description: "Which mailbox queue the message lands in, for pid only. " +
						"Default: normal. high and max skip ahead of what is already queued, " +
						"so a slow process handles them first."},
			},
		},
	},
	{
		Name:         "kill",
		Capabilities: []string{manage.CapKill},
		Title:        "Kill a process",
		Action:       "kill",
		Mutating:     true,
		Destructive:  true,
		Description: "Terminate a process at once, without letting it run Terminate. Prefer " +
			"send_exit unless it is stuck.",
		Schema: mcpSchema{
			Type:     "object",
			Required: []string{nodeArgument, "pid"},
			Properties: map[string]mcpSchemaItem{
				nodeArgument: nodeProperty(),
				"pid":        pidProperty("Process id."),
			},
		},
	},
	{
		Name: "send_exit",
		Capabilities: []string{manage.CapSendExit,
			manage.CapSendExitMeta},
		Title:       "Send an exit signal",
		Action:      "send_exit",
		Mutating:    true,
		Destructive: true,
		Description: "Ask a process or a meta to stop, so it runs Terminate. Give pid for a " +
			"process, alias for a meta.",
		Schema: mcpSchema{
			Type:     "object",
			Required: []string{nodeArgument},
			Properties: map[string]mcpSchemaItem{
				nodeArgument: nodeProperty(),
				"pid":        pidProperty("Process id."),
				"alias":      aliasProperty("Meta alias, instead of pid."),
				"reason":     {Type: "string", Description: "Exit reason. Default: normal."},
			},
		},
	},
	{
		Name: "log_level_set",
		Capabilities: []string{
			manage.CapSetLogLevel,
			manage.CapSetProcessLogLevel,
			manage.CapSetMetaLogLevel,
		},
		Title:    "Set a log level",
		Action:   "set_log_level",
		Mutating: true,
		Description: "Change the log level of the node, of one process or of one meta: " +
			"target=process with pid, target=meta with alias, nothing for the node.",
		Schema: mcpSchema{
			Type:     "object",
			Required: []string{nodeArgument, "level"},
			Properties: map[string]mcpSchemaItem{
				nodeArgument: nodeProperty(),
				"level":      {Type: "string", Description: "Level to set.", Enum: logLevelNames},
				"target": {Type: "string", Enum: []string{"process", "meta"},
					Description: "What to change. Absent changes the node itself."},
				"pid":   pidProperty("Process id when target is process."),
				"alias": aliasProperty("Meta alias when target is meta."),
			},
		},
	},
	{
		Name: "tracing_sampler_set",
		Capabilities: []string{manage.CapSetNodeTracingSampler,
			manage.CapSetProcessTracingSampler},
		Title:    "Set a tracing sampler",
		Mutating: true,
		Build:    buildTracingSampler,
		Description: "Turn tracing on or off for the node, or for one process when pid is " +
			"given. Sampling makes the node do work it was not doing.",
		Schema: mcpSchema{
			Type:     "object",
			Required: []string{nodeArgument, "type"},
			Properties: map[string]mcpSchemaItem{
				nodeArgument: nodeProperty(),
				"type": {Type: "string", Enum: samplerTypes,
					Description: "Sampler to install. disable turns sampling off."},
				"rate": {Type: "number",
					Description: "Share of spans to keep, for ratio: 0.1 is one in ten."},
				"limit": {Type: "integer",
					Description: "Spans per second, for rate_limit."},
				"pid": pidProperty("One process instead of the node."),
			},
		},
	},
	{
		Name: "process_tune",
		Capabilities: []string{
			manage.CapSetProcessSendPriority,
			manage.CapSetProcessCompression,
			manage.CapSetProcessCompressionType,
			manage.CapSetProcessCompressionLevel,
			manage.CapSetProcessCompressionThreshold,
			manage.CapSetProcessKeepNetworkOrder,
			manage.CapSetProcessImportantDelivery,
			manage.CapSetMetaSendPriority,
		},
		Title:    "Tune one process setting",
		Mutating: true,
		Build:    buildProcessTune,
		Description: "Change one delivery setting of a process, or the send priority of a meta. " +
			"Name the knob and fill the field it reads: send_priority reads priority, " +
			"compression_type reads type, compression_level reads level, compression_threshold " +
			"reads threshold, and the three switches read enabled.",
		Schema: mcpSchema{
			Type:     "object",
			Required: []string{nodeArgument, "knob"},
			Properties: map[string]mcpSchemaItem{
				nodeArgument: nodeProperty(),
				"knob":       {Type: "string", Description: "Which setting to change.", Enum: knobNames},
				"pid":        pidProperty("Process id."),
				"alias":      aliasProperty("Meta alias, for send_priority only."),
				"enabled": {Type: "boolean",
					Description: "For compression, keep_network_order, important_delivery."},
				"priority":  {Type: "string", Description: "For send_priority.", Enum: priorityNames},
				"type":      {Type: "string", Description: "For compression_type.", Enum: compressionTypes},
				"level":     {Type: "string", Description: "For compression_level.", Enum: compressionLevels},
				"threshold": {Type: "integer", Description: "For compression_threshold, in bytes."},
			},
		},
	},
	{
		Name:         "app_start",
		Capabilities: []string{manage.CapAppStart},
		Title:        "Start an application",
		Action:       "app_start",
		Mutating:     true,
		Description:  "Start an application already loaded on the node.",
		Schema: appSchema("Application to start.", map[string]mcpSchemaItem{
			"mode": {Type: "string", Enum: applicationModes,
				Description: "Restart mode. Absent keeps the mode the application was loaded with."},
		}),
	},
	{
		Name:         "app_stop",
		Capabilities: []string{manage.CapAppStop},
		Title:        "Stop an application",
		Action:       "app_stop",
		Mutating:     true,
		Destructive:  true,
		Description:  "Stop a running application.",
		Schema: appSchema("Application to stop.", map[string]mcpSchemaItem{
			"force": {Type: "boolean",
				Description: "Stop without waiting for the children to finish."},
		}),
	},
	{
		Name:         "app_unload",
		Capabilities: []string{manage.CapAppUnload},
		Title:        "Unload an application",
		Action:       "app_unload",
		Mutating:     true,
		Destructive:  true,
		Description:  "Unload an application, so the node no longer knows it.",
		Schema:       appSchema("Application to unload.", nil),
	},
}

func appSchema(what string, extra map[string]mcpSchemaItem) mcpSchema {
	properties := map[string]mcpSchemaItem{
		nodeArgument: nodeProperty(),
		"name":       {Type: "string", Description: what},
	}
	for name, item := range extra {
		properties[name] = item
	}
	return mcpSchema{
		Type:       "object",
		Required:   []string{nodeArgument, "name"},
		Properties: properties,
	}
}

var knobs = map[string]struct{ action, field, argument string }{
	"send_priority":         {"set_process_send_priority", "priority", "priority"},
	"compression":           {"set_process_compression", "enabled", "enabled"},
	"compression_type":      {"set_process_compression_type", "type", "type"},
	"compression_level":     {"set_process_compression_level", "level", "level"},
	"compression_threshold": {"set_process_compression_threshold", "threshold", "threshold"},
	"keep_network_order":    {"set_process_keep_network_order", "enabled", "order"},
	"important_delivery":    {"set_process_important_delivery", "enabled", "important"},
}

func buildSend(args map[string]any) (string, map[string]any, error) {
	message, err := argSendMessage(args)
	if err != nil {
		return "", nil, err
	}

	out := make(map[string]any, len(args)+1)
	for name, given := range args {
		out[name] = given
	}
	out["message"] = message
	return "send", out, nil
}

func buildTracingSampler(args map[string]any) (string, map[string]any, error) {
	if hasArg(args, "pid") {
		return "set_process_tracing_sampler", args, nil
	}
	return "set_node_tracing_sampler", args, nil
}

func buildProcessTune(args map[string]any) (string, map[string]any, error) {
	knob, _ := args["knob"].(string)
	entry, known := knobs[knob]
	if known == false {
		return "", nil, fmt.Errorf("there is no knob %q", knob)
	}
	if hasArg(args, entry.field) == false {
		return "", nil, fmt.Errorf("knob %s reads %s, and it is not set", knob, entry.field)
	}

	action := entry.action
	if knob == "send_priority" && hasArg(args, "alias") {
		action = "set_meta_send_priority"
	}

	out := make(map[string]any, len(args)+1)
	for name, given := range args {
		out[name] = given
	}
	out[entry.argument] = args[entry.field]
	return action, out, nil
}
