package app

import (
	"fmt"
	"path/filepath"
	"sync"
	"testing"

	"squid-os/internal/inputqueue"
)

const subSessA = "11111111-2222-3333-4444-555555555555"
const subSessB = "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"

func subReq(id, session, text string, b inputqueue.Boundary) inputqueue.AdmissionRequest {
	return inputqueue.AdmissionRequest{RequestID: id, SessionID: session, Source: "test", Text: text, Boundary: b}
}

// newTestCoordinator builds a disk-backed coordinator whose live session is
// sessA and which is not incognito.
func newTestCoordinator(t *testing.T) *SubmissionCoordinator {
	t.Helper()
	c := NewSubmissionCoordinator(filepath.Join(t.TempDir(), "state"), func() bool { return false })
	if err := c.LoadSession(subSessA); err != nil {
		t.Fatal(err)
	}
	return c
}

func TestAdmitAcceptsAndReportsDiskDurability(t *testing.T) {
	c := newTestCoordinator(t)
	res := c.AdmitSubmission(subReq("00000000-0000-0000-0000-000000000001", subSessA, "hello", inputqueue.BoundaryStep), subSessA)
	if !res.OK || res.Code != "" {
		t.Fatalf("admit = %+v", res)
	}
	if res.Receipt.Status != inputqueue.StatusAccepted || res.Receipt.Durability != inputqueue.DurabilityDisk {
		t.Fatalf("receipt = %+v", res.Receipt)
	}
	if res.Receipt.Sequence != 0 {
		t.Fatalf("sequence = %d, want 0", res.Receipt.Sequence)
	}
	if c.Counts(subSessA).Step != 1 {
		t.Fatalf("counts = %+v", c.Counts(subSessA))
	}
}

func TestAdmitDuplicateIsIdempotent(t *testing.T) {
	c := newTestCoordinator(t)
	r := subReq("00000000-0000-0000-0000-000000000001", subSessA, "same", inputqueue.BoundaryTurn)
	first := c.AdmitSubmission(r, subSessA)
	if !first.OK {
		t.Fatal(first)
	}
	second := c.AdmitSubmission(r, subSessA)
	if !second.OK || !second.Receipt.Duplicate {
		t.Fatalf("second = %+v", second)
	}
	if first.Receipt.Sequence != second.Receipt.Sequence {
		t.Fatalf("sequences differ: %d vs %d", first.Receipt.Sequence, second.Receipt.Sequence)
	}
	if n := len(c.Pending(subSessA)); n != 1 {
		t.Fatalf("pending = %d, want 1", n)
	}
}

func TestAdmitSameIDDifferentPayloadConflicts(t *testing.T) {
	c := newTestCoordinator(t)
	id := "00000000-0000-0000-0000-000000000001"
	if res := c.AdmitSubmission(subReq(id, subSessA, "original", inputqueue.BoundaryStep), subSessA); !res.OK {
		t.Fatal(res)
	}
	for _, mut := range []struct {
		name string
		r    inputqueue.AdmissionRequest
	}{
		{"different text", subReq(id, subSessA, "changed", inputqueue.BoundaryStep)},
		{"different boundary", subReq(id, subSessA, "original", inputqueue.BoundaryTurn)},
	} {
		t.Run(mut.name, func(t *testing.T) {
			res := c.AdmitSubmission(mut.r, subSessA)
			if res.OK || res.Code != inputqueue.CodeIDConflict {
				t.Fatalf("res = %+v, want id_conflict", res)
			}
		})
	}
}

func TestIdenticalTextDifferentIDsAreDistinct(t *testing.T) {
	c := newTestCoordinator(t)
	a := c.AdmitSubmission(subReq("00000000-0000-0000-0000-000000000001", subSessA, "repeat", inputqueue.BoundaryStep), subSessA)
	b := c.AdmitSubmission(subReq("00000000-0000-0000-0000-000000000002", subSessA, "repeat", inputqueue.BoundaryStep), subSessA)
	if !a.OK || !b.OK {
		t.Fatalf("a=%+v b=%+v", a, b)
	}
	if a.Receipt.Sequence == b.Receipt.Sequence {
		t.Fatalf("both share sequence %d", a.Receipt.Sequence)
	}
	if c.Counts(subSessA).Step != 2 {
		t.Fatalf("counts = %+v", c.Counts(subSessA))
	}
}

