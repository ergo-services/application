package observer

import (
	"ergo.services/ergo/app/system/inspect"
)

type wireMCPGetState struct {
	State map[string]string
	Error string `json:"Error,omitempty"`
}

type wireMCPGetGoroutines struct {
	Groups   []inspect.GoroutineGroup
	Total    int
	Filtered int
	Error    string `json:"Error,omitempty"`
}

func init() {
	mcpRegisterView(inspect.ResponseGetProcessState{}, func(value any) any {
		r, ok := value.(inspect.ResponseGetProcessState)
		if ok == false {
			return value
		}
		return wireMCPGetState{State: r.State, Error: mcpErrorText(r.Error)}
	})

	mcpRegisterView(inspect.ResponseGetMetaState{}, func(value any) any {
		r, ok := value.(inspect.ResponseGetMetaState)
		if ok == false {
			return value
		}
		return wireMCPGetState{State: r.State, Error: mcpErrorText(r.Error)}
	})

	mcpRegisterView(inspect.ResponseGetGoroutines{}, func(value any) any {
		r, ok := value.(inspect.ResponseGetGoroutines)
		if ok == false {
			return value
		}
		return wireMCPGetGoroutines{
			Groups: r.Groups, Total: r.Total, Filtered: r.Filtered,
			Error: mcpErrorText(r.Error),
		}
	})
}
