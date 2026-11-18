// Package app submission coordinator.
//
// Submission is the single admission entry point for queued user input. Both
// keyboard (task 1.3) and socket (task 2.x) call AdmitSubmission; it validates
// session identity, serializes admission through the inputqueue domain, writes
// a durable receipt (disk or memory-only for incognito), and returns a
// structured result. It owns no UI state and never mutates chat.Session.
//
// Persistence runs in a serialized worker goroutine owned by this coordinator
// (design §3): disk I/O must not occupy the Bubble Tea loop. AdmitSubmission
// enqueues the journal write and waits on a bounded result channel; callers
// stay responsive while the worker performs the I/O.
package app

import (
	"fmt"
	"path/filepath"
	"sync"

	"squid-os/internal/inputqueue"
)

// SubmissionResult classifies the outcome of an admission attempt.
type SubmissionResult struct {
	// OK is true when the request was accepted (newly admitted or idempotent
	// duplicate). False means the request was rejected with Code set.
	OK bool
	// Code is a structured rejection code from inputqueue (empty on success).
	Code string
	// Msg is a human-readable detail (empty on success).
	Msg string
	// Receipt is populated on success (accepted / duplicate).
	Receipt inputqueue.Receipt
	// Entry is the queue entry for this request (original on duplicate).
	Entry *inputqueue.Entry
}

// admissionOp is one unit of work for the persistence worker. The caller has
// already committed the in-memory admit (sequence assigned, queue mutated)
// under c.mu before enqueuing; the op carries everything the worker needs to
// perform the durable write and finalize the result without touching shared
// coordinator state.
type admissionOp struct {
	req   inputqueue.AdmissionRequest
	entry *inputqueue.Entry // newly admitted in-memory entry (nil on duplicate)
	dur   inputqueue.Durability
	done  chan admissionOutcome
}

type admissionOutcome struct {
	res  SubmissionResult
	fail error // non-nil only for store failures that require rollback
}

// SubmissionCoordinator serializes admission for one TUI process. All
// transports funnel through it so ordering, deduplication and persistence are
// shared. It is safe for concurrent use.
type SubmissionCoordinator struct {
	mu     sync.Mutex
	store  inputqueue.Journal
	dur    inputqueue.Durability
	queues map[string]*inputqueue.Queue // sessionID → queue
	// (incognito is probed lazily via c.flag when set, else the constructor closure)

	ops    chan admissionOp // serialized persistence queue (worker-owned)
	closed chan struct{}    // closed when the coordinator is retired (Close)
	wg     sync.WaitGroup

	closeOnce sync.Once      // Close is idempotent; guards double close(c.ops)
	flag      *incognitoFlag // live incognito probe, backfilled by app.New

}

// NewSubmissionCoordinator builds a coordinator rooted at the configured state
// area. Admission receives the current session identity from its caller. A single
// background worker drains the ops queue so journal I/O never runs on the
// caller's goroutine. Call Close when the coordinator is retired.
func NewSubmissionCoordinator(stateRoot string, incognito func() bool) *SubmissionCoordinator {
	c := &SubmissionCoordinator{
		store:  inputqueue.NewStore(filepath.Join(stateRoot, "input-queue")),
		dur:    inputqueue.DurabilityDisk,
		queues: make(map[string]*inputqueue.Queue),

		ops:    make(chan admissionOp, 256),
		closed: make(chan struct{}),
	}
	c.wg.Add(1)
	go c.workerLoop()
	return c
}

// NewMemSubmissionCoordinator builds a memory-only coordinator (incognito
// default, tests). Nothing is written to disk; the same worker model applies
// so behavior matches the disk path.
func NewMemSubmissionCoordinator(incognito func() bool) *SubmissionCoordinator {
	c := &SubmissionCoordinator{
		store:  inputqueue.NewMemStore(),
		dur:    inputqueue.DurabilityMemory,
		queues: make(map[string]*inputqueue.Queue),

		ops:    make(chan admissionOp, 256),
		closed: make(chan struct{}),
	}
	c.wg.Add(1)
	go c.workerLoop()
	return c
}

// Close stops the persistence worker. Pending operations are drained first so
// no admission is lost. Idempotent: safe to call multiple times (e.g. once
// explicitly and once via defer).
func (c *SubmissionCoordinator) Close() {
	c.closeOnce.Do(func() {
		close(c.closed)
		close(c.ops) // closing the channel drains queued ops, then ends range
	})
	c.wg.Wait()
}

