package localinput

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"squid-os/internal/inputqueue"
)

// fakeCoordinator is an in-memory AdmissionFunc/StatusFunc/ControlFunc pair.
type fakeCoordinator struct {
	mu     sync.Mutex
	store  map[string][]string // sessionID → admitted texts (FIFO)
	paused map[string]bool
}

func newFake() *fakeCoordinator {
	return &fakeCoordinator{store: make(map[string][]string), paused: make(map[string]bool)}
}

func (f *fakeCoordinator) admit(req inputqueue.AdmissionRequest) ReceiptInfo {
	f.mu.Lock()
	defer f.mu.Unlock()
	if req.RequestID == "" || strings.TrimSpace(req.Text) == "" {
		return ReceiptInfo{Code: "invalid_request", Message: "bad request"}
	}
	f.store[req.SessionID] = append(f.store[req.SessionID], req.Text)
	return ReceiptInfo{OK: true, Durability: "disk", Sequence: int64(len(f.store[req.SessionID])) - 1,
		Counts: Counts{Step: len(f.store[req.SessionID])}}
}

func (f *fakeCoordinator) status(sessionID string) (Counts, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := len(f.store[sessionID])
	return Counts{Step: n}, f.paused[sessionID], nil
}

func (f *fakeCoordinator) control(action, sessionID string) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	switch action {
	case "clear":
		n := len(f.store[sessionID])
		f.store[sessionID] = nil
		f.paused[sessionID] = false
		return n, nil
	case "resume":
		f.paused[sessionID] = false
		return 0, nil
	default:
		return 0, fmt.Errorf("unknown %q", action)
	}
}

// startTestServer runs a real socket server on a temp XDG_RUNTIME_DIR.
func startTestServer(t *testing.T, fc *fakeCoordinator) (*Server, string) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("XDG_RUNTIME_DIR", dir)

	sess := "11111111-2222-3333-4444-555555555555"
	cfg := ServerConfig{
		InstanceID: "test-instance",
		SessionID:  func() string { return sess },
		Admit:      fc.admit,
		Status:     fc.status,
		Control:    fc.control,
	}
	srv, err := cfg.Start()
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	t.Cleanup(srv.Close)
	return srv, sess
}

// sendFrame dials the socket, writes one frame, and reads one reply.
func sendFrame(t *testing.T, path string, payload any) Reply {
	t.Helper()
	conn, err := net.DialTimeout("unix", path, 2*time.Second)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()
	data, _ := json.Marshal(payload)
	data = append(data, '\n')
	if _, err := conn.Write(data); err != nil {
		t.Fatalf("write: %v", err)
	}
	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	line, err := bufio.NewReader(conn).ReadBytes('\n')
	if err != nil {
		t.Fatalf("read reply: %v", err)
	}
	var r Reply
	if err := json.Unmarshal(bytes.TrimRight(line, "\n"), &r); err != nil {
		t.Fatalf("decode reply: %v (%q)", err, line)
	}
	return r
}

func TestSubmitAcceptedAndReceipt(t *testing.T) {
	fc := newFake()
	srv, sess := startTestServer(t, fc)

	r := sendFrame(t, srv.Path(), Request{Version: 1, Op: "submit", RequestID: "aaaaaaaa-0000-0000-0000-000000000001", SessionID: sess, When: "step", Source: "test", Text: "hello from socket"})
	if r.Status != "accepted" || r.Durability != "disk" {
		t.Fatalf("reply = %+v", r)
	}
	if r.Sequence != 0 || r.QueueCounts == nil || r.QueueCounts.Step != 1 {
		t.Fatalf("receipt fields wrong: %+v", r)
	}
	if got := fc.store[sess]; len(got) != 1 || got[0] != "hello from socket" {
		t.Fatalf("coordinator store = %+v", got)
	}
}

