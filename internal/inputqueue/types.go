// Package inputqueue is the transport-independent domain for queued user
// input: entries, FIFO selection, stable request IDs, dedup receipts,
// safety limits and state transitions.
//
// It must never import GTK, X11, Bubble Tea or provider implementations.
// The application coordinator (internal/app/submission.go) owns admission
// serialization; both keyboard and socket transports call it.
package inputqueue

import (
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Documented safety limits. Limits are enforced with explicit rejection,
// never eviction: an over-limit submission is refused and the client keeps
// its draft/outbox.
const (
	// MaxRequestBytes bounds a single submitted text payload.
	MaxRequestBytes = 256 * 1024 // 256 KiB per request
	// MaxPendingBytes bounds the total pending (not yet injected/discarded)
	// text for one session.
	MaxPendingBytes = 16 * 1024 * 1024 // 16 MiB pending per session
	// MaxPendingEntries bounds the number of pending entries per session.
	MaxPendingEntries = 4096
)

// Boundary selects when an entry becomes eligible for injection.
type Boundary string

const (
	// BoundaryStep drains at tool-batch-done / terminal-with-step-pending.
	BoundaryStep Boundary = "step"
	// BoundaryTurn drains only at logical-turn end.
	BoundaryTurn Boundary = "turn"
)

// ParseBoundary validates a wire-level boundary value.
func ParseBoundary(s string) (Boundary, error) {
	switch s {
	case string(BoundaryStep):
		return BoundaryStep, nil
	case string(BoundaryTurn):
		return BoundaryTurn, nil
	default:
		return "", fmt.Errorf("unknown queue boundary %q", s)
	}
}

// State is the lifecycle state of a queue entry. Paused is scheduler state
// owned by the coordinator, not an entry state.
type State string

const (
	StatePending   State = "pending"
	StatePrepared  State = "prepared-for-injection"
	StateInjected  State = "injected"
	StateDiscarded State = "discarded"
)

// Entry is one admitted (or still-admitting) queued message.
type Entry struct {
	RequestID      string    `json:"request_id"`
	Sequence       int64     `json:"sequence"` // server-assigned, monotonic per session
	SessionID      string    `json:"session_id"`
	Source         string    `json:"source"`
	Text           string    `json:"text"`
	Boundary       Boundary  `json:"when"`
	AdmittedAt     time.Time `json:"admitted_at"`
	State          State     `json:"state"`
	InjectedMsgIDs []string  `json:"injected_message_ids,omitempty"`
}

// Receipt is the acknowledgement returned to a submitter after durable
// admission (or describing an existing/duplicate outcome).
type Receipt struct {
	RequestID  string     `json:"request_id"`
	Status     Status     `json:"status"`
	Durability Durability `json:"durability"`
	SessionID  string     `json:"session_id"`
	Boundary   Boundary   `json:"when"`
	Sequence   int64      `json:"sequence"`
	Counts     Counts     `json:"queue_counts"`
	// InjectedMsgIDs is populated once the entry has been injected.
	InjectedMsgIDs []string `json:"injected_message_ids,omitempty"`
	// Duplicate is true when this receipt was returned for an already-known
	// request ID with an identical payload (idempotent retry).
	Duplicate bool `json:"duplicate,omitempty"`
}

// Status classifies a receipt outcome.
type Status string

const (
	StatusAccepted  Status = "accepted"
	StatusRejected  Status = "rejected"
	StatusPausedErr Status = "paused-error"
)

// Durability reports where the admission record lives.
type Durability string

const (
	DurabilityDisk   Durability = "disk"
	DurabilityMemory Durability = "memory"
)

// Counts holds pending-entry counts per boundary for display/receipts.
type Counts struct {
	Step int `json:"step"`
	Turn int `json:"turn"`
}

// Error codes for structured rejections.
const (
	CodeInvalid      = "invalid_request"
	CodeEmpty        = "empty_text"
	CodeOversize     = "oversize_request"
	CodeQueueFull    = "queue_full"
	CodeIDConflict   = "id_conflict"
	CodeWrongSession = "wrong_session"
	CodeStoreError   = "store_error"
)

// AdmissionRequest is a validated-or-raw submission from any transport.
type AdmissionRequest struct {
	RequestID string
	SessionID string
	Source    string
	Text      string
	Boundary  Boundary
}

// Validate performs pure validation of an admission request. It returns a
// structured error carrying one of the Code* codes (see AsCode) on failure.
func (r AdmissionRequest) Validate() error {
	if r.RequestID == "" {
		return newCodeErr(CodeInvalid, "%s: request id is required", CodeInvalid)
	}
	if !isValidUUID(r.RequestID) {
		return newCodeErr(CodeInvalid, "%s: request id %q is not a valid UUID", CodeInvalid, r.RequestID)
	}
	if r.SessionID == "" {
		return newCodeErr(CodeInvalid, "%s: session id is required", CodeInvalid)
	}
	if _, err := ParseBoundary(string(r.Boundary)); err != nil {
		return newCodeErr(CodeInvalid, "%s: %v", CodeInvalid, err)
	}
	if len(r.Text) > MaxRequestBytes {
		return newCodeErr(CodeOversize, "%s: text is %d bytes, limit is %d", CodeOversize, len(r.Text), MaxRequestBytes)
	}
	if strings.TrimSpace(r.Text) == "" {
		return newCodeErr(CodeEmpty, "%s: text is empty", CodeEmpty)
	}
	return nil
}

// NewRequestID mints a stable random request ID for submissions that do not
// carry one (keyboard submissions get IDs too).
func NewRequestID() string { return uuid.NewString() }

func isValidUUID(s string) bool {
	_, err := uuid.Parse(s)
	return err == nil
}
