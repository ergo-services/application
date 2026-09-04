package observer

import (
	"encoding/json"
	"reflect"
	"sort"
	"strings"
	"testing"

	"ergo.services/ergo/app/system/inspect"
	"ergo.services/ergo/gen"
	"ergo.services/ergo/testing/check"
	"ergo.services/ergo/testing/unit"
)

// every lens is announced, and nothing else is
func TestResourceTemplatesCoverEveryLens(t *testing.T) {
	announced := map[string]bool{}
	for _, spec := range lensSpecs {
		if announced[spec.Lens] {
			t.Errorf("lens %q is announced twice", spec.Lens)
		}
		announced[spec.Lens] = true

		if lensOf(spec.Lens) == "" {
			t.Errorf("lens %q is announced and is not a lens of a node", spec.Lens)
		}
		if spec.Title == "" || spec.Description == "" {
			t.Errorf("lens %q is announced without a title or a description", spec.Lens)
		}
	}

	for lens := range lensSubscription {
		if announced[lens] == false {
			t.Errorf("lens %q is readable and is not announced anywhere", lens)
		}
	}
}

// a node name holds an @ and dots, so its expansion has to be the reserved one
func TestResourceTemplatesExpandToReadableURIs(t *testing.T) {
	const node = "shop-catalog-indexer-7d9f8b5c4-x2m4q@localhost"
	targets := map[string]string{
		"{pid}":    "1041.1787758127",
		"{alias}":  "107118.6819740677833.0.1787758127",
		"{+peer}":  "shop-order-matcher-6b8d4f2a1-kk29s@localhost",
		"{name}":   "shop.orders%2Fplaced",
		"{key}":    "run1",
		"{lens}":   "processes",
		"{+node}":  node,
		"{?query}": "",
	}

	for _, entry := range mcpResourceTemplates(Ceiling{}) {
		expanded := entry.URITemplate
		for expression, value := range targets {
			expanded = strings.ReplaceAll(expanded, expression, value)
		}
		if at := strings.Index(expanded, "{?"); at >= 0 {
			expanded = expanded[:at]
		}
		if strings.ContainsAny(expanded, "{}") {
			t.Errorf("%s: %q still holds an unexpanded expression", entry.Name, expanded)
			continue
		}

		uri, err := parseURI(expanded)
		if err != nil {
			t.Errorf("%s: %q does not parse: %s", entry.Name, expanded, err)
			continue
		}
		switch entry.Name {
		case "watch":
			if uri.Key == "" {
				t.Errorf("%s: %q carries no key", entry.Name, expanded)
			}
		case uriWordJob:
			if uri.Lens != uriWordJob || uri.Key == "" {
				t.Errorf("%s: %q is not a run", entry.Name, expanded)
			}
		default:
			if uri.Lens != entry.Name {
				t.Errorf("%s: %q reads as lens %q", entry.Name, expanded, uri.Lens)
			}
			if string(uri.Node) != node {
				t.Errorf("%s: %q reads node %q", entry.Name, expanded, uri.Node)
			}
		}
	}
}

func TestResourceTemplatesRespectTheCeiling(t *testing.T) {
	all := mcpResourceTemplates(Ceiling{})
	if len(all) < len(lensSpecs) {
		t.Fatalf("an open ceiling was shown %d of %d lenses", len(all), len(lensSpecs))
	}

	narrow := mcpResourceTemplates(Ceiling{Allow: []string{inspect.CapNode}})
	named := map[string]bool{}
	for _, entry := range narrow {
		named[entry.Name] = true
	}
	if named["node"] == false {
		t.Error("the one permitted lens is not announced")
	}
	for _, hidden := range []string{"log", "tracing", "processes"} {
		if named[hidden] {
			t.Errorf("lens %q is announced to a ceiling that refuses it", hidden)
		}
	}

	if named[uriWordJob] == false || named["watch"] == false {
		t.Error("the runs and the keyed level disappeared with the ceiling of a node")
	}
}

// the announced parameter names are the ones converted
func TestAnnouncedParamsSurviveTheConversion(t *testing.T) {
	for _, spec := range lensSpecs {
		for _, param := range spec.Params {
			if param == "since" {
				continue
			}
			uri, err := parseURI(mcpScheme + "n@h/" + spec.Lens + "?" + param + "=1")
			check.NoError(t, err)

			args, err := lensArgs(uri)
			check.NoError(t, err)

			value := args[param]
			switch {
			case lensArgNumber[param]:
				if _, ok := value.(float64); ok == false {
					t.Errorf("lens %q reads %s as a number and it arrives as %T",
						spec.Lens, param, value)
				}
			case lensArgBool[param]:
				if _, ok := value.(bool); ok == false {
					t.Errorf("lens %q reads %s as true or false and it arrives as %T",
						spec.Lens, param, value)
				}
			case lensArgList[param]:
				if _, ok := value.([]any); ok == false {
					t.Errorf("lens %q reads %s as a list and it arrives as %T",
						spec.Lens, param, value)
				}
			default:
				if _, ok := value.(string); ok == false {
					t.Errorf("lens %q reads %s as text and it arrives as %T",
						spec.Lens, param, value)
				}
			}
		}
	}
}