func TestSubmitRejectsMalformedAndOversize(t *testing.T) {
	fc := newFake()
	srv, sess := startTestServer(t, fc)

	// Wrong version.
	r := sendFrame(t, srv.Path(), Request{Version: 99, Op: "submit", RequestID: "aaaaaaaa-0000-0000-0000-000000000001", SessionID: sess, Text: "x"})
	if r.Status != "rejected" || r.Code != "unsupported_version" {
		t.Fatalf("version reject = %+v", r)
	}
	// Missing request ID.
	r = sendFrame(t, srv.Path(), Request{Version: 1, Op: "submit", SessionID: sess, Text: "x"})
	if r.Status != "rejected" || r.Code != "missing_request_id" {
		t.Fatalf("id reject = %+v", r)
	}
	// Oversize frame.
	big := Request{Version: 1, Op: "submit", RequestID: "aaaaaaaa-0000-0000-0000-000000000001", SessionID: sess, Text: strings.Repeat("x", MaxFrameBytes+10)}
	conn, _ := net.Dial("unix", srv.Path())
	data, _ := json.Marshal(big)
	conn.Write(append(data, '\n'))
	line, _ := bufio.NewReader(conn).ReadBytes('\n')
	conn.Close()
	var rr Reply
	json.Unmarshal(line, &rr)
	if rr.Code != "frame_too_large" && rr.Code != "invalid_json" {
		t.Fatalf("oversize = %+v", rr)
	}
}

func TestStatusListClearResume(t *testing.T) {
	fc := newFake()
	srv, sess := startTestServer(t, fc)

	sendFrame(t, srv.Path(), Request{Version: 1, Op: "submit", RequestID: "aaaaaaaa-0000-0000-0000-000000000001", SessionID: sess, Text: "one"})
	sendFrame(t, srv.Path(), Request{Version: 1, Op: "submit", RequestID: "bbbbbbbb-0000-0000-0000-000000000001", SessionID: sess, Text: "two"})

	r := sendFrame(t, srv.Path(), Request{Version: 1, Op: "status", SessionID: sess})
	if r.Status != "pending" || r.QueueCounts.Step != 2 {
		t.Fatalf("status = %+v", r)
	}
	r = sendFrame(t, srv.Path(), Request{Version: 1, Op: "list"})
	if r.Status != "pending" || r.QueueCounts.Step != 2 {
		t.Fatalf("list = %+v", r)
	}
	r = sendFrame(t, srv.Path(), Request{Version: 1, Op: "clear", SessionID: sess})
	if r.Status != "accepted" || !strings.Contains(r.Message, "2") {
		t.Fatalf("clear = %+v", r)
	}
	r = sendFrame(t, srv.Path(), Request{Version: 1, Op: "status", SessionID: sess})
	if r.QueueCounts.Step != 0 {
		t.Fatalf("after clear = %+v", r)
	}
	r = sendFrame(t, srv.Path(), Request{Version: 1, Op: "resume", SessionID: sess})
	if r.Status != "accepted" {
		t.Fatalf("resume = %+v", r)
	}
}

func TestConcurrentSubmissionsSerialize(t *testing.T) {
	fc := newFake()
	srv, sess := startTestServer(t, fc)

	const n = 20
	var wg sync.WaitGroup
	errs := make(chan error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			id := fmt.Sprintf("%08x-0000-4000-8000-%012d", i, i)
			r := sendFrame(t, srv.Path(), Request{Version: 1, Op: "submit", RequestID: id, SessionID: sess, Text: fmt.Sprintf("msg %d", i)})
			if r.Status != "accepted" {
				errs <- fmt.Errorf("submit %d: %+v", i, r)
			}
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
	if got := len(fc.store[sess]); got != n {
		t.Fatalf("admitted %d, want %d", got, n)
	}
}

func TestSocketPermissionsAndCleanup(t *testing.T) {
	fc := newFake()
	srv, _ := startTestServer(t, fc)

	fi, err := os.Stat(srv.Path())
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode()&os.FileMode(0777) != 0600 {
		t.Fatalf("socket mode = %v, want 0600", fi.Mode())
	}
	srv.Close()
	if _, err := os.Stat(srv.Path()); !os.IsNotExist(err) {
		t.Fatalf("socket not removed after Close: %v", err)
	}
	// Runtime dir must be 0700.
	rd, _ := RuntimeDir()
	dfi, _ := os.Stat(rd)
	if dfi.Mode()&os.FileMode(0777) != 0700 {
		t.Fatalf("runtime dir mode = %v, want 0700", dfi.Mode())
	}
	_ = filepath.Base(rd)
}
