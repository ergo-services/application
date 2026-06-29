package maestro

import (
	"ergo.services/actor/saga"
	"ergo.services/ergo/gen"
)

// Options configures the maestro application.
type Options struct {
	// Saga is the factory for the user's saga.Actor. maestro spawns one instance
	// per run and drives it with a MessageBegin. The instance is spawned with the
	// run's RunID as its first Init argument so it can be used as an idempotency
	// key for re-driven runs. Required.
	Saga gen.ProcessFactory

	// Journal persists run intents so incomplete runs can be re-driven after a
	// restart. nil installs an in-memory journal (no cross-process durability;
	// survives a manager restart but not a node restart).
	Journal Journal

	// TxOptions are applied to the transaction started for each run.
	TxOptions saga.TransactionOptions
}
