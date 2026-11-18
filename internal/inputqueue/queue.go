package inputqueue

import (
	"fmt"
	"sync"
	"time"
)

// Queue is the in-memory FIFO queue for one session identity. It owns entry
// state transitions and selection; durability is owned by Store.
//
// A Queue is NOT safe for concurrent use on its own — the application
// coordinator serializes all mutations. The mutex guards only against
// accidental data races during testing.
type Queue struct {
	mu        sync.Mutex
	sessionID string
	entries   []*Entry
	byID      map[string]*Entry // request ID → entry (all states, for dedup receipts)
	nextSeq   int64
}

// NewQueue creates an empty queue bound to a session identity.
func NewQueue(sessionID string) *Queue {
	return &Queue{
		sessionID: sessionID,
		byID:      make(map[string]*Entry),
	}
}

// SessionID returns the session this queue belongs to.
func (q *Queue) SessionID() string { return q.sessionID }

// Load hydrates the queue from persisted entries (crash recovery). Entries
// are expected sorted by sequence; the highest sequence determines nextSeq.
func (q *Queue) Load(entries []Entry) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.entries = nil
	q.byID = make(map[string]*Entry, len(entries))
	for _, e := range entries {
		e := e
		q.addLocked(&e)
	}
}

func (q *Queue) addLocked(e *Entry) {
	q.entries = append(q.entries, e)
	q.byID[e.RequestID] = e
	if e.Sequence >= q.nextSeq {
		q.nextSeq = e.Sequence + 1
	}
}

// Admit appends a new pending entry with the next server sequence number.
// It enforces the documented safety limits and rejects (never evicts).
// Returns the stored entry or a structured error. ErrDuplicate signals an
// idempotent retry (same request ID and identical payload); the returned
// entry is then the original.
func (q *Queue) Admit(req AdmissionRequest, admittedAt time.Time) (*Entry, error) {
	q.mu.Lock()
	defer q.mu.Unlock()

	if existing, ok := q.byID[req.RequestID]; ok {
		if payloadEqual(existing, req) {
			return existing, ErrDuplicate
		}
		return nil, newCodeErr(CodeIDConflict, "%s: request id %q already exists with a different payload", CodeIDConflict, req.RequestID)
	}

	pending := q.pendingLocked()
	if len(pending)+1 >= MaxPendingEntries {
		return nil, newCodeErr(CodeQueueFull, "%s: session has %d pending entries, limit is %d", CodeQueueFull, len(pending), MaxPendingEntries)
	}
	pendingBytes := 0
	for _, e := range pending {
		pendingBytes += len(e.Text)
	}
	if pendingBytes+len(req.Text) > MaxPendingBytes {
		return nil, newCodeErr(CodeQueueFull, "%s: pending text would be %d bytes, limit is %d", CodeQueueFull, pendingBytes+len(req.Text), MaxPendingBytes)
	}

	e := &Entry{
		RequestID:  req.RequestID,
		Sequence:   q.nextSeq,
		SessionID:  q.sessionID,
		Source:     req.Source,
		Text:       req.Text,
		Boundary:   req.Boundary,
		AdmittedAt: admittedAt,
		State:      StatePending,
	}
	q.addLocked(e)
	return e, nil
}

// ErrDuplicate signals an idempotent retry: same request ID and identical
// payload. The returned entry is the original.
var ErrDuplicate = fmt.Errorf("duplicate_request")

// Find returns the entry for a request ID in any state.
func (q *Queue) Find(requestID string) (*Entry, bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	e, ok := q.byID[requestID]
	return e, ok
}

// Pending returns pending entries in admission order. It returns defensive
// copies of the entries (same contract as All): callers may mutate the
// returned values without affecting queue state.
func (q *Queue) Pending() []*Entry {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.pendingLocked()
}

func (q *Queue) pendingLocked() []*Entry {
	out := make([]*Entry, 0, len(q.entries))
	for _, e := range q.entries {
		if e.State == StatePending {
			c := *e
			out = append(out, &c)
		}
	}
	return out
}

// Counts returns pending counts per boundary.
func (q *Queue) Counts() Counts {
	var c Counts
	for _, e := range q.Pending() {
		switch e.Boundary {
		case BoundaryStep:
			c.Step++
		case BoundaryTurn:
			c.Turn++
		}
	}
	return c
}

// SelectEligible returns eligible pending entries in admission (FIFO) order
// for the given boundary. Selection does not mutate state and returns
// defensive copies of the entries (same contract as Pending/All).
func (q *Queue) SelectEligible(boundary Boundary) []*Entry {
	out := make([]*Entry, 0)
	for _, e := range q.Pending() {
		if e.Boundary == boundary {
			out = append(out, e)
		}
	}
	return out
}

// Prepare marks entries as prepared-for-injection. Callers may pass the
// exact entries returned by SelectEligible; matching is by request ID so
// defensive copies work too.
func (q *Queue) Prepare(entries []*Entry) {
	q.mu.Lock()
	defer q.mu.Unlock()
	for _, e := range entries {
		q.setLocked(e, StatePrepared)
	}
}

// MarkInjected records the injected stable message IDs and finalizes the
// entry.
func (q *Queue) MarkInjected(entry *Entry, msgIDs []string) {
	q.mu.Lock()
	defer q.mu.Unlock()
	entry.State = StateInjected
	entry.InjectedMsgIDs = append([]string(nil), msgIDs...)
}

// Discard marks entries discarded (explicit flush/clear). A discarded entry
// stays discarded on retry.
func (q *Queue) Discard(entries []*Entry) {
	q.mu.Lock()
	defer q.mu.Unlock()
	for _, e := range entries {
		q.setLocked(e, StateDiscarded)
	}
}

// setLocked transitions a stored entry to a new state, matching by request ID
// so it works with both internal pointers and defensive copies returned by
// Pending/SelectEligible. Caller holds q.mu.
func (q *Queue) setLocked(e *Entry, st State) {
	stored, ok := q.byID[e.RequestID]
	if !ok {
		return
	}
	stored.State = st
}

// All returns every entry (any state) in admission order, for persistence.
func (q *Queue) All() []*Entry {
	q.mu.Lock()
	defer q.mu.Unlock()
	out := make([]*Entry, len(q.entries))
	copy(out, q.entries)
	return out
}

// NextSequence peeks the next server sequence number without consuming it.
func (q *Queue) NextSequence() int64 {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.nextSeq
}

// Receipt builds a receipt for an admitted (or duplicate) entry.
func (q *Queue) Receipt(e *Entry, dup bool, dur Durability) Receipt {
	return Receipt{
		RequestID:  e.RequestID,
		Status:     StatusAccepted,
		Durability: dur,
		SessionID:  q.sessionID,
		Boundary:   e.Boundary,
		Sequence:   e.Sequence,
		Counts:     q.Counts(),
		Duplicate:  dup,
	}
}

// ReceiptForState builds a status receipt for an entry in any state.
func (q *Queue) ReceiptForState(e *Entry, dur Durability) Receipt {
	r := q.Receipt(e, false, dur)
	switch e.State {
	case StateInjected:
		r.InjectedMsgIDs = e.InjectedMsgIDs
	case StateDiscarded:
		r.Status = StatusRejected
	}
	return r
}

func payloadEqual(e *Entry, req AdmissionRequest) bool {
	return e.SessionID == req.SessionID &&
		e.Text == req.Text &&
		e.Boundary == req.Boundary
}
