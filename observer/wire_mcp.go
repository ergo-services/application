package observer

import (
	"fmt"
	"reflect"

	"ergo.services/ergo/gen"
)

var mcpViews = map[reflect.Type]func(any) any{}

var mcpViewed = map[reflect.Type]bool{}

func mcpRegisterView(domain any, view func(any) any) {
	t := reflect.TypeOf(domain)
	mcpViews[t] = view
	mcpViewed[reflect.TypeOf(view(reflect.Zero(t).Interface()))] = true
}

func mcpViewOf(value any) any {
	if value == nil {
		return nil
	}

	if batches, ok := value.([]any); ok {
		out := make([]any, len(batches))
		for i, batch := range batches {
			out[i] = mcpViewOf(batch)
		}
		return out
	}

	if view, served := mcpViews[reflect.TypeOf(value)]; served {
		return view(value)
	}
	return value
}

func mcpLegendFor(view any) map[string]any {
	return mcpLegendOf(reflect.TypeOf(view))
}

func mcpPIDTexts(list []gen.PID) []string {
	if list == nil {
		return nil
	}
	out := make([]string, len(list))
	for i, pid := range list {
		out[i] = mcpPIDText(pid)
	}
	return out
}

func mcpAliasTexts(list []gen.Alias) []string {
	if list == nil {
		return nil
	}
	out := make([]string, len(list))
	for i, alias := range list {
		out[i] = mcpRefText(gen.Ref(alias))
	}
	return out
}

func mcpProcessIDTexts(list []gen.ProcessID) []string {
	if list == nil {
		return nil
	}
	out := make([]string, len(list))
	for i, id := range list {
		out[i] = mcpProcessIDText(id)
	}
	return out
}

func mcpErrorText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func mcpTargetText(target any) string {
	if target == nil {
		return ""
	}
	if text, ok := mcpIdentText(target); ok {
		return text
	}
	return fmt.Sprintf("%v", target)
}

func mcpEventTexts(list []gen.Event) []string {
	if list == nil {
		return nil
	}
	out := make([]string, len(list))
	for i, event := range list {
		out[i] = mcpEventText(event)
	}
	return out
}
