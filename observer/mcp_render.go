package observer

import (
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"time"

	"ergo.services/ergo/app/system/inspect"
	"ergo.services/ergo/gen"
)

const (
	mcpMimeJSON   = "application/json"
	mcpMimeStream = "text/event-stream"
)

type cacheTTLKey struct{}

func mcpCacheTTL(request *http.Request) int {
	if request == nil {
		return int(defaultMCPCacheTTL.Milliseconds())
	}
	ttl, carried := request.Context().Value(cacheTTLKey{}).(time.Duration)
	if carried == false || ttl <= 0 {
		return int(defaultMCPCacheTTL.Milliseconds())
	}
	return int(ttl.Milliseconds())
}

type mcpRendered struct {
	MimeType string
	Text     string
}

const mcpLegendKey = "services.ergo/legend"

func mcpLegendOf(row reflect.Type) map[string]any {
	units := map[string]string{}
	sentinels := map[string]string{}
	axes := map[string]string{}
	mcpLegendWalk(row, "", map[reflect.Type]bool{}, units, sentinels, axes)

	legend := map[string]any{}
	for name, entries := range map[string]map[string]string{
		"units": units, "sentinels": sentinels, "axes": axes,
	} {
		if len(entries) > 0 {
			legend[name] = entries
		}
	}
	if len(legend) == 0 {
		return nil
	}
	return legend
}

func mcpLegendWalk(row reflect.Type, prefix string, open map[reflect.Type]bool,
	units, sentinels, axes map[string]string) {

	if row.Kind() != reflect.Struct || open[row] {
		return
	}
	open[row] = true
	defer delete(open, row)

	for i := 0; i < row.NumField(); i++ {
		field := row.Field(i)
		if field.IsExported() == false {
			continue
		}
		name := prefix + field.Name
		if unit, tagged := field.Tag.Lookup("unit"); tagged {
			units[name] = unit
		}
		if sentinel, tagged := field.Tag.Lookup("sentinel"); tagged {
			sentinels[name] = sentinel
		}
		if axis, tagged := field.Tag.Lookup("axis"); tagged {
			axes[name] = axis
		}

		step := name + "."
		if mcpLegendCollection(field.Type) {
			step = name + "[]."
		}
		mcpLegendWalk(mcpLegendElem(field.Type), step, open, units, sentinels, axes)
	}
}

func mcpLegendElem(typ reflect.Type) reflect.Type {
	for {
		switch typ.Kind() {
		case reflect.Slice, reflect.Array, reflect.Ptr, reflect.Map:
			typ = typ.Elem()
		default:
			return typ
		}
	}
}

func mcpLegendCollection(typ reflect.Type) bool {
	for typ.Kind() == reflect.Ptr {
		typ = typ.Elem()
	}
	switch typ.Kind() {
	case reflect.Slice, reflect.Array, reflect.Map:
		return true
	}
	return false
}

func mcpAbsent(value any, uri mcpURI) (string, bool) {
	node := string(uri.Node)

	switch v := value.(type) {
	case inspect.ResponseInspectConnection:
		if v.Disconnected {
			return fmt.Sprintf("node %s has no connection to %s", node, uri.Target), true
		}
	case inspect.MessageInspectConnection:
		if v.Disconnected {
			return fmt.Sprintf("node %s has no connection to %s", node, uri.Target), true
		}

	case inspect.ResponseInspectEventStream:
		if v.Error != nil {
			return fmt.Sprintf("node %s cannot follow event %s: %s", node, uri.Target, v.Error), true
		}
	case inspect.MessageInspectEvent:
		switch {
		case v.Closed == false:
		case v.Reason == "":
			return fmt.Sprintf("node %s has no event %s", node, uri.Target), true
		default:
			return fmt.Sprintf("node %s cannot follow event %s: %s", node, uri.Target, v.Reason), true
		}

	case inspect.MessageInspectProcess:
		if v.Info.State == gen.ProcessStateTerminated {
			return fmt.Sprintf("node %s has no process %s", node, uri.Target), true
		}

	case inspect.MessageInspectMeta:
		if v.Info.State == gen.MetaStateTerminated {
			return fmt.Sprintf("node %s has no meta process %s", node, uri.Target), true
		}
	}
	return "", false
}

func mcpRender(value any) (mcpRendered, error) {
	encoded, err := json.Marshal(mcpViewOf(value))
	if err != nil {
		return mcpRendered{}, err
	}
	return mcpRendered{MimeType: mcpMimeJSON, Text: string(encoded)}, nil
}