// an announced parameter is read in the shape it arrives in
func TestAnnouncedParamsAreReadInTheShapeTheyArriveIn(t *testing.T) {
	shapes := map[string][]any{
		"number": {float64(1)},
		"list":   {[]any{"error"}},
		"bool":   {true},
		"text":   {"error", "yes", "no", "true", "1"},
	}

	for _, spec := range lensSpecs {
		subscription := lensOf(spec.Lens)
		for _, param := range spec.Params {
			if param == "since" {
				continue
			}

			absent, _, err := buildInspectRequest(subscription, map[string]any{})
			if err != nil {
				continue
			}

			read := map[string]bool{}
			for shape, values := range shapes {
				for _, value := range values {
					built, _, err := buildInspectRequest(subscription,
						map[string]any{param: value})
					if err == nil && reflect.DeepEqual(built, absent) == false {
						read[shape] = true
					}
				}
			}

			produced := "text"
			switch {
			case lensArgNumber[param]:
				produced = "number"
			case lensArgBool[param]:
				produced = "bool"
			case lensArgList[param]:
				produced = "list"
			}

			if len(read) == 0 {
				t.Errorf("lens %q announces %s and the builder reads no shape of it",
					spec.Lens, param)
				continue
			}
			if read[produced] == false {
				shown := make([]string, 0, len(read))
				for shape := range read {
					shown = append(shown, shape)
				}
				sort.Strings(shown)
				t.Errorf("lens %q reads %s as %s and a uri delivers it as %s",
					spec.Lens, param, strings.Join(shown, " or "), produced)
			}
		}
	}
}

// the method names nothing, so it carries no mirroring name header
func TestWorkerAnswersResourceTemplates(t *testing.T) {
	n := unit.StartNode(t, testNode, gen.NodeOptions{})
	n.Network().FailRegistrar(gen.ErrUnsupported)

	sub, err := n.Spawn(factory_post_worker, gen.ProcessOptions{})
	check.NoError(t, err)

	message, answer := webRequest(t, false, "resources/templates/list", nil)
	sub.SendMessage(gen.PID{}, message)

	if answer.Body.Len() == 0 {
		t.Fatal("the templates went unanswered")
	}

	var out struct {
		Result mcpTemplateList `json:"result"`
		Error  *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(answer.Body.Bytes(), &out); err != nil {
		t.Fatalf("body %q: %s", answer.Body.String(), err)
	}
	if out.Error != nil {
		t.Fatalf("refused with %d: %s", out.Error.Code, out.Error.Message)
	}
	if out.Result.ResultType != mcpResultComplete {
		t.Errorf("result type %q", out.Result.ResultType)
	}
	if len(out.Result.ResourceTemplates) < len(lensSpecs) {
		t.Errorf("%d templates for %d lenses", len(out.Result.ResourceTemplates), len(lensSpecs))
	}
	if out.Result.TTLMs <= 0 {
		t.Error("the list says nothing about how long it stays fresh")
	}
	if out.Result.CacheScope != mcpCachePrivate {
		t.Errorf("cache scope %q", out.Result.CacheScope)
	}

	for _, entry := range out.Result.ResourceTemplates {
		if entry.Name != "processes" {
			continue
		}
		if strings.Contains(entry.URITemplate, "{+node}") == false {
			t.Errorf("the process listing is announced as %q", entry.URITemplate)
		}
		if entry.MimeType != mcpMimeJSON {
			t.Errorf("the process listing is announced as %q", entry.MimeType)
		}
		return
	}
	t.Error("the process listing is not among the templates")
}

func TestToolSaysWhatTheCeilingRefusesOfIt(t *testing.T) {
	whole := map[string]string{}
	for _, tool := range toolEntries(Ceiling{}) {
		whole[tool.Name] = tool.Description
		if strings.Contains(tool.Description, "Not permitted here") {
			t.Errorf("%s tells an open ceiling what it refuses", tool.Name)
		}
	}

	narrow := Ceiling{ReadOnly: true, Deny: []string{inspect.CapProcessRange}}
	for _, tool := range toolEntries(narrow) {
		if tool.Name != "processes" {
			continue
		}
		if strings.Contains(tool.Description, inspect.CapProcessRange) == false {
			t.Errorf("processes is offered without saying what of it is refused: %s",
				tool.Description)
		}
		if strings.HasPrefix(tool.Description, whole["processes"]) == false {
			t.Error("the refusal replaced the description instead of following it")
		}
		return
	}
	t.Error("the tool whose other mode is still permitted was not offered at all")
}
