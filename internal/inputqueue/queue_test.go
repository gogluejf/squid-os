package inputqueue

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

func fixedTime() time.Time { return time.Date(2026, 11, 17, 12, 0, 0, 0, time.UTC) }

func req(id, session, text string, b Boundary) AdmissionRequest {
	return AdmissionRequest{RequestID: id, SessionID: session, Source: "test", Text: text, Boundary: b}
}

func TestValidate(t *testing.T) {
	cases := []struct {
		name    string
		req     AdmissionRequest
		wantErr string
	}{
		{"valid step", req("11111111-2222-3333-4444-555555555555", "s1", "hello", BoundaryStep), ""},
		{"valid turn", req("11111111-2222-3333-4444-555555555555", "s1", "hello", BoundaryTurn), ""},
		{"empty request id", req("", "s1", "hi", BoundaryStep), CodeInvalid},
		{"non-uuid request id", req("not-a-uuid", "s1", "hi", BoundaryStep), CodeInvalid},
		{"empty session", req("11111111-2222-3333-4444-555555555555", "", "hi", BoundaryStep), CodeInvalid},
		{"bad boundary", req("11111111-2222-3333-4444-555555555555", "s1", "hi", Boundary("bogus")), CodeInvalid},
		{"oversize", req("11111111-2222-3333-4444-555555555555", "s1", strings.Repeat("x", MaxRequestBytes+1), BoundaryStep), CodeOversize},
		{"exactly at limit", req("11111111-2222-3333-4444-555555555555", "s1", strings.Repeat("x", MaxRequestBytes), BoundaryStep), ""},
		{"blank text", req("11111111-2222-3333-4444-555555555555", "s1", "   \n\t ", BoundaryStep), CodeEmpty},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.req.Validate()
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("Validate() = %v, want nil", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("Validate() = %v, want error containing %q", err, tc.wantErr)
			}
		})
	}
}

func TestAdmitAssignsMonotonicSequence(t *testing.T) {
	q := NewQueue("s1")
	for i, text := range []string{"a", "b", "c"} {
		e, err := q.Admit(req(fmt.Sprintf("11111111-2222-3333-4444-%012d", i), "s1", text, BoundaryStep), fixedTime())
		if err != nil {
			t.Fatalf("Admit(%d): %v", i, err)
		}
		if e.Sequence != int64(i) {
			t.Fatalf("sequence = %d, want %d", e.Sequence, i)
		}
		if e.State != StatePending {
			t.Fatalf("state = %q, want pending", e.State)
		}
		if e.SessionID != "s1" {
			t.Fatalf("session = %q", e.SessionID)
		}
	}
	if got := q.NextSequence(); got != 3 {
		t.Fatalf("NextSequence = %d, want 3", got)
	}
}

func TestDedupSameIDSamePayloadIsIdempotent(t *testing.T) {
	q := NewQueue("s1")
	r := req("11111111-2222-3333-4444-555555555555", "s1", "same text", BoundaryTurn)
	first, err := q.Admit(r, fixedTime())
	if err != nil {
		t.Fatal(err)
	}
	second, err := q.Admit(r, fixedTime().Add(time.Second))
	if err != ErrDuplicate {
		t.Fatalf("second Admit err = %v, want ErrDuplicate", err)
	}
	if second.RequestID != first.RequestID || second.Sequence != first.Sequence {
		t.Fatalf("duplicate returned different entry: %+v vs %+v", second, first)
	}
	if len(q.Pending()) != 1 {
		t.Fatalf("pending = %d, want 1", len(q.Pending()))
	}
}

func TestDedupSameIDDifferentPayloadConflicts(t *testing.T) {
	q := NewQueue("s1")
	id := "11111111-2222-3333-4444-555555555555"
	if _, err := q.Admit(req(id, "s1", "original", BoundaryStep), fixedTime()); err != nil {
		t.Fatal(err)
	}
	for _, mut := range []struct {
		name string
		r    AdmissionRequest
	}{
		{"different text", req(id, "s1", "changed", BoundaryStep)},
		{"different boundary", req(id, "s1", "original", BoundaryTurn)},
		{"different session", req(id, "s2", "original", BoundaryStep)},
	} {
		t.Run(mut.name, func(t *testing.T) {
			_, err := q.Admit(mut.r, fixedTime())
			if err == nil || !strings.Contains(err.Error(), CodeIDConflict) {
				t.Fatalf("err = %v, want id_conflict", err)
			}
		})
	}
}

