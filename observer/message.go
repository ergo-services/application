package observer

// commandRequest sent via Call from POST worker to session actor
type commandRequest struct {
	Command string         // "subscribe", "unsubscribe", "switch"
	Type    string         // subscription type (node_info, process_list, etc.)
	Args    map[string]any // type-specific arguments
}

// apiResponse returned from session actor to POST worker
type apiResponse struct {
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
	Data  any    `json:"data,omitempty"`
}

// actionRequest sent via Call from POST worker to session actor for do/* commands
type actionRequest struct {
	Action string         // "send", "send_exit", "kill", "set_log_level", etc.
	Args   map[string]any
}
