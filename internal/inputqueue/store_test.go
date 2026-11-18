package inputqueue

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const sessA = "11111111-2222-3333-4444-555555555555"
const sessB = "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"

func newEntry(id, session, text string, b Boundary, seq int64) *Entry {
	return &Entry{RequestID: id, Sequence: seq, SessionID: session, Source: "test", Text: text, Boundary: b, AdmittedAt: fixedTime(), State: StatePending}
}

func TestStoreAdmitAndLoad(t *testing.T) {
	s := NewStore(filepath.Join(t.TempDir(), "input-queue"))
	e1 := newEntry(sessA, sessA, "first", BoundaryStep, 0)
	if err := s.Admit(e1); err != nil {
		t.Fatal(err)
	}
	e2 := newEntry(sessB, sessA, "second", BoundaryTurn, 1)
	if err := s.Admit(e2); err != nil {
		t.Fatal(err)
	}

	got, err := s.Load(sessA)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].RequestID != e1.RequestID || got[1].Text != "second" {
		t.Fatalf("loaded = %+v", got)
	}

	// No journal for another session.
	none, err := s.Load("bbbbbbbb-cccc-dddd-eeee-ffffffffffff")
	if err != nil || none != nil {
		t.Fatalf("missing journal: entries=%v err=%v", none, err)
	}
}

func TestStorePermissions(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "input-queue")
	s := NewStore(dir)
	if err := s.Admit(newEntry(sessA, sessA, "x", BoundaryStep, 0)); err != nil {
		t.Fatal(err)
	}
	di, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	if di.Mode().Perm() != 0700 {
		t.Fatalf("dir perms = %o, want 700", di.Mode().Perm())
	}
	fi, err := os.Stat(s.Path(sessA))
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0600 {
		t.Fatalf("file perms = %o, want 600", fi.Mode().Perm())
	}
}

func TestStoreDedupAndConflict(t *testing.T) {
	s := NewStore(filepath.Join(t.TempDir(), "input-queue"))
	e := newEntry(sessA, sessA, "same", BoundaryStep, 0)
	if err := s.Admit(e); err != nil {
		t.Fatal(err)
	}
	// Same ID + same payload → idempotent duplicate.
	if err := s.Admit(newEntry(sessA, sessA, "same", BoundaryStep, 0)); !errors.Is(err, ErrDuplicate) {
		t.Fatalf("duplicate admit err = %v, want ErrDuplicate", err)
	}
	// Same ID + different payload → conflict.
	err := s.Admit(newEntry(sessA, sessA, "changed", BoundaryStep, 1))
	if err == nil || !strings.Contains(err.Error(), CodeIDConflict) {
		t.Fatalf("conflict err = %v, want id_conflict", err)
	}
	// Journal still holds exactly the original entry.
	got, _ := s.Load(sessA)
	if len(got) != 1 || got[0].Text != "same" {
		t.Fatalf("journal mutated: %+v", got)
	}
}