func TestIdenticalTextDifferentIDsAreDistinct(t *testing.T) {
	q := NewQueue("s1")
	a, err := q.Admit(req("11111111-2222-3333-4444-555555555555", "s1", "repeat", BoundaryStep), fixedTime())
	if err != nil {
		t.Fatal(err)
	}
	b, err := q.Admit(req("aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee", "s1", "repeat", BoundaryStep), fixedTime())
	if err != nil {
		t.Fatalf("distinct IDs with identical text must both admit: %v", err)
	}
	if a.Sequence == b.Sequence {
		t.Fatalf("both entries share sequence %d", a.Sequence)
	}
	if c := q.Counts(); c.Step != 2 {
		t.Fatalf("counts = %+v, want step=2", c)
	}
}

func TestLimitsRejectNeverEvict(t *testing.T) {
	q := NewQueue("s1")
	// Fill up to just under MaxPendingEntries.
	for i := 0; i < MaxPendingEntries-1; i++ {
		if _, err := q.Admit(req(newUUID(t, i), "s1", "x", BoundaryStep), fixedTime()); err != nil {
			t.Fatalf("Admit %d: %v", i, err)
		}
	}
	before := len(q.Pending())
	_, err := q.Admit(req(newUUID(t, MaxPendingEntries+1), "s1", "x", BoundaryStep), fixedTime())
	if err == nil || !strings.Contains(err.Error(), CodeQueueFull) {
		t.Fatalf("over count limit: err = %v, want queue_full", err)
	}
	if len(q.Pending()) != before {
		t.Fatalf("entries were evicted/added: %d -> %d", before, len(q.Pending()))
	}
}

func TestByteLimitRejects(t *testing.T) {
	q := NewQueue("s1")
	big := strings.Repeat("y", MaxPendingBytes-10)
	if _, err := q.Admit(req("11111111-2222-3333-4444-555555555555", "s1", big, BoundaryStep), fixedTime()); err != nil {
		t.Fatal(err)
	}
	// Next small admission would push past the byte budget.
	_, err := q.Admit(req("aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee", "s1", strings.Repeat("z", 11), BoundaryStep), fixedTime())
	if err == nil || !strings.Contains(err.Error(), CodeQueueFull) {
		t.Fatalf("err = %v, want queue_full", err)
	}
}

func TestBoundarySelectionFIFO(t *testing.T) {
	q := NewQueue("s1")
	mk := func(n int, b Boundary) *Entry {
		e, err := q.Admit(req(newUUID(t, n), "s1", fmt.Sprintf("msg %d", n), b), fixedTime())
		if err != nil {
			t.Fatal(err)
		}
		return e
	}
	s1, s2, s3 := mk(0, BoundaryStep), mk(1, BoundaryTurn), mk(2, BoundaryStep)
	t1, t2 := mk(3, BoundaryTurn), mk(4, BoundaryTurn)

	steps := q.SelectEligible(BoundaryStep)
	if len(steps) != 2 || steps[0].RequestID != s1.RequestID || steps[1].RequestID != s3.RequestID {
		t.Fatalf("step selection wrong: %v", ids(steps))
	}
	turns := q.SelectEligible(BoundaryTurn)
	if len(turns) != 3 || turns[0].RequestID != s2.RequestID || turns[1].RequestID != t1.RequestID || turns[2].RequestID != t2.RequestID {
		t.Fatalf("turn selection wrong: %v", ids(turns))
	}

	// Selection does not mutate state.
	if c := q.Counts(); c.Step != 2 || c.Turn != 3 {
		t.Fatalf("counts after selection = %+v", c)
	}
}

