package inputqueue

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// ErrInvalidSessionID is returned when a session identity is not a valid UUID.
// Journal stores must never guess file names for unknown identities.
var ErrInvalidSessionID = errors.New("invalid_session_id")

// AsCode reports the structured rejection code carried by err, or "" if the
// error carries none. Classification uses this instead of string matching on
// error text so codes cannot misfire.
func AsCode(err error) string {
	var ce *codeError
	if errors.As(err, &ce) {
		return ce.code
	}
	return ""
}

// codeError is a structured error carrying one of the Code* rejection codes.
type codeError struct {
	code string
	msg  string
}

func (e *codeError) Error() string { return e.msg }

func newCodeErr(code, format string, args ...any) error {
	return &codeError{code: code, msg: fmt.Sprintf(format, args...)}
}

// journalFile is the per-session queue journal file name.
const journalFile = "input-queue.json"

// journalDoc is the on-disk shape of a session's queue state. It is written
// with atomic replace + fsync and is durable independently of chat autosave.
type journalDoc struct {
	Version   int     `json:"version"`
	SessionID string  `json:"session_id"`
	NextSeq   int64   `json:"next_seq"`
	Entries   []Entry `json:"entries"`
}

const journalVersion = 1

// Store persists queue journals under a configured state area, one file per
// immutable session identity. Directories are created 0700, files 0600.
type Store struct {
	dir string // <stateArea>/input-queue/
	// failDirSync forces directory fsync to fail (tests only).
	failDirSync bool
}

// NewStore opens a store rooted at dir (the caller passes e.g.
// filepath.Join(paths.Root, "input-queue")).
func NewStore(dir string) *Store { return &Store{dir: dir} }

// Path returns the journal path for a session ID.
func (s *Store) Path(sessionID string) string {
	return filepath.Join(s.dir, safeSessionName(sessionID)+".json")
}

// Admit durably appends an entry to the journal and returns it. The entry's
// Sequence must already be assigned by the Queue. A disk error is returned
// as-is so callers can reject or mark paused-error — never false accepted.
func (s *Store) Admit(e *Entry) error {
	if err := checkSessionID(e.SessionID); err != nil {
		return err
	}
	doc, err := s.load(e.SessionID)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return newCodeErr(CodeStoreError, "%s: load journal: %v", CodeStoreError, err)
	}
	if doc == nil {
		doc = &journalDoc{Version: journalVersion, SessionID: e.SessionID}
	}
	if doc.SessionID != e.SessionID {
		return newCodeErr(CodeWrongSession, "%s: journal %q belongs to session %q", CodeWrongSession, s.Path(e.SessionID), doc.SessionID)
	}
	for _, existing := range doc.Entries {
		if existing.RequestID == e.RequestID {
			if payloadEqual(&existing, AdmissionRequest{
				RequestID: e.RequestID,
				SessionID: e.SessionID,
				Text:      e.Text,
				Boundary:  e.Boundary,
			}) {
				return ErrDuplicate
			}
			return newCodeErr(CodeIDConflict, "%s: request id %q already exists with a different payload", CodeIDConflict, e.RequestID)
		}
	}
	doc.Entries = append(doc.Entries, *e)
	if e.Sequence >= doc.NextSeq {
		doc.NextSeq = e.Sequence + 1
	}
	return s.write(doc)
}

// UpdateState rewrites the journal after a state transition (prepared →
// injected / discarded). Used by the injection transaction.
func (s *Store) UpdateState(sessionID string, mutate func(entries []*Entry)) error {
	if err := checkSessionID(sessionID); err != nil {
		return err
	}
	doc, err := s.load(sessionID)
	if err != nil {
		return newCodeErr(CodeStoreError, "%s: load journal: %v", CodeStoreError, err)
	}
	entries := make([]*Entry, len(doc.Entries))
	for i := range doc.Entries {
		entries[i] = &doc.Entries[i]
	}
	mutate(entries)
	for i := range entries {
		doc.Entries[i] = *entries[i]
	}
	return s.write(doc)
}

// Load reads the persisted entries for a session in admission order.
// Returns nil entries (not an error) when no journal exists yet.
func (s *Store) Load(sessionID string) ([]Entry, error) {
	if err := checkSessionID(sessionID); err != nil {
		return nil, err
	}
	doc, err := s.load(sessionID)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, newCodeErr(CodeStoreError, "%s: load journal: %v", CodeStoreError, err)
	}
	out := make([]Entry, len(doc.Entries))
	copy(out, doc.Entries)
	return out, nil
}

// Remove deletes the journal for a session (explicit cleanup only; v1 does
// not auto-prune receipts within session lifetime).
func (s *Store) Remove(sessionID string) error {
	if err := checkSessionID(sessionID); err != nil {
		return err
	}
	err := os.Remove(s.Path(sessionID))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

func (s *Store) load(sessionID string) (*journalDoc, error) {
	data, err := os.ReadFile(s.Path(sessionID))
	if err != nil {
		return nil, err
	}
	var doc journalDoc
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("decode journal: %w", err)
	}
	return &doc, nil
}

