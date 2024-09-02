package observer

import (
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"

	"ergo.services/ergo/act"
	"ergo.services/ergo/gen"
	"ergo.services/meta/websocket"
)

func factory_observer_handler() gen.ProcessBehavior {
	return &observer_handler{}
}

type observer_handler struct {
	act.Actor

	consumers     map[gen.Alias]*consumer
	subscriptions map[string]*subscription
}

func (oh *observer_handler) Init(args ...any) error {
	// TODO make it configurable
	oh.Log().SetLogger("default")

	oh.Log().Debug("%s started", oh.Name())
	oh.consumers = make(map[gen.Alias]*consumer)
	oh.subscriptions = make(map[string]*subscription)
	return nil
}

func (oh *observer_handler) HandleMessage(from gen.PID, message any) error {
	switch m := message.(type) {

	case websocket.MessageConnect:
		oh.Log().Debug("%s got connect message %v", oh.Name(), m.ID)
		consumer := &consumer{
			id:            m.ID,
			subscriptions: make(map[string]gen.Event),
		}
		oh.consumers[m.ID] = consumer
		networkInfo, _ := oh.Node().Network().Info()
		standalone := false
		if v, exist := oh.Env("standalone"); exist {
			standalone, _ = v.(bool)
		}

		type desc struct {
			Name  gen.Atom
			CRC32 string
		}

		intro := struct {
			Node    desc
			Peers   []desc
			Version gen.Version
		}{
			Peers:   []desc{},
			Version: Version,
		}

		for _, node := range networkInfo.Nodes {
			intro.Peers = append(intro.Peers, desc{node, node.CRC32()})
		}

		if standalone == false {
			intro.Node = desc{oh.Node().Name(), oh.Node().Name().CRC32()}
		}

		// send observer details
		buf, _ := json.Marshal(intro)
		wsmsg := websocket.Message{
			Body: buf,
		}
		oh.SendAlias(m.ID, wsmsg)

	case websocket.MessageDisconnect:
		oh.Log().Debug("%s got disconnect %v", oh.Name(), m.ID)
		consumer, exist := oh.consumers[m.ID]
		if exist == false {
			// bug? ignore this message
			oh.Log().Debug("%s disconnect unknown consumer %v", oh.Name(), m.ID)
			break
		}
		delete(oh.consumers, m.ID)

		for _, event := range consumer.subscriptions {
			oh.unsubscribe(event, consumer.id)
		}

	case websocket.Message:
		oh.Log().Debug("%s got message %s", oh.Name(), m.Body)
		cmd := messageCommand{}
		if err := json.Unmarshal(m.Body, &cmd); err != nil {
			oh.Log().Error("unable to parse websocket message: %s", err)
			break
		}
		cmd.ID = m.ID
		return oh.handleCommand(cmd)

	case gen.MessageDownEvent:
		subscription, exist := oh.subscriptions[m.Event.String()]
		if exist == false {
			// ignore
			break
		}
		for consumer_id := range subscription.consumers {
			oh.unsubscribe(m.Event, consumer_id)
			oh.sendMessageEvent(consumer_id, time.Now().UnixNano(), m.Event, "terminated")
		}
		delete(oh.subscriptions, m.Event.String())

	default:
		oh.Log().Error("uknown message from %s %#v", from, message)
	}
	return nil
}

func (oh *observer_handler) HandleEvent(message gen.MessageEvent) error {
	oh.Log().Debug("got event %s", message.Event)
	subscription, exist := oh.subscriptions[message.Event.String()]
	if exist == false {
		// handler has no this subscription. it's a BUG if continuing to receive these events
		oh.Log().Error("unknown event %s message. handler has no this subscriptions", message.Event)
		return nil
	}
	// keep the last event message
	subscription.last = &message
	for c := range subscription.consumers {
		oh.sendMessageEvent(c, message.Timestamp, message.Event, message.Message)
	}

	return nil
}

func (oh *observer_handler) subscribe(event gen.Event, id gen.Alias) error {
	// check if the handler already has this subscription and then add the new consumer id
	ev := event.String()
	s, exist := oh.subscriptions[ev]

	if exist == false {
		last_events, err := oh.MonitorEvent(event)
		if err != nil {
			oh.Log().Error("unable to monitor event %s: %s", event, err)
			return err
		}
		oh.Log().Debug("created monitor to %s", event)
		s = &subscription{
			event:     event,
			consumers: make(map[gen.Alias]bool),
		}

		if l := len(last_events); l > 0 {
			last := last_events[l-1]
			s.last = &last
		}
		oh.subscriptions[ev] = s
	}
	s.consumers[id] = true
	if s.last != nil {
		oh.sendMessageEvent(id, s.last.Timestamp, event, s.last.Message)
	}
	oh.Log().Debug("%s subscribed to %s", id, event)
	return nil
}

func (oh *observer_handler) unsubscribe(event gen.Event, id gen.Alias) {
	subscription, exist := oh.subscriptions[event.String()]
	if exist == false {
		oh.Log().Error("handler %s hasn't had subscription %s (requested for %s). Bug?", oh.Name(), event, id)
		return
	}

	delete(subscription.consumers, id)
	if len(subscription.consumers) == 0 {
		if err := oh.DemonitorEvent(subscription.event); err != nil {
			oh.Log().Error("unable to demonitor event %s:%s", subscription.event, err)
		}

		oh.Log().Debug("monitor to %s eliminated", event)
		delete(oh.subscriptions, event.String())
	}

}

func str2pid(node gen.Atom, creation int64, s string) (gen.PID, error) {
	pid := gen.PID{
		Node:     node,
		Creation: creation,
	}
	//return fmt.Sprintf("<%s.%d.%d>", p.Node.CRC32(), int32(p.ID>>32), int32(p.ID))
	parts := strings.Split(s, ".")
	if len(parts) != 3 {
		return pid, errors.New("incorrect string for gen.PID value")
	}

	id1, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		return pid, err
	}
	id2, err := strconv.ParseInt(strings.TrimSuffix(parts[2], ">"), 10, 64)
	if err != nil {
		return pid, err
	}
	pid.ID = (uint64(id1) << 32) | (uint64(id2))
	return pid, nil
}

func str2alias(node gen.Atom, creation int64, s string) (gen.Alias, error) {
	alias := gen.Alias{
		Node:     node,
		Creation: creation,
	}
	//return fmt.Sprintf("Ref#<%s.%d.%d.%d>", r.Node.CRC32(), r.ID[0], r.ID[1], r.ID[2])
	parts := strings.Split(s, ".")
	if len(parts) != 4 {
		return alias, errors.New("incorrect string for gen.Alias value")
	}
	id1, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		return alias, err
	}
	id2, err := strconv.ParseInt(parts[2], 10, 64)
	if err != nil {
		return alias, err
	}
	id3, err := strconv.ParseInt(strings.TrimSuffix(parts[3], ">"), 10, 64)
	if err != nil {
		return alias, err
	}
	alias.ID[0] = uint64(id1)
	alias.ID[1] = uint64(id2)
	alias.ID[2] = uint64(id3)
	return alias, nil
}