func TestWrongSessionRejectedNotRedirected(t *testing.T) {
	c := newTestCoordinator(t) // live session is subSessA
	res := c.AdmitSubmission(subReq("00000000-0000-0000-0000-000000000001", subSessB, "stale", inputqueue.BoundaryStep), subSessA)
	if res.OK || res.Code != inputqueue.CodeWrongSession {
		t.Fatalf("res = %+v, want wrong_session", res)
	}
	// Nothing admitted into either session.
	if n := len(c.Pending(subSessB)); n != 0 {
		t.Fatalf("foreign session got entries: %d", n)
	}
}

func TestIncognitoReportsMemoryDurability(t *testing.T) {
	c := NewMemSubmissionCoordinator(func() bool { return true })
	res := c.AdmitSubmission(subReq("00000000-0000-0000-0000-000000000001", subSessA, "voice", inputqueue.BoundaryStep), subSessA)
	if !res.OK {
		t.Fatal(res)
	}
	if res.Receipt.Durability != inputqueue.DurabilityMemory {
		t.Fatalf("durability = %q, want memory", res.Receipt.Durability)
	}
}

func TestValidationRejections(t *testing.T) {
	c := newTestCoordinator(t)
	cases := []struct {
		name string
		r    inputqueue.AdmissionRequest
		want string
	}{
		{"empty id", subReq("", subSessA, "hi", inputqueue.BoundaryStep), inputqueue.CodeInvalid},
		{"non-uuid id", subReq("not-a-uuid", subSessA, "hi", inputqueue.BoundaryStep), inputqueue.CodeInvalid},
		{"blank text", subReq("00000000-0000-0000-0000-000000000001", subSessA, "   ", inputqueue.BoundaryStep), inputqueue.CodeEmpty},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res := c.AdmitSubmission(tc.r, subSessA)
			if res.OK || res.Code != tc.want {
				t.Fatalf("res = %+v, want %s", res, tc.want)
			}
		})
	}
}

func TestDiscardFlushesPending(t *testing.T) {
	c := newTestCoordinator(t)
	if res := c.AdmitSubmission(subReq("00000000-0000-0000-0000-000000000001", subSessA, "a", inputqueue.BoundaryStep), subSessA); !res.OK {
		t.Fatal(res)
	}
	if res := c.AdmitSubmission(subReq("00000000-0000-0000-0000-000000000002", subSessA, "b", inputqueue.BoundaryTurn), subSessA); !res.OK {
		t.Fatal(res)
	}
	n := c.Discard(subSessA)
	if n != 2 {
		t.Fatalf("discarded = %d, want 2", n)
	}
	if n := len(c.Pending(subSessA)); n != 0 {
		t.Fatalf("pending after discard = %d", n)
	}
	// A discarded request stays discarded on retry (idempotent duplicate, not re-admitted).
	res := c.AdmitSubmission(subReq("00000000-0000-0000-0000-000000000001", subSessA, "a", inputqueue.BoundaryStep), subSessA)
	if !res.OK || !res.Receipt.Duplicate {
		t.Fatalf("retry of discarded = %+v, want duplicate receipt", res)
	}
}

