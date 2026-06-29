package maestro

// RunRecord describes a run recorded as started but not yet completed.
type RunRecord struct {
	ID    RunID
	Input any
}

// Journal persists run intents so incomplete runs can be re-driven after a
// restart.
//
// Implementations MUST be non-blocking when called from the manager's
// callbacks: do not block on disk/network I/O directly. A durable
// implementation should back itself with an internal actor or an async buffer.
// The default MemoryJournal is non-blocking but offers no cross-process
// durability.
type Journal interface {
	// Started records a run before its instance is spawned. Returning an error
	// aborts the run (fail-fast): no instance is spawned and Run reports the error.
	Started(id RunID, input any) error

	// Completed marks a run terminally settled (done or cancelled) so it is not
	// re-driven.
	Completed(id RunID, result any, reason error) error

	// Incomplete returns runs recorded as Started but not Completed, for re-drive
	// on manager start.
	Incomplete() ([]RunRecord, error)
}

// MemoryJournal is the default in-memory Journal. It is intended to be accessed
// only by the single manager goroutine, so it holds no locks. It survives a
// manager restart (the instance lives in the application options) but not a node
// restart.
type MemoryJournal struct {
	runs map[RunID]any
}

// NewMemoryJournal creates an empty in-memory journal.
func NewMemoryJournal() *MemoryJournal {
	return &MemoryJournal{runs: make(map[RunID]any)}
}

func (j *MemoryJournal) Started(id RunID, input any) error {
	j.runs[id] = input
	return nil
}

func (j *MemoryJournal) Completed(id RunID, _ any, _ error) error {
	delete(j.runs, id)
	return nil
}

func (j *MemoryJournal) Incomplete() ([]RunRecord, error) {
	records := make([]RunRecord, 0, len(j.runs))
	for id, input := range j.runs {
		records = append(records, RunRecord{ID: id, Input: input})
	}
	return records, nil
}
