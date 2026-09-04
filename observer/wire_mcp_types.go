package observer

import (
	"ergo.services/ergo/app/system/inspect"
	"ergo.services/ergo/gen"
)

type wireMCPRegisteredTypeStats struct {
	Enabled      bool `sentinel:"false = the node was built without -tags=typestats, so every counter stays zero"`
	Encoded      int64
	Decoded      int64
	EncodedBytes int64 `unit:"bytes"`
	DecodedBytes int64 `unit:"bytes"`
}

type wireMCPRegisteredTypeInfo struct {
	ID           uint64
	Name         string
	Kind         string
	Schema       string
	Proto        string
	MinSize      uint32 `unit:"bytes"`
	SizeVariable bool
	Stats        wireMCPRegisteredTypeStats
}

func wireMCPTypeRows(list []gen.RegisteredTypeInfo) []wireMCPRegisteredTypeInfo {
	if list == nil {
		return nil
	}
	out := make([]wireMCPRegisteredTypeInfo, len(list))
	for i, info := range list {
		out[i] = wireMCPRegisteredTypeInfo{
			ID:           info.ID,
			Name:         info.Name,
			Kind:         info.Kind,
			Schema:       info.Schema,
			Proto:        info.Proto,
			MinSize:      info.MinSize,
			SizeVariable: info.SizeVariable,
			Stats: wireMCPRegisteredTypeStats{
				Enabled:      info.Stats.Enabled,
				Encoded:      info.Stats.Encoded,
				Decoded:      info.Stats.Decoded,
				EncodedBytes: info.Stats.EncodedBytes,
				DecodedBytes: info.Stats.DecodedBytes,
			},
		}
	}
	return out
}

type wireMCPGetTypes struct {
	Types     []wireMCPRegisteredTypeInfo
	Truncated int
	Error     string `json:"Error,omitempty"`

	Legend map[string]any `json:"services.ergo/legend,omitempty"`
}

var wireMCPTypesLegend = mcpLegendFor(wireMCPGetTypes{})

type wireMCPRegisteredErrorInfo struct {
	ID    uint16
	Text  string
	Proto string
}

type wireMCPGetErrors struct {
	Errors    []wireMCPRegisteredErrorInfo
	Truncated int
	Error     string `json:"Error,omitempty"`
}

func wireMCPErrorRows(list []gen.RegisteredErrorInfo) []wireMCPRegisteredErrorInfo {
	if list == nil {
		return nil
	}
	out := make([]wireMCPRegisteredErrorInfo, len(list))
	for i, info := range list {
		out[i] = wireMCPRegisteredErrorInfo{
			ID:    info.ID,
			Text:  info.Text,
			Proto: info.Proto,
		}
	}
	return out
}

type wireMCPRegisteredAtomInfo struct {
	ID    uint16
	Name  gen.Atom
	Proto string
}

type wireMCPGetAtoms struct {
	Atoms     []wireMCPRegisteredAtomInfo
	Truncated int
	Error     string `json:"Error,omitempty"`
}

func wireMCPAtomRows(list []gen.RegisteredAtomInfo) []wireMCPRegisteredAtomInfo {
	if list == nil {
		return nil
	}
	out := make([]wireMCPRegisteredAtomInfo, len(list))
	for i, info := range list {
		out[i] = wireMCPRegisteredAtomInfo{
			ID:    info.ID,
			Name:  info.Name,
			Proto: info.Proto,
		}
	}
	return out
}

func init() {
	mcpRegisterView(inspect.ResponseGetTypes{}, func(value any) any {
		r, ok := value.(inspect.ResponseGetTypes)
		if ok == false {
			return value
		}
		return wireMCPGetTypes{
			Types: wireMCPTypeRows(r.Types), Truncated: r.Truncated,
			Error:  mcpErrorText(r.Error),
			Legend: wireMCPTypesLegend,
		}
	})

	mcpRegisterView(inspect.ResponseGetErrors{}, func(value any) any {
		r, ok := value.(inspect.ResponseGetErrors)
		if ok == false {
			return value
		}
		return wireMCPGetErrors{
			Errors: wireMCPErrorRows(r.Errors), Truncated: r.Truncated,
			Error: mcpErrorText(r.Error),
		}
	})

	mcpRegisterView(inspect.ResponseGetAtoms{}, func(value any) any {
		r, ok := value.(inspect.ResponseGetAtoms)
		if ok == false {
			return value
		}
		return wireMCPGetAtoms{
			Atoms: wireMCPAtomRows(r.Atoms), Truncated: r.Truncated,
			Error: mcpErrorText(r.Error),
		}
	})
}