// workerLoop executes persisted admissions strictly one at a time (the
// serialized queue-owned worker required by design §3). It exits when c.ops
// is closed (Close), after draining every queued operation.
func (c *SubmissionCoordinator) workerLoop() {
	defer c.wg.Done()
	for op := range c.ops {
		op.done <- c.executeAdmit(op)
	}
}

// executeAdmit performs the durable journal write and finalizes the result.
// Runs on the worker goroutine. The in-memory admit was already committed by
// the caller under c.mu, so no coordinator state is touched here; only the
// store (serialized through this single worker for admissions) and the
// op's own entry/receipt data are used.
func (c *SubmissionCoordinator) executeAdmit(op admissionOp) admissionOutcome {
	err := c.store.Admit(op.entry)
	switch {
	case err == nil:
		return admissionOutcome{res: SubmissionResult{OK: true, Receipt: receiptFor(op.entry, false, op.dur), Entry: op.entry}}
	case err == inputqueue.ErrDuplicate:
		// Journal already has it (restart/retry path): treat as duplicate.
		return admissionOutcome{res: SubmissionResult{OK: true, Receipt: receiptFor(op.entry, true, op.dur), Entry: op.entry}}
	default:
		// Durable write failed: roll back the in-memory admit so state stays
		// consistent and the client keeps its draft/outbox.
		c.rollbackAdmit(op.req.SessionID, op.entry)
		return admissionOutcome{res: SubmissionResult{Code: inputqueue.CodeStoreError, Msg: err.Error()}, fail: err}
	}
}

// receiptFor builds a receipt from an immutable entry without touching live
// queue state. Safe to call from the worker goroutine.
func receiptFor(e *inputqueue.Entry, dup bool, dur inputqueue.Durability) inputqueue.Receipt {
	return inputqueue.Receipt{
		RequestID:  e.RequestID,
		Status:     inputqueue.StatusAccepted,
		Durability: dur,
		SessionID:  e.SessionID,
		Boundary:   e.Boundary,
		Sequence:   e.Sequence,
		Duplicate:  dup,
	}
}

// Durability reports where admissions are recorded for the current session.
func (c *SubmissionCoordinator) Durability() inputqueue.Durability {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.durabilityLocked()
}

// durabilityLocked returns the effective durability. Caller holds c.mu.
func (c *SubmissionCoordinator) durabilityLocked() inputqueue.Durability {
	if c.flag != nil && c.flag.get() {
		return inputqueue.DurabilityMemory
	}
	return c.dur
}

// Counts returns pending-entry counts for the given session.
func (c *SubmissionCoordinator) Counts(sessionID string) inputqueue.Counts {
	c.mu.Lock()
	q := c.queueForLocked(sessionID)
	c.mu.Unlock()
	return q.Counts()
}

// Find looks up an entry by request ID within a session (any state).
func (c *SubmissionCoordinator) Find(sessionID, requestID string) (*inputqueue.Entry, bool) {
	c.mu.Lock()
	q := c.queueForLocked(sessionID)
	c.mu.Unlock()
	return q.Find(requestID)
}

// Pending returns pending entries in admission order for a session. Entries
// are defensive copies (same contract as Queue.All).
func (c *SubmissionCoordinator) Pending(sessionID string) []*inputqueue.Entry {
	c.mu.Lock()
	q := c.queueForLocked(sessionID)
	c.mu.Unlock()
	return q.Pending()
}

// Discard marks all pending entries for a session discarded (explicit flush).
// Returns how many were discarded, or -1 if the durable discard failed.
// In-memory state is rolled back on failure so it never diverges from the
// journal.
func (c *SubmissionCoordinator) Discard(sessionID string) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	q := c.queueForLocked(sessionID)
	pending := q.Pending()
	if len(pending) == 0 {
		return 0
	}
	snapshot := make([]inputqueue.Entry, 0, len(pending))
	for _, e := range q.All() {
		snapshot = append(snapshot, *e)
	}
	q.Discard(pending)
	err := c.store.UpdateState(sessionID, func(entries []*inputqueue.Entry) {
		for _, e := range entries {
			if e.State == inputqueue.StatePending {
				e.State = inputqueue.StateDiscarded
			}
		}
	})
	if err != nil {
		// Durable discard failed: restore the pre-discard in-memory state so
		// the queue still matches the journal, and report the failure.
		q.Load(snapshot)
		return -1
	}
	return len(pending)
}

