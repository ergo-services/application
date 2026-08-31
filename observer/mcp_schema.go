package observer

import (
	"fmt"
	"math"
	"slices"
	"strings"
)

type mcpTool struct {
	Name        string              `json:"name"`
	Title       string              `json:"title,omitempty"`
	Description string              `json:"description"`
	Schema      mcpSchema           `json:"inputSchema"`
	Annotations *mcpToolAnnotations `json:"annotations,omitempty"`

	Action string `json:"-"` // empty when the tool is served here rather than on a node

	Lens string `json:"-"` // the resource this answer is a reading of, if any

	// the argument that becomes the last segment of that uri
	LensTarget string `json:"-"`

	// what it may ask the node for; a lens takes them from its spec
	Capabilities []string `json:"-"`

	Fanout   bool `json:"-"` // answers with a resource to read instead of a result
	Mutating bool `json:"-"` // gated by a read-only ceiling

	Destructive bool `json:"-"`
	Writes      bool `json:"-"` // state here, not on the observed node

	NotIdempotent bool `json:"-"` // calling it twice does it twice

	Build func(args map[string]any) (string, map[string]any, error) `json:"-"`
}

// every hint is stated: destructiveHint and openWorldHint default to true, so an omitted
// field would claim the opposite
type mcpToolAnnotations struct {
	ReadOnlyHint    bool `json:"readOnlyHint"`
	DestructiveHint bool `json:"destructiveHint"`
	IdempotentHint  bool `json:"idempotentHint"`
	OpenWorldHint   bool `json:"openWorldHint"`
}

func mcpAnnotationsOf(tool mcpTool) *mcpToolAnnotations {
	return &mcpToolAnnotations{
		ReadOnlyHint:    tool.Mutating == false && tool.Fanout == false && tool.Writes == false,
		DestructiveHint: tool.Destructive,
		IdempotentHint:  tool.NotIdempotent == false,
		OpenWorldHint:   false,
	}
}

func toolAction(tool mcpTool, args map[string]any) (string, map[string]any, error) {
	if tool.Build != nil {
		return tool.Build(args)
	}
	return tool.Action, args, nil
}

func (t mcpTool) servesAction() bool {
	return t.Action != "" || t.Build != nil
}

type mcpSchema struct {
	Type       string                   `json:"type"`
	Properties map[string]mcpSchemaItem `json:"properties,omitempty"`
	Required   []string                 `json:"required,omitempty"`
}

type mcpSchemaItem struct {
	Type        string   `json:"type"`
	Description string   `json:"description,omitempty"`
	Items       *mcpItem `json:"items,omitempty"`

	// every name here has to be one the framework parses: this is a copy, and a copy drifts
	Enum []string `json:"enum,omitempty"`

	Properties map[string]mcpSchemaItem `json:"properties,omitempty"`
}

type mcpItem struct {
	Type       string                   `json:"type"`
	Properties map[string]mcpSchemaItem `json:"properties,omitempty"`
}

func pidProperty(what string) mcpSchemaItem {
	return mcpSchemaItem{
		Type: "string",
		Description: what + " As any answer writes it: node@host:ID.Creation. The node may be " +
			"left out when it is the node argument: ID.Creation",
	}
}

func aliasProperty(what string) mcpSchemaItem {
	return mcpSchemaItem{
		Type: "string",
		Description: what + " As any answer writes it: node@host:ID.ID.ID.Creation. The node " +
			"may be left out when it is the node argument: ID.ID.ID.Creation",
	}
}

// the closed sets the framework parses; TestSchemaEnumsAreParsed walks these against the
// parsers
var (
	logLevelNames = []string{"debug", "info", "warning", "error", "panic", "disabled"}
	priorityNames = []string{"normal", "high", "max"}

	processStateNames = []string{
		"init", "sleep", "running", "wait response", "terminated", "zombee",
	}
	compressionTypes  = []string{"gzip", "zlib", "lzw"}
	compressionLevels = []string{"best size", "best speed", "default"}
	applicationModes  = []string{"temporary", "transient", "permanent"}
	samplerTypes      = []string{"always", "disable", "ratio", "rate_limit"}

	// what a message may be: the values EDF carries without a type registered for them
	sendTypes = []string{
		"bool", "string", "atom", "binary",
		"int", "int8", "int16", "int32", "int64",
		"uint", "uint8", "uint16", "uint32", "uint64",
		"float32", "float64",
		"time", "pid", "process_id", "alias", "ref", "event",
	}
	knobNames = []string{
		"send_priority", "compression", "compression_type", "compression_level",
		"compression_threshold", "keep_network_order", "important_delivery",
	}

	// TestStepToolsMatchTheTable keeps this equal to what allowedStep admits
	stepTools = []string{
		"processes", "process_state", "meta_state", "process_lookup", "app_tree", "subtree",
		"types", "goroutines", "heap_profile", "capabilities",
		"node", "network", "connections", "connection", "applications", "events", "event",
		"cron", "cron_schedule", "registrar_nodes", "registrar_routes",
		"registrar_proxy_routes", "registrar_application_routes",
	}
)