func TestStateTransitions(t *testing.T) {
	q := NewQueue("s1")
	e, _ := q.Admit(req("11111111-2222-3333-4444-555555555555", "s1", "steer", BoundaryStep), fixedTime())
	d, _ := q.Admit(req("aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee", "s1", "later", BoundaryTurn), fixedTime())

	// pending → prepared → injected (Prepare driven by a SelectEligible copy,
	// proving the defensive-copy contract works for state transitions)
	stepCopy := q.SelectEligible(BoundaryStep)[0]
	q.Prepare([]*Entry{stepCopy})
	if e.State != StatePrepared {
		t.Fatalf("state = %q, want prepared", e.State)
	}
	q.MarkInjected(e, []string{"msg_1"})
	if e.State != StateInjected || len(e.InjectedMsgIDs) != 1 || e.InjectedMsgIDs[0] != "msg_1" {
		t.Fatalf("injected entry wrong: %+v", e)
	}

	// pending → discarded (Discard driven by a Pending copy)
	pendingCopy := q.Pending()[0]
	q.Discard([]*Entry{pendingCopy})
	if d.State != StateDiscarded {
		t.Fatalf("state = %q, want discarded", d.State)
	}

	// Discarded stays discarded on retry (no re-admission).
	if _, err := q.Admit(req("aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee", "s1", "later", BoundaryTurn), fixedTime()); err != ErrDuplicate {
		t.Fatalf("retry of discarded entry err = %v, want duplicate receipt", err)
	}
	if d.State != StateDiscarded {
		t.Fatalf("discarded entry mutated to %q", d.State)
	}

	// Only non-pending entries remain out of selection.
	if len(q.SelectEligible(BoundaryStep)) != 0 || len(q.SelectEligible(BoundaryTurn)) != 0 {
		t.Fatal("selection should be empty after transitions")
	}
}

func TestLoadRestoresSequencesAndReceipts(t *testing.T) {
	q := NewQueue("s1")
	entries := []Entry{
		{RequestID: "11111111-2222-3333-4444-555555555555", Sequence: 0, SessionID: "s1", Text: "a", Boundary: BoundaryStep, State: StateInjected, InjectedMsgIDs: []string{"msg_1"}},
		{RequestID: "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee", Sequence: 1, SessionID: "s1", Text: "b", Boundary: BoundaryTurn, State: StatePending},
		{RequestID: "bbbbbbbb-cccc-dddd-eeee-ffffffffffff", Sequence: 2, SessionID: "s1", Text: "c", Boundary: BoundaryStep, State: StateDiscarded},
	}
	q.Load(entries)
	if got := q.NextSequence(); got != 3 {
		t.Fatalf("NextSequence = %d, want 3", got)
	}
	if c := q.Counts(); c.Step != 0 || c.Turn != 1 {
		t.Fatalf("counts = %+v, want only pending turn entry", c)
	}
	// Retry of an injected ID returns the existing receipt (idempotent).
	_, err := q.Admit(req("11111111-2222-3333-4444-555555555555", "s1", "a", BoundaryStep), fixedTime())
	if err != ErrDuplicate {
		t.Fatalf("injected retry err = %v, want duplicate", err)
	}
}

func TestReceiptShapes(t *testing.T) {
	q := NewQueue("s1")
	e, _ := q.Admit(req("11111111-2222-3333-4444-555555555555", "s1", "hi", BoundaryTurn), fixedTime())
	r := q.Receipt(e, false, DurabilityDisk)
	if r.Status != StatusAccepted || r.Durability != DurabilityDisk || r.Sequence != 0 || r.Boundary != BoundaryTurn {
		t.Fatalf("receipt = %+v", r)
	}
	if r.Counts.Turn != 1 {
		t.Fatalf("counts = %+v", r.Counts)
	}

	q.MarkInjected(e, []string{"msg_9"})
	rs := q.ReceiptForState(e, DurabilityDisk)
	if rs.InjectedMsgIDs[0] != "msg_9" {
		t.Fatalf("injected receipt = %+v", rs)
	}

	q.Discard([]*Entry{e})
	rd := q.ReceiptForState(e, DurabilityMemory)
	if rd.Status != StatusRejected || rd.Durability != DurabilityMemory {
		t.Fatalf("discarded receipt = %+v", rd)
	}
}

func newUUID(t *testing.T, n int) string {
	t.Helper()
	// 64-bit low half: keeps the UUID unique for n < 2^64.
	return fmt.Sprintf("00000000-0000-0000-0000-%016x", uint64(n))
}

func ids(entries []*Entry) []string {
	out := make([]string, len(entries))
	for i, e := range entries {
		out[i] = e.RequestID
	}
	return out
}
