// Package localinput implements the private local input gateway (design §5):
// a Unix-domain stream socket that bridges validated submissions into the TUI's
// shared admission coordinator. It never simulates keys and never touches
// chat.Session directly.
package localinput

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	// ProtocolVersion is the JSON protocol version spoken on the wire.
	ProtocolVersion = 1
	// MaxFrameBytes bounds a single request frame (design §5: 256 KiB).
	MaxFrameBytes = 256 * 1024
)

// Request is an inbound newline-delimited JSON frame.
type Request struct {
	Version   int    `json:"version"`
	Op        string `json:"op"` // submit | status | clear | resume | list | voice_stop
	RequestID string `json:"request_id,omitempty"`
	SessionID string `json:"session_id,omitempty"`
	When      string `json:"when,omitempty"` // step | turn (submit only)
	Source    string `json:"source,omitempty"`
	Text      string `json:"text,omitempty"`
}

// Reply is the single JSON response frame for a request.
type Reply struct {
	Version     int      `json:"version"`
	RequestID   string   `json:"request_id,omitempty"`
	Status      string   `json:"status"` // accepted|pending|injected|discarded|paused|rejected|error
	Code        string   `json:"code,omitempty"`
	Durability  string   `json:"durability,omitempty"`
	SessionID   string   `json:"session_id,omitempty"`
	When        string   `json:"when,omitempty"`
	Sequence    int64    `json:"sequence,omitempty"`
	QueueCounts *Counts  `json:"queue_counts,omitempty"`
	InjectedIDs []string `json:"injected_message_ids,omitempty"`
	Message     string   `json:"message,omitempty"`
}

// Counts mirrors inputqueue.Counts for the wire format.
type Counts struct {
	Step int `json:"step"`
	Turn int `json:"turn"`
}

// Encode serializes a frame with a trailing newline.
func (r Reply) Encode() ([]byte, error) {
	data, err := json.Marshal(r)
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}

// DecodeRequest parses one frame, enforcing the size limit and protocol version.
func DecodeRequest(data []byte) (Request, error) {
	var req Request
	if err := json.Unmarshal(data, &req); err != nil {
		return req, fmt.Errorf("invalid_json: %v", err)
	}
	if req.Version != ProtocolVersion {
		return req, fmt.Errorf("unsupported_version: got %d want %d", req.Version, ProtocolVersion)
	}
	return req, nil
}

// Reject builds a structured rejection reply.
func Reject(code, msg string) Reply {
	return Reply{Version: ProtocolVersion, Status: "rejected", Code: code, Message: msg}
}

// RuntimeDir returns the per-UID runtime directory for squid-os sockets,
// creating it with strict permissions. Symlinks are refused.
func RuntimeDir() (string, error) {
	base := os.Getenv("XDG_RUNTIME_DIR")
	if base == "" {
		home, _ := os.UserHomeDir()
		base = filepath.Join(home, ".run")
	}
	dir := filepath.Join(base, "squid-os")
	if fi, err := os.Lstat(dir); err == nil {
		if fi.Mode()&os.ModeSymlink != 0 {
			return "", fmt.Errorf("runtime dir is a symlink: %s", dir)
		}
	} else if !os.IsNotExist(err) {
		return "", err
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", err
	}
	return dir, nil
}

// SocketPath returns the socket path for an instance ID. Instance IDs are kept
// short so the full path stays under the Unix sun_path limit.
func SocketPath(instanceID string) (string, error) {
	dir, err := RuntimeDir()
	if err != nil {
		return "", err
	}
	id := sanitizeInstanceID(instanceID)
	p := filepath.Join(dir, id+".sock")
	if len(p) > 104 {
		return "", fmt.Errorf("socket path too long (%d bytes)", len(p))
	}
	return p, nil
}

func sanitizeInstanceID(id string) string {
	id = strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			return r
		default:
			return '_'
		}
	}, id)
	if len(id) > 48 {
		id = id[:48]
	}
	if id == "" {
		id = "instance"
	}
	return id
}

// OwnUID is the effective UID of this process.
func OwnUID() int { return os.Geteuid() }