// Drain atomically selects all pending entries for a boundary (FIFO,
// admission order) and marks them prepared-for-injection durably. It is the
// boundary transaction's first half: the caller appends the returned entries'
// text to the session, then calls MarkDrained to finalize with the stable
// message IDs. Selection and preparation happen under one lock so admissions
// arriving during the drain wait for the NEXT boundary (design §3 rule 4).
func (c *SubmissionCoordinator) Drain(sessionID string, boundary inputqueue.Boundary) []*inputqueue.Entry {
	c.mu.Lock()
	defer c.mu.Unlock()
	q := c.queueForLocked(sessionID)
	eligible := q.SelectEligible(boundary)
	if len(eligible) == 0 {
		return nil
	}
	q.Prepare(eligible)
	_ = c.store.UpdateState(sessionID, func(es []*inputqueue.Entry) {
		for i := range es {
			for _, sel := range eligible {
				if es[i].RequestID == sel.RequestID && es[i].State == inputqueue.StatePending {
					es[i].State = inputqueue.StatePrepared
				}
			}
		}
	})
	return eligible
}

// MarkDrained finalizes a drained batch: records the injected stable message
// IDs on each entry (matched by request ID) and transitions them to injected
// durably. Call after the session checkpoint holds the appended messages.
func (c *SubmissionCoordinator) MarkDrained(sessionID string, msgIDsByRequest map[string][]string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	_ = c.store.UpdateState(sessionID, func(es []*inputqueue.Entry) {
		for i := range es {
			ids, ok := msgIDsByRequest[es[i].RequestID]
			if !ok {
				continue
			}
			es[i].State = inputqueue.StateInjected
			es[i].InjectedMsgIDs = ids
		}
	})
	if entries, err := c.store.Load(sessionID); err == nil {
		c.queueForLocked(sessionID).Load(entries)
	}
}

// LoadSession hydrates the in-memory queue for a session from its persisted
// journal (crash recovery).
// the scheduler never auto-runs them. Safe to call repeatedly.
func (c *SubmissionCoordinator) LoadSession(sessionID string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	entries, err := c.store.Load(sessionID)
	if err != nil {
		return fmt.Errorf("%s: %v", inputqueue.CodeStoreError, err)
	}
	q := c.queueForLocked(sessionID)
	q.Load(entries)
	return nil
}

// ReconcileInjected reconciles recovered queue entries against the session's
// persisted checkpoint after a crash (design §4). injectedMsgIDs maps the
// stable message IDs present in the persisted session history.
//
// Rule: a prepared entry whose stable message ID is present in the checkpoint
// was already injected before the crash ⇒ mark injected durably. A prepared
// entry absent from the checkpoint was never appended ⇒ revert to pending.
// Either way the session recovers PAUSED: reconciliation never re-appends
// messages and never auto-runs inference.
func (c *SubmissionCoordinator) ReconcileInjected(sessionID string, injectedMsgIDs map[string]bool) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	entries, err := c.store.Load(sessionID)
	if err != nil {
		return fmt.Errorf("%s: %v", inputqueue.CodeStoreError, err)
	}
	changed := false
	for i := range entries {
		e := &entries[i]
		if e.State == inputqueue.StatePrepared && len(e.InjectedMsgIDs) > 0 {
			// Design §4: a prepared entry whose stable message ID is present in
			// the persisted checkpoint was already injected before the crash ⇒
			// mark injected. Absent (including an empty checkpoint, where nothing
			// could have been persisted) ⇒ never appended ⇒ revert to pending.
			inCheckpoint := false
			for _, mid := range e.InjectedMsgIDs {
				if injectedMsgIDs[mid] {
					inCheckpoint = true
					break
				}
			}
			if inCheckpoint {
				e.State = inputqueue.StateInjected
			} else {
				e.State = inputqueue.StatePending
			}
			changed = true
		}
	}
	if changed {
		if err := c.store.UpdateState(sessionID, func(es []*inputqueue.Entry) {
			for i := range es {
				es[i].State = entries[i].State
				es[i].InjectedMsgIDs = entries[i].InjectedMsgIDs
			}
		}); err != nil {
			return fmt.Errorf("%s: %v", inputqueue.CodeStoreError, err)
		}
	}
	c.queueForLocked(sessionID).Load(entries)
	return nil
}

