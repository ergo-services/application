package observer

import (
	"ergo.services/ergo/app/system/inspect"
	"ergo.services/ergo/gen"
)

type wireMCPGetProcessLookup struct {
	PID   string
	Name  gen.Atom
	State string
	Error string `json:"Error,omitempty"`
}

func init() {
	mcpRegisterView(inspect.ResponseGetProcessLookup{}, func(value any) any {
		r, ok := value.(inspect.ResponseGetProcessLookup)
		if ok == false {
			return value
		}
		return wireMCPGetProcessLookup{
			PID: mcpPIDText(r.PID), Name: r.Name, State: r.State.String(),
			Error: mcpErrorText(r.Error),
		}
	})
}
