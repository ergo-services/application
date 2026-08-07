package grain

import (
	"context"
	"errors"
	"fmt"
	"time"

	"ergo.services/application/grain/store"
	"ergo.services/ergo/act"
	"ergo.services/ergo/gen"
)

func factoryLeaseHolder() gen.ProcessBehavior {
	return &leaseHolder{}
}

type messageRenewTick struct{ gen uint64 }

// leaseHolder is the per-node singleton that owns the node lease. It acquires
// (or resumes) the lease, publishes the node incarnation for activators to
// stamp keys with, and renews the lease on a heartbeat.
type leaseHolder struct {
	act.Actor

	opts        Options
	node        gen.Atom
	data        *catalog
	renewGen    uint64
	lastRenewOK time.Time
}

func (l *leaseHolder) Init(args ...any) error {
	l.opts = args[0].(Options)
	l.node = l.Node().Name()
	d, _ := catalogs.LoadOrStore(catalogKey{node: l.node, domain: l.opts.Domain}, &catalog{opts: l.opts})
	l.data = d.(*catalog)

	lease, err := l.acquireOrResume()
	if err != nil {
		return err // Init error frees the name cleanly; Transient restart retries
	}
	l.data.incarnation.Store(int64(lease.Incarnation))
	l.lastRenewOK = time.Now()
	l.arm()
	return nil
}

// acquireOrResume resumes an existing lease (this is an actor restart while the
// node is up) or mints a fresh incarnation on first boot. With an in-memory
// store a node restart starts from an empty store, so Renew reports ErrNoLease
// and a fresh incarnation is minted. A persistent backend additionally needs a
// node boot token to fence a stale lease after a same-node reboot (deferred).
func (l *leaseHolder) acquireOrResume() (store.Lease, error) {
	ctx, cancel := context.WithTimeout(context.Background(), l.opts.StoreIOTimeout)
	defer cancel()
	if lease, err := l.opts.Store.Renew(ctx, l.node); err == nil {
		return lease, nil
	}
	return l.opts.Store.AcquireLease(ctx, l.node, l.opts.LeaseTTL)
}

func (l *leaseHolder) arm() {
	l.renewGen++
	l.SendAfter(l.PID(), messageRenewTick{gen: l.renewGen}, l.opts.RenewInterval)
}

func (l *leaseHolder) HandleMessage(from gen.PID, message any) error {
	m, ok := message.(messageRenewTick)
	if ok == false {
		return nil
	}
	if m.gen != l.renewGen {
		return nil // stale reschedule ghost
	}
	l.arm() // re-arm before the IO so beat spacing excludes renew latency

	ctx, cancel := context.WithTimeout(context.Background(), l.opts.StoreIOTimeout)
	_, err := l.opts.Store.Renew(ctx, l.node)
	cancel()
	if err == nil {
		l.lastRenewOK = time.Now()
		return nil
	}
	if errors.Is(err, store.ErrNoLease) {
		return err // lost the lease; crash and let the Transient restart re-acquire
	}
	// Watchdog: a repeatedly failing renew must not be swallowed while the lease
	// ages toward expiry.
	ttl := time.Duration(l.opts.LeaseTTL) * time.Second
	if time.Since(l.lastRenewOK) >= ttl-l.opts.RenewInterval {
		return fmt.Errorf("grain: lease heartbeat watchdog: %w", err)
	}
	return nil
}

func (l *leaseHolder) Terminate(reason error) {
	ctx, cancel := context.WithTimeout(context.Background(), l.opts.StoreIOTimeout)
	defer cancel()
	_ = l.opts.Store.DropLease(ctx, l.node)
}