const nodeArgument = "node"

func nodeProperty() mcpSchemaItem {
	return mcpSchemaItem{
		Type:        "string",
		Description: "Node name as it appears in ergo://cluster.",
	}
}

// a value of the wrong type would be asserted into false or zero and the answer would still
// say done
func checkArgs(schema mcpSchema, args map[string]any) error {
	for _, name := range schema.Required {
		if hasArg(args, name) == false {
			return fmt.Errorf("%s is required", name)
		}
	}
	for name, value := range args {
		item, declared := schema.Properties[name]
		if declared == false || value == nil {
			continue
		}
		if err := checkArgValue(name, item, value); err != nil {
			return err
		}
	}
	return nil
}

func argKind(value any) string {
	switch value.(type) {
	case string:
		return "text"
	case bool:
		return "true or false"
	case float64:
		return "a number"
	case []any:
		return "a list"
	case map[string]any:
		return "an object"
	case nil:
		return "nothing"
	}
	return fmt.Sprintf("%T", value)
}

func checkArgValue(name string, item mcpSchemaItem, value any) error {
	switch item.Type {
	case "string":
		text, ok := value.(string)
		if ok == false {
			return fmt.Errorf("%s must be text, got %s", name, argKind(value))
		}
		if len(item.Enum) > 0 && slices.Contains(item.Enum, text) == false {
			return fmt.Errorf("%s is one of %s, not %q", name, strings.Join(item.Enum, ", "), text)
		}
	case "boolean":
		if _, ok := value.(bool); ok == false {
			return fmt.Errorf("%s must be true or false, got %s", name, argKind(value))
		}
	case "integer":
		number, ok := value.(float64)
		if ok == false {
			return fmt.Errorf("%s must be a whole number, got %s", name, argKind(value))
		}
		if number != math.Trunc(number) {
			return fmt.Errorf("%s must be whole, not %v", name, number)
		}
	case "number":
		if _, ok := value.(float64); ok == false {
			return fmt.Errorf("%s must be a number, got %s", name, argKind(value))
		}
	case "array":
		list, ok := value.([]any)
		if ok == false {
			return fmt.Errorf("%s must be a list, got %s", name, argKind(value))
		}
		if item.Items == nil {
			return nil
		}
		for i, entry := range list {
			at := mcpSchemaItem{Type: item.Items.Type, Properties: item.Items.Properties}
			if err := checkArgValue(fmt.Sprintf("%s[%d]", name, i), at, entry); err != nil {
				return err
			}
		}
	case "object":
		fields, ok := value.(map[string]any)
		if ok == false {
			return fmt.Errorf("%s must be an object, got %s", name, argKind(value))
		}
		for field, declared := range item.Properties {
			given, present := fields[field]
			if present == false || given == nil {
				continue
			}
			if err := checkArgValue(name+"."+field, declared, given); err != nil {
				return err
			}
		}
	}
	return nil
}

var toolTables = [][]mcpTool{mcpTools, mutationTools}

var toolIndex = buildToolIndex()

func buildToolIndex() map[string]mcpTool {
	index := map[string]mcpTool{}
	for _, table := range toolTables {
		for _, tool := range table {
			index[tool.Name] = tool
		}
	}
	return index
}

func toolByName(name string) (mcpTool, bool) {
	tool, served := toolIndex[name]
	return tool, served
}

func toolCapabilities(tool mcpTool) []string {
	if spec, known := lensSpecOf(tool.Lens); known {
		return spec.Capabilities
	}
	return tool.Capabilities
}

func toolPartly(ceiling Ceiling, capabilities []string) string {
	if len(capabilities) < 2 {
		return ""
	}

	refused := make([]string, 0, len(capabilities))
	for _, name := range capabilities {
		if ceiling.Allows(name) == false {
			refused = append(refused, name)
		}
	}
	if len(refused) == 0 {
		return ""
	}
	return " Not permitted here, so the arguments that need it are refused: " +
		strings.Join(refused, ", ") + "."
}

func toolEntries(ceiling Ceiling) []mcpTool {
	out := make([]mcpTool, 0, len(toolIndex))
	for _, table := range toolTables {
		for _, tool := range table {
			if tool.Mutating && ceiling.ReadOnly {
				continue
			}
			if ceilingAllowsAny(ceiling, toolCapabilities(tool)) == false {
				continue
			}
			tool.Annotations = mcpAnnotationsOf(tool)
			tool.Description += toolPartly(ceiling, toolCapabilities(tool))
			out = append(out, tool)
		}
	}
	return out
}
