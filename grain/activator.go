package grain

import (
	"context"
	"errors"
	"time"

	"ergo.services/application/grain/store"
	"ergo.services/ergo/act"
	"ergo.services/ergo/gen"
)

func factoryActivator() gen.ProcessBehavior {
	return &activator{}
}

// activator owns one shard of the keyspace on this node. It serves activation
// requests, runs the two-phase boot, and monitors the grains it spawned.
type activator struct {
	act.Actor

	opts         Options
	index        int
	node         gen.Atom
	domain       gen.Atom
	backend      store.Backend
	data         *catalog
	grainFactory gen.ProcessFactory
}

func (a *activator) Init(args ...any) error {
	a.opts = args[0].(Options)
	a.index = args[1].(int)
	a.node = a.Node().Name()
	a.domain = a.opts.Domain
	a.backend = a.opts.Store
	d, _ := catalogs.LoadOrStore(catalogKey{node: a.node, domain: a.domain}, &catalog{opts: a.opts})
	a.data = d.(*catalog)
	a.grainFactory = func() gen.ProcessBehavior { return a.opts.Factory() }

	// A restarted activator inherits the live map but not the previous instance's
	// monitors; re-monitor the grains this shard owns so their death is observed.
	a.data.live.Range(func(k, v any) bool {
		key := k.(string)
		if activatorIndexFor(a.node, a.domain, key) != a.index {
			return true
		}
		if err := a.MonitorPID(v.(liveGrain).pid); err != nil {
			a.data.live.Delete(key) // already dead
		}
		return true
	})
	return nil
}

func (a *activator) HandleCall(from gen.PID, ref gen.Ref, request any) (any, error) {
	switch r := request.(type) {
	case activateRequest:
		pid, err := a.activate(r.key)
		if err != nil {
			a.SendResponseError(from, ref, err)
			return nil, nil
		}
		a.SendResponse(from, ref, activateResponse{pid: pid})
		return nil, nil

	case deleteRequest:
		if err := a.remove(r.key); err != nil {
			a.SendResponseError(from, ref, err)
			return nil, nil
		}
		a.SendResponse(from, ref, deleteResponse{})
		return nil, nil
	}
	a.SendResponseError(from, ref, gen.ErrUnsupported)
	return nil, nil
}

func (a *activator) HandleMessage(from gen.PID, message any) error {
	m, ok := message.(gen.MessageDownPID)
	if ok == false {
		return nil
	}
	a.data.live.Range(func(k, v any) bool {
		if v.(liveGrain).pid == m.PID {
			a.data.live.Delete(k.(string))
			return false
		}
		return true
	})
	return nil
}

// Terminate drains the grains this shard owns on an orderly stop so they flush
// state and release their locks. A crash/restart of the activator itself is not
// an orderly stop and must leave its grains running.
func (a *activator) Terminate(reason error) {
	if errors.Is(reason, gen.TerminateReasonShutdown) == false {
		return
	}
	a.data.live.Range(func(k, v any) bool {
		if activatorIndexFor(a.node, a.domain, k.(string)) == a.index {
			a.SendExit(v.(liveGrain).pid, gen.TerminateReasonShutdown)
		}
		return true
	})
}

// activate ensures a grain for key is live and returns its PID. Two-phase boot:
// bounded Acquire, bounded LoadState, then SpawnRegister (whose Init does no
// Store IO). ErrTaken/ErrTimeout mean the previous incarnation's name has not
// been freed yet - retry with bounded backoff.
func (a *activator) activate(key string) (gen.PID, error) {
	if v, ok := a.data.live.Load(key); ok {
		return v.(liveGrain).pid, nil
	}
	inc := store.Incarnation(a.data.incarnation.Load())
	if inc == 0 {
		return gen.PID{}, ErrNoIncarnation
	}

	deadline := time.Now().Add(maxActivateWall)
	backoff := 20 * time.Millisecond

	for attempt := 0; attempt < maxActivateAttempts; attempt++ {
		epoch, err := a.acquire(key, inc)
		if err != nil {
			return gen.PID{}, err
		}
		state, err := a.loadState(key)
		if err != nil {
			return gen.PID{}, err
		}
		pid, err := a.SpawnRegister(grainName(a.domain, key), a.grainFactory,
			gen.ProcessOptions{InitTimeout: a.opts.GrainInitSecs},
			grainArgs{
				key:            key,
				state:          state,
				epoch:          epoch,
				backend:        a.backend,
				idleTimeout:    a.opts.IdleTimeout,
				storeIOTimeout: a.opts.StoreIOTimeout,
				syncEvery:      a.opts.SyncEvery,
			})
		if errors.Is(err, gen.ErrTaken) || errors.Is(err, gen.ErrTimeout) {
			if time.Now().After(deadline) {
				return gen.PID{}, ErrActivateExhausted
			}
			time.Sleep(backoff)
			if backoff *= 2; backoff > 500*time.Millisecond {
				backoff = 500 * time.Millisecond
			}
			continue
		}
		if err != nil {
			return gen.PID{}, err
		}
		a.data.live.Store(key, liveGrain{pid: pid, epoch: epoch})
		a.MonitorPID(pid)
		return pid, nil
	}
	return gen.PID{}, ErrActivateExhausted
}

// acquire takes ownership of key at inc and returns the granted epoch. On
// ErrAlreadyOwned (single node: a stale Running stamp under our own incarnation)
// it reuses the existing epoch.
func (a *activator) acquire(key string, inc store.Incarnation) (store.Epoch, error) {
	ctx, cancel := context.WithTimeout(context.Background(), a.opts.StoreIOTimeout)
	defer cancel()
	stamp, err := a.backend.Acquire(ctx, key, a.node, inc)
	if err == nil {
		return stamp.Epoch, nil
	}
	if errors.Is(err, store.ErrAlreadyOwned) {
		existing, rerr := a.backend.Read(ctx, key)
		if rerr != nil {
			return 0, rerr
		}
		return existing.Epoch, nil
	}
	return 0, err
}

func (a *activator) loadState(key string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), a.opts.StoreIOTimeout)
	defer cancel()
	state, err := a.backend.LoadState(ctx, key)
	if errors.Is(err, store.ErrNoState) {
		return nil, nil
	}
	return state, err
}

// remove deactivates a live grain (if any) and deletes its stamp and state.
func (a *activator) remove(key string) error {
	var epoch store.Epoch
	if v, ok := a.data.live.Load(key); ok {
		lg := v.(liveGrain)
		epoch = lg.epoch
		a.data.live.Delete(key)
		a.SendExit(lg.pid, gen.TerminateReasonKill)
	} else {
		ctx, cancel := context.WithTimeout(context.Background(), a.opts.StoreIOTimeout)
		stamp, err := a.backend.Read(ctx, key)
		cancel()
		if errors.Is(err, store.ErrNoStamp) {
			return nil // already gone
		}
		if err != nil {
			return err
		}
		epoch = stamp.Epoch
	}
	ctx, cancel := context.WithTimeout(context.Background(), a.opts.StoreIOTimeout)
	defer cancel()
	return a.backend.Delete(ctx, key, epoch)
}
