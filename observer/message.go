package observer

import "ergo.services/ergo/gen"

type messageCommand struct {
	ID      gen.Alias
	CID     string
	Command string
	Name    string
	Args    map[string]any
}

type messageError struct {
	CID   string
	Error string
}

type messageResult struct {
	CID    string
	Result any
}

type messageEvent struct {
	Timestamp int64
	Event     gen.Event
	Message   any
}
