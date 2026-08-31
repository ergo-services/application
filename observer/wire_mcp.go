package observer

import (
	"fmt"
	"reflect"

	"ergo.services/ergo/gen"
)

// The agent contract. Field names mirror the framework type one for one, and values are passed
// through untouched, so what the legend says about a field stays true. The only change is the
// type of an identifier: it becomes the text an agent can hand back as an argument.
//
// A domain type without a view here reaches the agent as the framework marshals it.
var mcpViews = map[reflect.Type]func(any) any{}

// what the views answer with, so a plan registered for a view is not mistaken for a framework
// type still waiting for one
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

	// an accumulating lens answers with the batches it gathered: the list is ours, the values
	// in it are the framework's, so each one is viewed on its own
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

// read from the view, because the legend describes the answer and the answer is the view
func mcpLegendFor(view any) map[string]any {
	return mcpLegendOf(reflect.TypeOf(view))
}

// nil stays nil: the view mirrors what the framework holds, and an empty list is not the same
// answer as no list
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

// an error carries no exported field, so json renders it as {} and the reason is lost
func mcpErrorText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

// a target the framework types as any: a PID, a process name or an alias
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
