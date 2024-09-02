package observer

import (
	"encoding/json"

	"ergo.services/ergo/gen"
	"ergo.services/meta/websocket"
)

func (oh *observer_handler) sendError(id gen.Alias, cid string, err error) {

	msg := messageError{
		CID:   cid,
		Error: err.Error(),
	}
	buf, err := json.Marshal(msg)
	if err != nil {
		oh.Log().Error("unable to marshal error message for %s (cid: %s): %s", id, cid, err)
		return
	}
	wsmsg := websocket.Message{
		Body: buf,
	}
	if err := oh.SendAlias(id, wsmsg); err != nil {
		oh.Log().Error("unable to send error message to %s (cid: %s): %s", id, cid, err)
	}
}

func (oh *observer_handler) sendMessageResult(id gen.Alias, cid string, result any) {
	msg := messageResult{
		CID:    cid,
		Result: result,
	}
	buf, err := json.Marshal(msg)
	if err != nil {
		oh.Log().Error("unable to marshal message for %s: %s", id, err)
		return
	}
	wsmsg := websocket.Message{
		Body: buf,
	}
	if err := oh.SendAlias(id, wsmsg); err != nil {
		oh.Log().Error("unable to send message to %s : %s", id, err)
	}
}

func (oh *observer_handler) sendMessageEvent(id gen.Alias, timestamp int64, event gen.Event, message any) {
	msg := messageEvent{
		Timestamp: timestamp,
		Event:     event,
		Message:   message,
	}
	buf, err := json.Marshal(msg)
	if err != nil {
		oh.Log().Error("unable to marshal message for %s: %s", id, err)
		return
	}
	wsmsg := websocket.Message{
		Body: buf,
	}
	if err := oh.SendAlias(id, wsmsg); err != nil {
		oh.Log().Error("unable to send message to %s : %s", id, err)
	}
}
