package observer

import (
	"errors"

	"ergo.services/ergo/app/system/inspect"
	"ergo.services/ergo/gen"
)

func (oh *observer_handler) handleCommand(cmd messageCommand) error {
	switch cmd.Command {
	case "connect":
		// check consumer
		consumer, exist := oh.consumers[cmd.ID]
		if exist == false {
			oh.Log().Error("got connect request from unknown consumer %s", cmd.ID)
			return nil
		}

		node := oh.Node().Name()
		if cmd.Name != "" {
			node = gen.Atom(cmd.Name)
		}
		if node == consumer.node {
			// already connected
			oh.sendError(cmd.ID, cmd.CID, errors.New("already connected"))
			return nil
		}

		if err := oh.tryConnect(node, cmd.Args); err != nil {
			oh.sendError(cmd.ID, cmd.CID, err)
			return nil
		}

		inspectProcess := gen.ProcessID{
			Name: inspect.Name,
			Node: node,
		}
		// inspect node
		v, err := oh.Call(inspectProcess, inspect.RequestInspectNode{})
		if err != nil {
			oh.sendError(cmd.ID, cmd.CID, err)
			return nil
		}

		inspectNode, ok := v.(inspect.ResponseInspectNode)
		if ok == false {
			oh.Log().Error("incorrect result (expected inspect.ResponseInspectNode): %#v", v)
			return nil
		}

		// before we go further we should check if this consumer
		// already has a 'connection' (has subscriptions)
		if consumer.node != "" {
			// cancel all subscriptions first
			for _, event := range consumer.subscriptions {
				oh.unsubscribe(event, consumer.id)
			}
		}

		oh.sendMessageResult(cmd.ID, cmd.CID, inspectNode)

		if err := oh.subscribe(inspectNode.Event, cmd.ID); err != nil {
			oh.sendError(cmd.ID, cmd.CID, err)
			return nil
		}

		consumer.node = node
		consumer.creation = inspectNode.Creation
		consumer.subscriptions[inspectNode.Event.String()] = inspectNode.Event

	case "peers":
		networkInfo, _ := oh.Node().Network().Info()
		type desc struct {
			Name  gen.Atom
			CRC32 string
		}

		peers := []desc{}

		for _, node := range networkInfo.Nodes {
			peers = append(peers, desc{node, node.CRC32()})
		}
		oh.sendMessageResult(cmd.ID, cmd.CID, peers)

	case "subscribe":
		// check consumer
		consumer, exist := oh.consumers[cmd.ID]
		if exist == false {
			oh.Log().Error("got subscribe request from unknown consumer %s", cmd.ID)
			return nil
		}

		inspectProcess := gen.ProcessID{
			Name: inspect.Name,
			Node: consumer.node,
		}

		switch cmd.Name {
		case "log":
			// inspect node logs
			levels := []gen.LogLevel{}
			slevels, ok := cmd.Args["Levels"].([]any)
			if ok == false {
				oh.Log().Error("incorrect Args.Levels value in log subscribe command")
			}
			for _, l := range slevels {
				s, _ := l.(string)
				switch s {
				case "debug":
					levels = append(levels, gen.LogLevelDebug)
				case "info":
					levels = append(levels, gen.LogLevelInfo)
				case "warning":
					levels = append(levels, gen.LogLevelWarning)
				case "error":
					levels = append(levels, gen.LogLevelError)
				case "panic":
					levels = append(levels, gen.LogLevelPanic)
				}
			}
			v, err := oh.Call(inspectProcess, inspect.RequestInspectLog{Levels: levels})
			if err != nil {
				oh.sendError(cmd.ID, cmd.CID, err)
				return nil
			}
			inspectLog, ok := v.(inspect.ResponseInspectLog)
			if ok == false {
				oh.Log().Error("incorrect result (expected inspect.ResponseInspectLog): %#v", v)
				return nil
			}
			oh.sendMessageResult(cmd.ID, cmd.CID, inspectLog)
			if err := oh.subscribe(inspectLog.Event, cmd.ID); err != nil {
				oh.sendError(cmd.ID, cmd.CID, err)
				return nil
			}

			consumer.subscriptions[inspectLog.Event.String()] = inspectLog.Event

		case "network":
			v, err := oh.Call(inspectProcess, inspect.RequestInspectNetwork{})
			if err != nil {
				oh.sendError(cmd.ID, cmd.CID, err)
				return nil
			}
			response, ok := v.(inspect.ResponseInspectNetwork)
			if ok == false {
				oh.Log().Error("incorrect result (expected inspect.ResponseInspectNetwork): %#v", v)
				return nil
			}
			oh.sendMessageResult(cmd.ID, cmd.CID, response)
			if err := oh.subscribe(response.Event, cmd.ID); err != nil {
				oh.sendError(cmd.ID, cmd.CID, err)
				return nil
			}

			consumer.subscriptions[response.Event.String()] = response.Event

		case "remote_node":
			var remote gen.Atom
			vremote := cmd.Args["Name"]

			if vremote == nil {
				oh.sendError(cmd.ID, cmd.CID, errors.New("Args: {Name: is empty}"))
				return nil
			}
			remote = gen.Atom(vremote.(string))
			request := inspect.RequestInspectConnection{
				RemoteNode: remote,
			}
			v, err := oh.Call(inspectProcess, request)
			if err != nil {
				oh.sendError(cmd.ID, cmd.CID, err)
				return nil
			}
			response, ok := v.(inspect.ResponseInspectConnection)
			if ok == false {
				oh.Log().Error("incorrect result (expected inspect.ResponseInspectConnection): %#v", v)
				return nil
			}

			oh.sendMessageResult(cmd.ID, cmd.CID, response)
			if err := oh.subscribe(response.Event, cmd.ID); err != nil {
				oh.sendError(cmd.ID, cmd.CID, err)
				return nil
			}

			consumer.subscriptions[response.Event.String()] = response.Event

		case "process_list":
			request := inspect.RequestInspectProcessList{}
			if len(cmd.Args) > 0 {
				request.Start = int(cmd.Args["start"].(float64))
				request.Limit = int(cmd.Args["limit"].(float64))
			}
			v, err := oh.Call(inspectProcess, request)
			if err != nil {
				oh.sendError(cmd.ID, cmd.CID, err)
				return nil
			}
			response, ok := v.(inspect.ResponseInspectProcessList)
			if ok == false {
				oh.Log().Error("incorrect result (expected inspect.ResponseInspectProcessList): %#v", v)
				return nil
			}
			oh.sendMessageResult(cmd.ID, cmd.CID, response)
			if err := oh.subscribe(response.Event, cmd.ID); err != nil {
				oh.sendError(cmd.ID, cmd.CID, err)
				return nil
			}

			consumer.subscriptions[response.Event.String()] = response.Event

		case "process":
			var pid gen.PID
			vpid := cmd.Args["PID"]
			if vpid == nil {
				oh.sendError(cmd.ID, cmd.CID, errors.New("Args: {PID: is empty}"))
				return nil
			}
			str, ok := vpid.(string)
			if ok == false {
				oh.sendError(cmd.ID, cmd.CID, errors.New("Args: {PID: incorrect value}"))
				return nil
			}
			pid, err := str2pid(consumer.node, consumer.creation, str)
			if err != nil {
				oh.sendError(cmd.ID, cmd.CID, err)
				return nil
			}

			request := inspect.RequestInspectProcess{
				PID: pid,
			}

			v, err := oh.Call(inspectProcess, request)
			if err != nil {
				oh.sendError(cmd.ID, cmd.CID, err)
				return nil
			}
			response, ok := v.(inspect.ResponseInspectProcess)
			if ok == false {
				oh.Log().Error("incorrect result (expected inspect.ResponseInspectProcess): %#v", v)
				return nil
			}
			oh.sendMessageResult(cmd.ID, cmd.CID, response)
			if err := oh.subscribe(response.Event, cmd.ID); err != nil {
				oh.sendError(cmd.ID, cmd.CID, err)
				return nil
			}

			consumer.subscriptions[response.Event.String()] = response.Event

		case "process_state":
			var pid gen.PID
			vpid := cmd.Args["PID"]
			if vpid == nil {
				oh.sendError(cmd.ID, cmd.CID, errors.New("Args: {PID: is empty}"))
				return nil
			}
			str, ok := vpid.(string)
			if ok == false {
				oh.sendError(cmd.ID, cmd.CID, errors.New("Args: {PID: incorrect value}"))
				return nil
			}
			pid, err := str2pid(consumer.node, consumer.creation, str)
			if err != nil {
				oh.sendError(cmd.ID, cmd.CID, err)
				return nil
			}

			request := inspect.RequestInspectProcessState{
				PID: pid,
			}

			v, err := oh.Call(inspectProcess, request)
			if err != nil {
				oh.sendError(cmd.ID, cmd.CID, err)
				return nil
			}
			response, ok := v.(inspect.ResponseInspectProcessState)
			if ok == false {
				oh.Log().Error("incorrect result (expected inspect.ResponseInspectProcessState): %#v", v)
				return nil
			}
			oh.sendMessageResult(cmd.ID, cmd.CID, response)
			if err := oh.subscribe(response.Event, cmd.ID); err != nil {
				oh.sendError(cmd.ID, cmd.CID, err)
				return nil
			}

			consumer.subscriptions[response.Event.String()] = response.Event

		case "meta":
			vmeta := cmd.Args["ID"]
			if vmeta == nil {
				oh.sendError(cmd.ID, cmd.CID, errors.New("Args: {ID: is empty}"))
				return nil
			}
			str, ok := vmeta.(string)
			if ok == false {
				oh.sendError(cmd.ID, cmd.CID, errors.New("Args: {ID: incorrect value}"))
				return nil
			}
			meta, err := str2alias(consumer.node, consumer.creation, str)
			if err != nil {
				oh.sendError(cmd.ID, cmd.CID, err)
				return nil
			}

			request := inspect.RequestInspectMeta{
				Meta: meta,
			}

			v, err := oh.Call(inspectProcess, request)
			if err != nil {
				oh.sendError(cmd.ID, cmd.CID, err)
				return nil
			}
			response, ok := v.(inspect.ResponseInspectMeta)
			if ok == false {
				oh.Log().Error("incorrect result (expected inspect.ResponseInspectMeta): %#v", v)
				return nil
			}
			oh.sendMessageResult(cmd.ID, cmd.CID, response)
			if err := oh.subscribe(response.Event, cmd.ID); err != nil {
				oh.sendError(cmd.ID, cmd.CID, err)
				return nil
			}

			consumer.subscriptions[response.Event.String()] = response.Event

		case "meta_state":
			vmeta := cmd.Args["ID"]
			if vmeta == nil {
				oh.sendError(cmd.ID, cmd.CID, errors.New("Args: {ID: is empty}"))
				return nil
			}
			str, ok := vmeta.(string)
			if ok == false {
				oh.sendError(cmd.ID, cmd.CID, errors.New("Args: {ID: incorrect value}"))
				return nil
			}
			meta, err := str2alias(consumer.node, consumer.creation, str)
			if err != nil {
				oh.sendError(cmd.ID, cmd.CID, err)
				return nil
			}

			request := inspect.RequestInspectMetaState{
				Meta: meta,
			}

			v, err := oh.Call(inspectProcess, request)
			if err != nil {
				oh.sendError(cmd.ID, cmd.CID, err)
				return nil
			}
			response, ok := v.(inspect.ResponseInspectMetaState)
			if ok == false {
				oh.Log().Error("incorrect result (expected inspect.ResponseInspectMetaState): %#v", v)
				return nil
			}
			oh.sendMessageResult(cmd.ID, cmd.CID, response)
			if err := oh.subscribe(response.Event, cmd.ID); err != nil {
				oh.sendError(cmd.ID, cmd.CID, err)
				return nil
			}

			consumer.subscriptions[response.Event.String()] = response.Event

		case "application_list":
			v, err := oh.Call(inspectProcess, inspect.RequestInspectApplicationList{})
			if err != nil {
				oh.sendError(cmd.ID, cmd.CID, err)
				return nil
			}
			response, ok := v.(inspect.ResponseInspectApplicationList)
			if ok == false {
				oh.Log().Error("incorrect result (expected inspect.ResponseInspectApplicationList): %#v", v)
				return nil
			}
			oh.sendMessageResult(cmd.ID, cmd.CID, response)
			if err := oh.subscribe(response.Event, cmd.ID); err != nil {
				oh.sendError(cmd.ID, cmd.CID, err)
				return nil
			}

			consumer.subscriptions[response.Event.String()] = response.Event

		case "application_tree":
			var application gen.Atom
			var limit int = 1000 // default limit

			vapp := cmd.Args["Application"]
			if vapp == nil {
				oh.sendError(cmd.ID, cmd.CID, errors.New("Args: {Application: is empty}"))
				return nil
			}
			appName, ok := vapp.(string)
			if ok == false {
				oh.sendError(cmd.ID, cmd.CID, errors.New("Args: {Application: incorrect value}"))
				return nil
			}
			application = gen.Atom(appName)

			if vlimit := cmd.Args["Limit"]; vlimit != nil {
				if flimit, ok := vlimit.(float64); ok {
					limit = int(flimit)
				}
			}

			request := inspect.RequestInspectApplicationTree{
				Application: application,
				Limit:       limit,
			}

			v, err := oh.Call(inspectProcess, request)
			if err != nil {
				oh.sendError(cmd.ID, cmd.CID, err)
				return nil
			}
			response, ok := v.(inspect.ResponseInspectApplicationTree)
			if ok == false {
				oh.Log().Error("incorrect result (expected inspect.ResponseInspectApplicationTree): %#v", v)
				return nil
			}
			oh.sendMessageResult(cmd.ID, cmd.CID, response)
			if err := oh.subscribe(response.Event, cmd.ID); err != nil {
				oh.sendError(cmd.ID, cmd.CID, err)
				return nil
			}

			consumer.subscriptions[response.Event.String()] = response.Event

		default:
			oh.Log().Error("unknown subscription name %q (from %s)", cmd.Name, cmd.ID)
			return nil
		}

	case "unsubscribe":
		// check consumer
		consumer, exist := oh.consumers[cmd.ID]
		if exist == false {
			oh.Log().Error("got unsubscribe request from unknown consumer %s", cmd.ID)
			return nil
		}
		event, exist := consumer.subscriptions[cmd.Name]
		if exist == false {
			oh.Log().Error("unsubscribe request from unknown subscription %s (consumer %s)", cmd.Name, cmd.ID)
			return nil
		}

		delete(consumer.subscriptions, cmd.Name)
		oh.unsubscribe(event, consumer.id)

	case "do":
		// check consumer
		consumer, exist := oh.consumers[cmd.ID]
		if exist == false {
			oh.Log().Error("got inspect request from unknown consumer %s", cmd.ID)
			return nil
		}
		inspectProcess := gen.ProcessID{
			Name: inspect.Name,
			Node: consumer.node,
		}
		switch cmd.Name {
		case "send":
			var pid gen.PID
			vpid := cmd.Args["PID"]
			if vpid == nil {
				oh.sendError(cmd.ID, cmd.CID, errors.New("Args: {PID: is empty}"))
				return nil
			}
			str, ok := vpid.(string)
			if ok == false {
				oh.sendError(cmd.ID, cmd.CID, errors.New("Args: {PID: incorrect value}"))
				return nil
			}
			pid, err := str2pid(consumer.node, consumer.creation, str)
			if err != nil {
				oh.sendError(cmd.ID, cmd.CID, err)
				return nil
			}
			vmessage := cmd.Args["Message"]
			if vmessage == nil {
				oh.sendError(cmd.ID, cmd.CID, errors.New("Args: {Message: is empty}"))
				return nil
			}

			vpriority := cmd.Args["Priority"]
			priority := gen.MessagePriorityNormal
			switch vpriority.(string) {
			case "high":
				priority = gen.MessagePriorityHigh
			case "max":
				priority = gen.MessagePriorityMax
			}
			request := inspect.RequestDoSend{
				PID:      pid,
				Message:  vmessage,
				Priority: priority,
			}

			v, err := oh.Call(inspectProcess, request)
			if err != nil {
				oh.sendError(cmd.ID, cmd.CID, err)
				return nil
			}
			response, ok := v.(inspect.ResponseDoSend)
			if ok == false {
				oh.Log().Error("incorrect result (expected inspect.ResponseDoSend): %#v", v)
				return nil
			}
			oh.sendMessageResult(cmd.ID, cmd.CID, response)

		case "send_meta":
			vmeta := cmd.Args["ID"]
			if vmeta == nil {
				oh.sendError(cmd.ID, cmd.CID, errors.New("Args: {ID: is empty}"))
				return nil
			}
			str, ok := vmeta.(string)
			if ok == false {
				oh.sendError(cmd.ID, cmd.CID, errors.New("Args: {ID: incorrect value}"))
				return nil
			}
			meta, err := str2alias(consumer.node, consumer.creation, str)
			if err != nil {
				oh.sendError(cmd.ID, cmd.CID, err)
				return nil
			}
			vmessage := cmd.Args["Message"]
			if vmessage == nil {
				oh.sendError(cmd.ID, cmd.CID, errors.New("Args: {Message: is empty}"))
				return nil
			}
			request := inspect.RequestDoSendMeta{
				Meta:    meta,
				Message: vmessage,
			}

			v, err := oh.Call(inspectProcess, request)
			if err != nil {
				oh.sendError(cmd.ID, cmd.CID, err)
				return nil
			}
			response, ok := v.(inspect.ResponseDoSendMeta)
			if ok == false {
				oh.Log().Error("incorrect result (expected inspect.ResponseDoSendMeta): %#v", v)
				return nil
			}
			oh.sendMessageResult(cmd.ID, cmd.CID, response)

		case "send_exit":
			vpid := cmd.Args["PID"]
			if vpid == nil {
				oh.sendError(cmd.ID, cmd.CID, errors.New("Args: {PID: is empty}"))
				return nil
			}
			str, ok := vpid.(string)
			if ok == false {
				oh.sendError(cmd.ID, cmd.CID, errors.New("Args: {PID: incorrect value}"))
				return nil
			}
			pid, err := str2pid(consumer.node, consumer.creation, str)
			if err != nil {
				oh.sendError(cmd.ID, cmd.CID, err)
				return nil
			}
			vmessage := cmd.Args["Reason"]
			if vmessage == nil {
				oh.sendError(cmd.ID, cmd.CID, errors.New("Args: {Reason: is empty}"))
				return nil
			}
			sreason, ok := vmessage.(string)
			if ok == false {
				oh.sendError(cmd.ID, cmd.CID, errors.New("Args: {Reason: incorrect value}"))
				return nil
			}
			reason := gen.TerminateReasonNormal
			switch sreason {
			case "", "normal":
				break
			case "shutdown":
				reason = gen.TerminateReasonShutdown
			default:
				reason = errors.New(sreason)
			}
			request := inspect.RequestDoSendExit{
				PID:    pid,
				Reason: reason,
			}

			v, err := oh.Call(inspectProcess, request)
			if err != nil {
				oh.sendError(cmd.ID, cmd.CID, err)
				return nil
			}
			response, ok := v.(inspect.ResponseDoSendExit)
			if ok == false {
				oh.Log().Error("incorrect result (expected inspect.ResponseDoSendExit): %#v", v)
				return nil
			}
			oh.sendMessageResult(cmd.ID, cmd.CID, response)

		case "send_exit_meta":
			vmeta := cmd.Args["ID"]
			if vmeta == nil {
				oh.sendError(cmd.ID, cmd.CID, errors.New("Args: {ID: is empty}"))
				return nil
			}
			str, ok := vmeta.(string)
			if ok == false {
				oh.sendError(cmd.ID, cmd.CID, errors.New("Args: {ID: incorrect value}"))
				return nil
			}
			meta, err := str2alias(consumer.node, consumer.creation, str)
			if err != nil {
				oh.sendError(cmd.ID, cmd.CID, err)
				return nil
			}
			vmessage := cmd.Args["Reason"]
			if vmessage == nil {
				oh.sendError(cmd.ID, cmd.CID, errors.New("Args: {Reason: is empty}"))
				return nil
			}
			sreason, ok := vmessage.(string)
			if ok == false {
				oh.sendError(cmd.ID, cmd.CID, errors.New("Args: {Reason: incorrect value}"))
				return nil
			}
			reason := gen.TerminateReasonNormal
			switch sreason {
			case "", "normal":
				break
			case "shutdown":
				reason = gen.TerminateReasonShutdown
			default:
				reason = errors.New(sreason)
			}
			request := inspect.RequestDoSendExitMeta{
				Meta:   meta,
				Reason: reason,
			}

			v, err := oh.Call(inspectProcess, request)
			if err != nil {
				oh.sendError(cmd.ID, cmd.CID, err)
				return nil
			}
			response, ok := v.(inspect.ResponseDoSendExitMeta)
			if ok == false {
				oh.Log().Error("incorrect result (expected inspect.ResponseDoSendExitMeta): %#v", v)
				return nil
			}
			oh.sendMessageResult(cmd.ID, cmd.CID, response)

		case "kill":
			var pid gen.PID
			vpid := cmd.Args["PID"]
			if vpid == nil {
				oh.sendError(cmd.ID, cmd.CID, errors.New("Args: {PID: is empty}"))
				return nil
			}
			str, ok := vpid.(string)
			if ok == false {
				oh.sendError(cmd.ID, cmd.CID, errors.New("Args: {PID: incorrect value}"))
				return nil
			}
			pid, err := str2pid(consumer.node, consumer.creation, str)
			if err != nil {
				oh.sendError(cmd.ID, cmd.CID, err)
				return nil
			}
			request := inspect.RequestDoKill{
				PID: pid,
			}

			v, err := oh.Call(inspectProcess, request)
			if err != nil {
				oh.sendError(cmd.ID, cmd.CID, err)
				return nil
			}
			response, ok := v.(inspect.ResponseDoKill)
			if ok == false {
				oh.Log().Error("incorrect result (expected inspect.ResponseDoKill): %#v", v)
				return nil
			}
			oh.sendMessageResult(cmd.ID, cmd.CID, response)

		case "set_log_level":
			var request any

			vlevel := cmd.Args["Level"]
			if vlevel == nil {
				oh.sendError(cmd.ID, cmd.CID, errors.New("Args: {Level: is empty}"))
				return nil
			}
			slevel, _ := vlevel.(string)
			level := gen.LogLevelInfo
			switch slevel {
			case "info":
				break
			case "warning":
				level = gen.LogLevelWarning
			case "debug":
				level = gen.LogLevelDebug
			case "error":
				level = gen.LogLevelError
			case "panic":
				level = gen.LogLevelPanic
			case "disabled":
				level = gen.LogLevelDisabled
			default:
				oh.sendError(cmd.ID, cmd.CID, errors.New("Args: {Level: incorrect value}"))
				return nil
			}

			if v, ok := cmd.Args["PID"]; ok {
				str, _ := v.(string)
				pid, err := str2pid(consumer.node, consumer.creation, str)
				if err != nil {
					oh.sendError(cmd.ID, cmd.CID, errors.New("Args: {PID: incorrect value}"))
					return nil
				}
				request = inspect.RequestDoSetLogLevelProcess{
					Level: level,
					PID:   pid,
				}
			}

			if v, ok := cmd.Args["ID"]; ok {
				str, _ := v.(string)
				meta, err := str2alias(consumer.node, consumer.creation, str)
				if err != nil {
					oh.sendError(cmd.ID, cmd.CID, errors.New("Args: {ID: incorrect value}"))
					return nil
				}
				request = inspect.RequestDoSetLogLevelMeta{
					Level: level,
					Meta:  meta,
				}
			}

			if request == nil {
				request = inspect.RequestDoSetLogLevel{
					Level: level,
				}
			}
			v, err := oh.Call(inspectProcess, request)

			if err != nil {
				oh.sendError(cmd.ID, cmd.CID, err)
				return nil
			}

			if response, ok := v.(inspect.ResponseDoSetLogLevel); ok {
				oh.sendMessageResult(cmd.ID, cmd.CID, response)
				return nil
			}
			oh.Log().Error("incorrect result (expected inspect.ResponseDoSetLogLevel): %#v", v)
			return nil

		default:
			oh.Log().Error("unknown 'do' command %q (from %s)", cmd.Name, cmd.ID)
			return nil
		}

	default:
		oh.Log().Error("unknown command from %s: %s", cmd.ID, cmd.Command)
	}
	return nil
}

func (oh *observer_handler) tryConnect(node gen.Atom, args map[string]any) error {

	if node == oh.Node().Name() {
		// itself
		return nil
	}

	if _, err := oh.Node().Network().Node(node); err == nil {
		// already connected
		return nil
	}

	if len(args) == 0 {
		// default
		_, err := oh.Node().Network().GetNode(node)
		return err
	}

	// making connection
	nr := gen.NetworkRoute{}

	if reg, err := oh.Node().Network().Registrar(); err == nil {
		if routes, _ := reg.Resolver().Resolve(node); len(routes) > 0 {
			// take the first one.
			// TODO handle all of them
			nr.Route = routes[0]
		}
	}

	if v, exist := args["Cookie"]; exist {
		nr.Cookie, _ = v.(string)
	}
	if v, exist := args["Host"]; exist {
		nr.Route.Host, _ = v.(string)
	}
	if v, exist := args["Port"]; exist {
		f, _ := v.(float64)
		nr.Route.Port = uint16(f)
	}
	if v, exist := args["TLS"]; exist {
		nr.Route.TLS, _ = v.(bool)
	}

	oh.Node().Network().GetNodeWithRoute(node, nr)

	return nil
}