// AdmitSubmission validates and durably admits a request. It is the public
// surface called by both keyboard and socket transports.
//
// Semantics (design §4):
//   - same request ID + identical payload → idempotent duplicate receipt;
//   - same request ID + different payload → id_conflict rejection;
//   - identical text with different IDs → two distinct messages;
//   - acceptance is reported only after the durable journal write succeeds;
//   - incognito sessions report durability "memory" and write nothing to disk.
//
// The journal write itself runs on the coordinator's serialized worker
// goroutine (design §3); this method enqueues the operation and blocks on a
// bounded result channel until the worker finishes it. The coordinator mutex
// is NOT held across the handoff: the in-memory admit is fully committed
// under c.mu first, then the lock is released before enqueueing so the
// worker never needs c.mu to make progress (no self-deadlock).
func (c *SubmissionCoordinator) AdmitSubmission(req inputqueue.AdmissionRequest, live string) SubmissionResult {
	if err := req.Validate(); err != nil {
		return rejectFromError(err)
	}

	c.mu.Lock()

	// Session identity binding: the request must target the live session.
	// Stale/foreign identities are rejected, never redirected.
	if live != "" && req.SessionID != live {
		c.mu.Unlock()
		return SubmissionResult{Code: inputqueue.CodeWrongSession, Msg: fmt.Sprintf("request targets session %q but live session is %q", req.SessionID, live)}
	}

	dur := c.durabilityLocked()
	q := c.queueForLocked(req.SessionID)

	e, err := q.Admit(req, inputqueue.Now())
	if err != nil {
		if err == inputqueue.ErrDuplicate {
			// Idempotent retry: return the existing receipt. Fully resolved
			// here; no durable write is needed for an already-admitted entry.
			res := SubmissionResult{OK: true, Receipt: q.Receipt(e, true, dur), Entry: e}
			c.mu.Unlock()
			return res
		}
		c.mu.Unlock()
		return rejectFromError(err)
	}

	// Commit point: sequence assigned and queue mutated atomically above.
	// Release the lock BEFORE handing off so the worker can finalize without
	// waiting on this caller.
	c.mu.Unlock()

	// Enqueue the durable write for the serialized worker and wait for its
	// bounded result. Disk I/O happens off the caller's goroutine.
	op := admissionOp{req: req, entry: e, dur: dur, done: make(chan admissionOutcome, 1)}
	select {
	case c.ops <- op:
	case <-c.closed:
		// Coordinator closed while we were about to enqueue: the in-memory
		// admit is committed but not durable. Report the failure rather than
		// block forever or silently claim durability.
		return SubmissionResult{Code: inputqueue.CodeStoreError, Msg: "submission coordinator closed before durable write"}
	}
	outcome := <-op.done
	return outcome.res
}

// rollbackAdmit removes a just-admitted in-memory entry whose durable write
// failed. Runs on the worker goroutine; takes c.mu briefly.
//
// Behavior note: if reloading the journal also fails, the in-memory queue is
// left untouched (it may contain one extra non-durable entry) and the reload
// error is returned so the caller can log/surface it instead of silently
// swallowing it.
func (c *SubmissionCoordinator) rollbackAdmit(sessionID string, e *inputqueue.Entry) error {
	// Re-load from the journal to drop the non-durable entry.
	entries, err := c.store.Load(sessionID)
	if err != nil {
		return fmt.Errorf("%s: rollback reload failed, in-memory queue may be ahead of journal: %v", inputqueue.CodeStoreError, err)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.queueForLocked(sessionID).Load(entries)
	return nil
}

// queueForLocked returns (creating if needed) the queue for a session. Caller
// holds c.mu.
func (c *SubmissionCoordinator) queueForLocked(sessionID string) *inputqueue.Queue {
	q, ok := c.queues[sessionID]
	if !ok {
		q = inputqueue.NewQueue(sessionID)
		c.queues[sessionID] = q
	}
	return q
}

// rejectFromError maps a structured inputqueue error into a rejection result
// using typed codes (errors.As), never string matching.
func rejectFromError(err error) SubmissionResult {
	code := inputqueue.AsCode(err)
	return SubmissionResult{Code: code, Msg: err.Error()}
}