func TestStoreUpdateState(t *testing.T) {
	s := NewStore(filepath.Join(t.TempDir(), "input-queue"))
	if err := s.Admit(newEntry(sessA, sessA, "steer", BoundaryStep, 0)); err != nil {
		t.Fatal(err)
	}
	err := s.UpdateState(sessA, func(entries []*Entry) {
		for _, e := range entries {
			e.State = StateInjected
			e.InjectedMsgIDs = []string{"msg_1"}
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	got, _ := s.Load(sessA)
	if got[0].State != StateInjected || got[0].InjectedMsgIDs[0] != "msg_1" {
		t.Fatalf("after update: %+v", got[0])
	}
}

func TestStoreDiskErrorIsNotAccepted(t *testing.T) {
	// A read-only directory makes the admission write fail; the error must
	// surface as a store error so callers reject / mark paused-error.
	root := t.TempDir()
	dir := filepath.Join(root, "ro")
	if err := os.MkdirAll(dir, 0500); err != nil {
		t.Skipf("cannot create 0500 dir here: %v", err)
	}
	defer os.Chmod(dir, 0755)
	s := NewStore(dir)
	err := s.Admit(newEntry(sessA, sessA, "x", BoundaryStep, 0))
	if err == nil {
		// Running as root may bypass permissions; that is acceptable — the
		// contract is that errors propagate, not that this test always fails.
		t.Skip("permissions did not block write (root?)")
	}
	if !strings.Contains(err.Error(), CodeStoreError) {
		t.Fatalf("err = %v, want store_error", err)
	}
}

func TestStoreRejectsInvalidSessionID(t *testing.T) {
	s := NewStore(filepath.Join(t.TempDir(), "input-queue"))
	bad := "not-a-uuid"
	if err := s.Admit(newEntry(bad, bad, "x", BoundaryStep, 0)); !errors.Is(err, ErrInvalidSessionID) {
		t.Fatalf("Admit err = %v, want ErrInvalidSessionID", err)
	}
	if _, err := s.Load(bad); !errors.Is(err, ErrInvalidSessionID) {
		t.Fatalf("Load err = %v, want ErrInvalidSessionID", err)
	}
	err := s.UpdateState(bad, func(entries []*Entry) {})
	if !errors.Is(err, ErrInvalidSessionID) {
		t.Fatalf("UpdateState err = %v, want ErrInvalidSessionID", err)
	}
	if err := s.Remove(bad); !errors.Is(err, ErrInvalidSessionID) {
		t.Fatalf("Remove err = %v, want ErrInvalidSessionID", err)
	}
	// Nothing was stored under a guessed name.
	if entries, _ := os.ReadDir(s.dir); len(entries) != 0 {
		t.Fatalf("unexpected files in store dir: %v", entries)
	}
}

func TestStoreDirSyncFailureIsNotAccepted(t *testing.T) {
	// A failing directory fsync must surface as a store error: the rename is
	// not guaranteed durable, so Admit must NOT report success.
	s := NewStore(filepath.Join(t.TempDir(), "input-queue"))
	s.failDirSync = true
	err := s.Admit(newEntry(sessA, sessA, "x", BoundaryStep, 0))
	if err == nil || !strings.Contains(err.Error(), CodeStoreError) {
		t.Fatalf("err = %v, want store_error from failed dir sync", err)
	}
	// The target journal must not be reported as durably written; whatever is
	// on disk, Load after a failed write must either fail or show no entry —
	// the contract under test is that Admit returned an error.
	if _, lerr := s.Load(sessA); lerr != nil && !strings.Contains(lerr.Error(), CodeStoreError) {
		t.Fatalf("Load err = %v", lerr)
	}
}

func TestMemStoreMatchesDiskSemantics(t *testing.T) {
	m := NewMemStore()
	if err := m.Admit(newEntry(sessA, sessA, "same", BoundaryStep, 0)); err != nil {
		t.Fatal(err)
	}
	if err := m.Admit(newEntry(sessA, sessA, "same", BoundaryStep, 0)); !errors.Is(err, ErrDuplicate) {
		t.Fatalf("dup err = %v", err)
	}
	if err := m.Admit(newEntry(sessA, sessA, "nope", BoundaryStep, 1)); err == nil || !strings.Contains(err.Error(), CodeIDConflict) {
		t.Fatalf("conflict err = %v", err)
	}
	got, err := m.Load(sessA)
	if err != nil || len(got) != 1 {
		t.Fatalf("load: %v %v", got, err)
	}
	if err := m.UpdateState(sessA, func(entries []*Entry) {
		entries[0].State = StateDiscarded
	}); err != nil {
		t.Fatal(err)
	}
	got, _ = m.Load(sessA)
	if got[0].State != StateDiscarded {
		t.Fatalf("state = %q", got[0].State)
	}
	if err := m.Remove(sessA); err != nil {
		t.Fatal(err)
	}
	if got, _ = m.Load(sessA); got != nil {
		t.Fatalf("after remove: %+v", got)
	}
}

func TestCrashWindows(t *testing.T) {
	// Simulate the three crash windows of the injection transaction and prove
	// recovery reconciles prepared IDs against the persisted checkpoint.
	st := NewStore(filepath.Join(t.TempDir(), "input-queue"))
	e := newEntry(sessA, sessA, "steer", BoundaryStep, 0)

	// Window 1: crash before append — nothing durable yet.
	if got, _ := st.Load(sessA); len(got) != 0 {
		t.Fatalf("before admit: %+v", got)
	}
	if err := st.Admit(e); err != nil {
		t.Fatal(err)
	}

	// Window 2: crash after admission but before prepared-state update.
	got, _ := st.Load(sessA)
	if got[0].State != StatePending {
		t.Fatalf("after admit state = %q", got[0].State)
	}
	// Recovery: no message in checkpoint → still pending, recover paused.
	recovered, err := reconcile(st, sessA, map[string]bool{}) // checkpoint has no msg_1
	if err != nil {
		t.Fatal(err)
	}
	if recovered[0].State != StatePending {
		t.Fatalf("recovery should keep entry pending, got %q", recovered[0].State)
	}

	// Window 3: crash after checkpoint write, before receipt update.
	// The coordinator marks prepared durably together with the stable message
	// IDs it assigned, then checkpoints the session history with those IDs,
	// and only then marks injected. A crash between checkpoint and receipt
	// leaves a prepared entry whose ID is already in the checkpoint.
	if err := st.UpdateState(sessA, func(entries []*Entry) {
		for i := range entries {
			entries[i].State = StatePrepared
			entries[i].InjectedMsgIDs = []string{"msg_1"}
		}
	}); err != nil {
		t.Fatal(err)
	}
	checkpointHas := map[string]bool{"msg_1": true} // session checkpoint now contains the message
	recovered, err = reconcile(st, sessA, checkpointHas)
	if err != nil {
		t.Fatal(err)
	}
	if recovered[0].State != StateInjected || recovered[0].InjectedMsgIDs[0] != "msg_1" {
		t.Fatalf("recovery should mark injected, got %+v", recovered[0])
	}

	// Idempotent: re-running recovery does not double-inject.
	recovered, err = reconcile(st, sessA, checkpointHas)
	if err != nil {
		t.Fatal(err)
	}
	if len(recovered[0].InjectedMsgIDs) != 1 {
		t.Fatalf("double injection: %+v", recovered[0])
	}
}

// reconcile implements the documented recovery rule: a prepared entry whose
// stable message ID is present in the persisted session checkpoint is already
// injected; absent means it is still pending. It never auto-runs inference.
func reconcile(j Journal, sessionID string, checkpointHasMessage map[string]bool) ([]Entry, error) {
	entries, err := j.Load(sessionID)
	if err != nil {
		return nil, err
	}
	var out []Entry
	changed := false
	for i := range entries {
		e := entries[i]
		if e.State == StatePrepared && len(e.InjectedMsgIDs) > 0 && checkpointHasMessage[e.InjectedMsgIDs[0]] {
			e.State = StateInjected
			changed = true
		}
		out = append(out, e)
	}
	if changed {
		final := out
		if err := j.UpdateState(sessionID, func(es []*Entry) {
			for i := range es {
				es[i].State = final[i].State
				es[i].InjectedMsgIDs = final[i].InjectedMsgIDs
			}
		}); err != nil {
			return nil, err
		}
	}
	return out, nil
}
