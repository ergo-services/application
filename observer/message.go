package observer

type commandRequest struct {
	Command string         // "subscribe", "unsubscribe", "switch"
	Type    string         // subscription type (node_info, process_list, etc.)
	Args    map[string]any // type-specific arguments
	Subject string         // who the guard says is calling
}

type apiResponse struct {
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
	Data  any    `json:"data,omitempty"`
}

// EnrollRequest asks the manager to confirm that this observer holds the enrollment
// secret. The secret burns on the first success.
type EnrollRequest struct {
	Token string
}

// EnrollResponse names the cluster on success. Burned says the secret was already spent.
type EnrollResponse struct {
	ClusterID string
	Burned    bool
	Error     error
}

type actionRequest struct {
	Action  string // "send", "send_exit", "kill", "set_log_level", etc.
	Args    map[string]any
	Subject string // who the guard says is calling
}
