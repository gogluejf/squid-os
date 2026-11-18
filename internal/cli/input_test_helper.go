package cli

import (
	"fmt"
	"strings"
	"sync"
	"testing"

	"squid-os/internal/inputqueue"
	"squid-os/internal/localinput"
)

// fakeStore is the in-memory coordinator backing the CLI socket tests.
type fakeStore struct {
	mu    sync.Mutex
	store []string
}

func (f *fakeStore) admit(req inputqueue.AdmissionRequest) localinput.ReceiptInfo {
	f.mu.Lock()
	defer f.mu.Unlock()
	if req.RequestID == "" || strings.TrimSpace(req.Text) == "" {
		return localinput.ReceiptInfo{Code: "invalid_request", Message: "bad request"}
	}
	f.store = append(f.store, req.Text)
	return localinput.ReceiptInfo{OK: true, Durability: "disk", Sequence: int64(len(f.store)) - 1,
		Counts: localinput.Counts{Step: len(f.store)}}
}

type localinputServerHandle = localinput.Server

type localinputServerConfig = localinput.ServerConfig

// newTestServer starts a real localinput.Server with the fake coordinator.
func newTestServer(t *testing.T, instanceID string, fs *fakeStore) (*localinput.Server, *fakeStore) {
	t.Helper()
	sess := "aaaaaaaa-1111-2222-3333-bbbbbbbbbbbb"
	cfg := localinput.ServerConfig{
		InstanceID: instanceID,
		SessionID:  func() string { return sess },
		Admit:      fs.admit,
		Status: func(sessionID string) (localinput.Counts, bool, error) {
			fs.mu.Lock()
			defer fs.mu.Unlock()
			return localinput.Counts{Step: len(fs.store)}, false, nil
		},
		Control: func(action, sessionID string) (int, error) {
			fs.mu.Lock()
			defer fs.mu.Unlock()
			if action == "clear" {
				n := len(fs.store)
				fs.store = nil
				return n, nil
			}
			return 0, fmt.Errorf("unknown %q", action)
		},
	}
	srv, err := cfg.Start()
	if err != nil {
		t.Fatalf("start server: %v", err)
	}
	t.Cleanup(srv.Close)
	return srv, fs
}
