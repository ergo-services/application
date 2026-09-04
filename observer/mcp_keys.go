package observer

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"ergo.services/ergo/gen"
)

type messageJobCancel struct{}

type jobEntry struct {
	Key string `json:"key"`
	URI string `json:"uri"`
}

func (w *postWorker) mcpJobList(writer http.ResponseWriter, request *http.Request, envelope mcpEnvelope) {
	identity, _ := identityOf(request)
	if identity.Subject == "" {
		w.mcpToolError(writer, request, envelope, errNeedsCaller.Error())
		return
	}

	owned := ownedBy(ownerPrefixJob, qualified(identity))
	entries := []jobEntry{}
	w.Node().ProcessRangeShortInfo(func(info gen.ProcessShortInfo) bool {
		if info.Application == appName && strings.HasPrefix(string(info.Name), owned) {
			key := strings.TrimPrefix(string(info.Name), owned)
			entries = append(entries, jobEntry{Key: key, URI: jobURI(key)})
		}
		return true
	})

	w.answerTool(writer, request, envelope, map[string]any{"jobs": entries})
}

func (w *postWorker) mcpJobCancel(writer http.ResponseWriter, request *http.Request,
	envelope mcpEnvelope, arguments map[string]any) {

	identity, _ := identityOf(request)
	if identity.Subject == "" {
		w.mcpToolError(writer, request, envelope, errNeedsCaller.Error())
		return
	}

	key, _ := arguments["key"].(string)
	if key == "" {
		w.mcpToolError(writer, request, envelope, "key is required")
		return
	}

	name := jobName(key, qualified(identity))
	force, _ := arguments["force"].(bool)
	held := false

	if force == false {
		held = w.Send(name, messageJobCancel{}) == nil
	} else {
		w.Node().ProcessRangeShortInfo(func(info gen.ProcessShortInfo) bool {
			if info.Application != appName || info.Name != name {
				return true
			}
			held = w.SendExit(info.PID, errors.New("cancelled by "+qualified(identity))) == nil
			return false
		})
	}

	w.answerTool(writer, request, envelope, map[string]any{
		"key": key, "held": held, "forced": force,
	})
}

func (w *postWorker) answerTool(writer http.ResponseWriter, request *http.Request,
	envelope mcpEnvelope, payload any) {

	value, err := json.Marshal(payload)
	if err != nil {
		w.mcpFail(writer, request, envelope.ID, mcpInternalError, "the answer is not renderable", nil)
		return
	}
	writeJSON(writer, request, http.StatusOK, mcpOK(envelope.ID, mcpToolResult{
		ResultType: mcpResultComplete,
		Content:    []any{mcpText(string(value))},
		Meta:       mcpAnswered(),
	}))
}