func TestCrashRecoveryReconcilesPreparedAgainstCheckpoint(t *testing.T) {
	root := t.TempDir()
	mk := func() *SubmissionCoordinator {
		c := NewSubmissionCoordinator(root, func() bool { return false })
		if err := c.LoadSession(subSessA); err != nil {
			t.Fatal(err)
		}
		return c
	}
	store := inputqueue.NewStore(filepath.Join(root, "input-queue"))
	const reqID = "00000000-0000-0000-0000-000000000001"

	// Window 1: crash before append — nothing durable yet. Recovery of an
	// empty journal must not fabricate entries; the request stays unadmitted
	// (client keeps its draft/outbox).
	c1 := mk()
	if got, _ := store.Load(subSessA); len(got) != 0 {
		t.Fatalf("before append: %+v", got)
	}
	if err := c1.ReconcileInjected(subSessA, map[string]bool{}); err != nil {
		t.Fatal(err)
	}
	if _, ok := c1.Find(subSessA, reqID); ok {
		t.Fatal("entry must not exist before append")
	}

	// Admit durably, then crash before prepared-state update.
	if res := c1.AdmitSubmission(subReq(reqID, subSessA, "steer", inputqueue.BoundaryStep), subSessA); !res.OK {
		t.Fatal(res)
	}

	// Window 2: after append/checkpoint with no injected message IDs in the
	// checkpoint → entry is still pending. Recover paused, never auto-run.
	c2 := mk()
	if err := c2.ReconcileInjected(subSessA, map[string]bool{}); err != nil {
		t.Fatal(err)
	}
	e, ok := c2.Find(subSessA, reqID)
	if !ok {
		t.Fatal("entry missing after recovery")
	}
	if e.State != inputqueue.StatePending {
		t.Fatalf("recovered state = %q, want pending", e.State)
	}
	// A second recovered coordinator sees the same durable pending state.
	c2b := mk()
	if e2, _ := c2b.Find(subSessA, reqID); e2.State != inputqueue.StatePending {
		t.Fatalf("second recovery state = %q, want pending", e2.State)
	}

	// Window 3: mark prepared + stable message ID durably, checkpoint written,
	// then crash before the injected receipt update.
	if err := store.UpdateState(subSessA, func(entries []*inputqueue.Entry) {
		for i := range entries {
			entries[i].State = inputqueue.StatePrepared
			entries[i].InjectedMsgIDs = []string{"msg_1"}
		}
	}); err != nil {
		t.Fatal(err)
	}

	// 3a: prepared ID present in the persisted session checkpoint ⇒ already
	// injected. Reconciliation marks it injected durably.
	c3 := mk()
	if err := c3.ReconcileInjected(subSessA, map[string]bool{"msg_1": true}); err != nil {
		t.Fatal(err)
	}
	e, _ = c3.Find(subSessA, reqID)
	if e.State != inputqueue.StateInjected || len(e.InjectedMsgIDs) != 1 || e.InjectedMsgIDs[0] != "msg_1" {
		t.Fatalf("checkpoint-present recovery = %+v, want injected msg_1", e)
	}
	got, err := store.Load(subSessA)
	if err != nil {
		t.Fatal(err)
	}
	if got[0].State != inputqueue.StateInjected {
		t.Fatalf("journal after reconciliation = %q, want injected", got[0].State)
	}

	// Idempotent: re-running reconciliation does not double-inject.
	if err := c3.ReconcileInjected(subSessA, map[string]bool{"msg_1": true}); err != nil {
		t.Fatal(err)
	}
	if e, _ = c3.Find(subSessA, reqID); len(e.InjectedMsgIDs) != 1 {
		t.Fatalf("double injection: %+v", e)
	}

	// 3b: reset to the pre-crash prepared state; prepared ID ABSENT from the
	// checkpoint ⇒ never appended ⇒ revert to pending (still paused).
	if err := store.UpdateState(subSessA, func(entries []*inputqueue.Entry) {
		for i := range entries {
			entries[i].State = inputqueue.StatePrepared
		}
	}); err != nil {
		t.Fatal(err)
	}
	c4 := mk()
	if err := c4.ReconcileInjected(subSessA, map[string]bool{}); err != nil {
		t.Fatal(err)
	}
	e, _ = c4.Find(subSessA, reqID)
	if e.State != inputqueue.StatePending {
		t.Fatalf("checkpoint-absent recovery = %q, want pending", e.State)
	}
	got, _ = store.Load(subSessA)
	if got[0].State != inputqueue.StatePending {
		t.Fatalf("journal after absent-reconciliation = %q, want pending", got[0].State)
	}
}

func TestConcurrentAdmissionsSerializeWithoutRaces(t *testing.T) {
	c := newTestCoordinator(t)
	defer c.Close()
	const n = 64
	var wg sync.WaitGroup
	errs := make(chan error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			id := fmt.Sprintf("00000000-0000-0000-0000-%012x", uint64(i))
			res := c.AdmitSubmission(subReq(id, subSessA, fmt.Sprintf("msg %d", i), inputqueue.BoundaryStep), subSessA)
			if !res.OK {
				errs <- fmt.Errorf("admit %d rejected: %+v", i, res)
				return
			}
			// Duplicate retry from the same goroutine must be idempotent.
			res2 := c.AdmitSubmission(subReq(id, subSessA, fmt.Sprintf("msg %d", i), inputqueue.BoundaryStep), subSessA)
			if !res2.OK || !res2.Receipt.Duplicate {
				errs <- fmt.Errorf("duplicate %d: %+v", i, res2)
			}
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
	if c.Counts(subSessA).Step != n {
		t.Fatalf("counts = %+v, want step=%d", c.Counts(subSessA), n)
	}
	// Sequences must be exactly 0..n-1 with no gaps or duplicates: proof the
	// worker serialized admissions.
	seen := make(map[int64]bool, n)
	for _, e := range c.Pending(subSessA) {
		if seen[e.Sequence] {
			t.Fatalf("duplicate sequence %d", e.Sequence)
		}
		seen[e.Sequence] = true
	}
	for seq := int64(0); seq < n; seq++ {
		if !seen[seq] {
			t.Fatalf("missing sequence %d", seq)
		}
	}
}