// write performs an atomic replace: write temp file in the same directory,
// chmod 0600, fsync file, rename over target, fsync directory. A failed
// directory fsync is returned as an error: the rename may not be durable yet,
// so the operation must NOT report success.
func (s *Store) write(doc *journalDoc) error {
	if err := os.MkdirAll(s.dir, 0700); err != nil {
		return newCodeErr(CodeStoreError, "%s: create state dir: %v", CodeStoreError, err)
	}
	data, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return newCodeErr(CodeStoreError, "%s: encode journal: %v", CodeStoreError, err)
	}
	tmp, err := os.CreateTemp(s.dir, "."+safeSessionName(doc.SessionID)+".tmp")
	if err != nil {
		return newCodeErr(CodeStoreError, "%s: create temp journal: %v", CodeStoreError, err)
	}
	name := tmp.Name()
	defer os.Remove(name)

	fail := func(err error) error {
		tmp.Close()
		return newCodeErr(CodeStoreError, "%s: %v", CodeStoreError, err)
	}
	if _, err := tmp.Write(data); err != nil {
		return fail(err)
	}
	if err := tmp.Chmod(0600); err != nil {
		return fail(err)
	}
	if err := tmp.Sync(); err != nil {
		return fail(err)
	}
	if err := tmp.Close(); err != nil {
		return fail(err)
	}
	if err := os.Rename(name, s.Path(doc.SessionID)); err != nil {
		return fail(err)
	}
	// Fsync the directory so the rename itself is durable. Failure here means
	// the rename is not guaranteed durable — propagate it, never swallow it.
	d, err := os.Open(s.dir)
	if err != nil {
		return newCodeErr(CodeStoreError, "%s: open state dir for sync: %v", CodeStoreError, err)
	}
	defer d.Close()
	var syncErr error
	if s.failDirSync {
		syncErr = errors.New("injected directory fsync failure")
	} else {
		syncErr = d.Sync()
	}
	if syncErr != nil {
		return newCodeErr(CodeStoreError, "%s: sync state dir after rename: %v", CodeStoreError, syncErr)
	}
	return nil
}

// checkSessionID rejects session identities that are not valid UUIDs with a
// structured error instead of silently storing them under a guessed name.
func checkSessionID(id string) error {
	if isValidUUID(id) {
		return nil
	}
	return fmt.Errorf("%w: %q", ErrInvalidSessionID, id)
}

// safeSessionName maps a validated (UUID) session ID into its file name
// component. Callers must validate via checkSessionID first; this only
// guards the path join.
func safeSessionName(id string) string {
	if isValidUUID(id) {
		return id
	}
	return "unknown-" + id
}

// --- Memory-only store (incognito) ----------------------------------------

// MemStore keeps queues and receipts in process memory only. Incognito
// sessions use it; receipts report durability "memory" and nothing is ever
// written to disk.
type MemStore struct {
	journals map[string]*journalDoc
}

// NewMemStore creates an empty memory-only store.
func NewMemStore() *MemStore {
	return &MemStore{journals: make(map[string]*journalDoc)}
}

// Admit records an entry in memory.
func (m *MemStore) Admit(e *Entry) error {
	doc, ok := m.journals[e.SessionID]
	if !ok {
		doc = &journalDoc{Version: journalVersion, SessionID: e.SessionID}
		m.journals[e.SessionID] = doc
	}
	for _, existing := range doc.Entries {
		if existing.RequestID == e.RequestID {
			if payloadEqual(&existing, AdmissionRequest{
				RequestID: e.RequestID,
				SessionID: e.SessionID,
				Text:      e.Text,
				Boundary:  e.Boundary,
			}) {
				return ErrDuplicate
			}
			return fmt.Errorf("%s: request id %q already exists with a different payload", CodeIDConflict, e.RequestID)
		}
	}
	doc.Entries = append(doc.Entries, *e)
	if e.Sequence >= doc.NextSeq {
		doc.NextSeq = e.Sequence + 1
	}
	return nil
}

// UpdateState mutates entry states in memory.
func (m *MemStore) UpdateState(sessionID string, mutate func(entries []*Entry)) error {
	doc := m.journals[sessionID]
	if doc == nil {
		return nil
	}
	entries := make([]*Entry, len(doc.Entries))
	for i := range doc.Entries {
		entries[i] = &doc.Entries[i]
	}
	mutate(entries)
	for i := range entries {
		doc.Entries[i] = *entries[i]
	}
	return nil
}

// Load returns the in-memory entries for a session.
func (m *MemStore) Load(sessionID string) ([]Entry, error) {
	doc := m.journals[sessionID]
	if doc == nil {
		return nil, nil
	}
	out := make([]Entry, len(doc.Entries))
	copy(out, doc.Entries)
	return out, nil
}

// Remove drops the in-memory journal.
func (m *MemStore) Remove(sessionID string) error {
	delete(m.journals, sessionID)
	return nil
}

// Journal is the interface both Store and MemStore implement. Callers treat
// admission as durable only when the underlying store is a disk Store;
// receipts carry the Durability value chosen by the coordinator.
type Journal interface {
	Admit(e *Entry) error
	UpdateState(sessionID string, mutate func(entries []*Entry)) error
	Load(sessionID string) ([]Entry, error)
	Remove(sessionID string) error
}

var _ Journal = (*Store)(nil)
var _ Journal = (*MemStore)(nil)

// Now is the clock used for admission timestamps; tests may override it.
var Now = time.Now
