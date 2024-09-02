package observer

import "ergo.services/ergo/gen"

type consumer struct {
	id            gen.Alias
	node          gen.Atom
	creation      int64
	subscriptions map[string]gen.Event
}

type subscription struct {
	event     gen.Event
	consumers map[gen.Alias]bool
	last      *gen.MessageEvent
}
