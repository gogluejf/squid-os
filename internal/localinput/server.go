package localinput

import (
	"bufio"
	"fmt"
	"net"
	"os"
	"strings"
	"sync"
	"time"

	"squid-os/internal/inputqueue"
)

// AdmissionFunc is the application admission entry point (wired to
// app.SubmissionCoordinator.AdmitSubmission by the TUI). It must be safe for
// concurrent use. The server never touches chat.Session directly.
type AdmissionFunc func(req inputqueue.AdmissionRequest) ReceiptInfo

// ReceiptInfo is the transport-neutral result of an admission attempt.
type ReceiptInfo struct {
	OK         bool
	Code       string
	Message    string
	Durability string
	Sequence   int64
	Duplicate  bool
	Counts     Counts
}

// StatusFunc reports queue state for status/list operations.
type StatusFunc func(sessionID string) (counts Counts, paused bool, err error)

// ControlFunc handles clear/resume operations. action is "clear" or "resume".
type ControlFunc func(action, sessionID string) (affected int, err error)

// Voice stop reply statuses (app-side handler → server). The three-valued
// result keeps a wedged UI loop distinguishable from "no active speech":
// only Cancelled and NothingPlaying are success outcomes; NotReady is a
// failure the server maps to a rejection.
const (
	VoiceStopCancelled       = "cancelled"
	VoiceStopNothingPlaying  = "nothing_playing"
	VoiceStopNotReady        = "receiver_not_ready"
)

// VoiceStopFunc cancels the active speech group (the same CancelGroup path as
// alt+x). It returns one of the VoiceStop* status strings: Cancelled when an
// active group was killed, NothingPlaying for the explicit no-op, or
// NotReady when the receiver could not confirm either (wedged UI loop).
// Must be safe for concurrent use.
type VoiceStopFunc func() string

// ServerConfig wires the socket server to the application coordinator.
type ServerConfig struct {
	InstanceID string
	SessionID  func() string // live session identity
	Admit      AdmissionFunc
	Status     StatusFunc
	Control    ControlFunc
	VoiceStop  VoiceStopFunc
}

// Server serves the private local input socket for one TUI instance.
type Server struct {
	cfg    ServerConfig
	listen net.Listener
	path   string

	mu        sync.Mutex
	conns     map[net.Conn]struct{}
	closing   bool
	closeOnce sync.Once
	done      chan struct{}
}

// Start binds the socket and begins accepting in a background goroutine.
// The socket is published only after binding succeeds; callers should treat
// a successful return as "accept path ready".
func (c ServerConfig) Start() (*Server, error) {
	path, err := SocketPath(c.InstanceID)
	if err != nil {
		return nil, err
	}
	// Refuse to steal another live process's socket: only remove if unconnectable.
	if _, err := os.Lstat(path); err == nil {
		conn, derr := net.DialTimeout("unix", path, 200*time.Millisecond)
		if derr == nil {
			conn.Close()
			return nil, fmt.Errorf("socket already live at %s", path)
		}
		if err := os.Remove(path); err != nil {
			return nil, err
		}
	}
	l, err := net.Listen("unix", path)
	if err != nil {
		return nil, err
	}
	if err := os.Chmod(path, 0600); err != nil {
		l.Close()
		return nil, err
	}
	s := &Server{
		cfg:    c,
		listen: l,
		path:   path,
		conns:  make(map[net.Conn]struct{}),
		done:   make(chan struct{}),
	}
	go s.acceptLoop()
	return s, nil
}

// Path returns the bound socket path.
func (s *Server) Path() string { return s.path }

// Close stops accepting, drops active connections, and removes the socket.
func (s *Server) Close() {
	s.closeOnce.Do(func() {
		s.mu.Lock()
		s.closing = true
		for c := range s.conns {
			c.Close()
		}
		s.conns = make(map[net.Conn]struct{})
		s.mu.Unlock()
		s.listen.Close()
		os.Remove(s.path)
		close(s.done)
	})
}

// Done is closed when the server has fully stopped.
func (s *Server) Done() <-chan struct{} { return s.done }

func (s *Server) acceptLoop() {
	for {
		conn, err := s.listen.Accept()
		if err != nil {
			return
		}
		s.mu.Lock()
		if s.closing {
			s.mu.Unlock()
			conn.Close()
			return
		}
		s.conns[conn] = struct{}{}
		s.mu.Unlock()
		go s.handleConn(conn)
	}
}

func (s *Server) handleConn(conn net.Conn) {
	defer func() {
		conn.Close()
		s.mu.Lock()
		delete(s.conns, conn)
		s.mu.Unlock()
	}()
	_ = conn.SetDeadline(time.Now().Add(10 * time.Second))
	r := bufio.NewReaderSize(conn, MaxFrameBytes)
	for {
		data, err := r.ReadBytes('\n')
		if len(data) > 0 && data[len(data)-1] == '\n' {
			data = data[:len(data)-1]
		}
		if err != nil || len(data) == 0 {
			return
		}
		if len(data) > MaxFrameBytes {
			s.reply(conn, Reject("frame_too_large", "request exceeds 256 KiB frame limit"))
			continue
		}
		req, perr := DecodeRequest(data)
		if perr != nil {
			code := "invalid_request"
			msg := perr.Error()
			if strings.HasPrefix(msg, "unsupported_version") {
				code = "unsupported_version"
			} else if strings.HasPrefix(msg, "invalid_json") {
				code = "invalid_json"
			}
			s.reply(conn, Reject(code, msg))
			continue
		}
		s.reply(conn, s.dispatch(req))
		if err != nil {
			return
		}
	}
}

func (s *Server) reply(conn net.Conn, r Reply) {
	data, err := r.Encode()
	if err != nil {
		return
	}
	conn.SetDeadline(time.Now().Add(5 * time.Second))
	_, _ = conn.Write(data)
}

func (s *Server) dispatch(req Request) Reply {
	switch req.Op {
	case "submit":
		return s.submit(req)
	case "status":
		return s.status(req)
	case "list":
		return s.list(req)
	case "clear":
		return s.control(req, "clear")
	case "resume":
		return s.control(req, "resume")
	case "voice_stop":
		return s.voiceStop(req)
	default:
		return Reject("unknown_op", "unsupported op "+req.Op)
	}
}

func (s *Server) submit(req Request) Reply {
	if s.cfg.Admit == nil {
		return Reject("receiver_unavailable", "admission path not ready")
	}
	if req.RequestID == "" {
		return Reject("missing_request_id", "submit requires request_id")
	}
	session := req.SessionID
	if session == "" {
		session = s.liveSession()
	}
	if session == "" {
		return Reject("wrong_session", "no target session")
	}
	boundary := inputqueue.BoundaryStep
	if req.When == "turn" {
		boundary = inputqueue.BoundaryTurn
	}
	res := s.cfg.Admit(inputqueue.AdmissionRequest{
		RequestID: req.RequestID,
		SessionID: session,
		Source:    orDefault(req.Source, "local"),
		Text:      req.Text,
		Boundary:  boundary,
	})
	if !res.OK {
		return Reply{Version: ProtocolVersion, RequestID: req.RequestID, Status: "rejected", Code: res.Code, Message: res.Message}
	}
	status := "accepted"
	if res.Duplicate {
		status = "accepted" // idempotent retry: same receipt
	}
	return Reply{
		Version:     ProtocolVersion,
		RequestID:   req.RequestID,
		Status:      status,
		Durability:  res.Durability,
		SessionID:   session,
		When:        string(boundary),
		Sequence:    res.Sequence,
		QueueCounts: &res.Counts,
	}
}

func (s *Server) status(req Request) Reply {
	session := req.SessionID
	if session == "" {
		session = s.liveSession()
	}
	if session == "" {
		return Reject("wrong_session", "no target session")
	}
	counts, paused, err := s.cfg.Status(session)
	if err != nil {
		return Reject("store_error", err.Error())
	}
	st := "pending"
	if paused {
		st = "paused"
	}
	return Reply{
		Version:     ProtocolVersion,
		RequestID:   req.RequestID,
		Status:      st,
		SessionID:   session,
		QueueCounts: &counts,
	}
}

func (s *Server) list(req Request) Reply {
	session := s.liveSession()
	if session == "" {
		return Reject("wrong_session", "no live session")
	}
	counts, paused, err := s.cfg.Status(session)
	if err != nil {
		return Reject("store_error", err.Error())
	}
	st := "pending"
	if paused {
		st = "paused"
	}
	return Reply{
		Version:     ProtocolVersion,
		Status:      st,
		SessionID:   session,
		QueueCounts: &counts,
	}
}

func (s *Server) control(req Request, action string) Reply {
	if s.cfg.Control == nil {
		return Reject("receiver_unavailable", "control path not ready")
	}
	session := req.SessionID
	if session == "" {
		session = s.liveSession()
	}
	if session == "" {
		return Reject("wrong_session", "no target session")
	}
	n, err := s.cfg.Control(action, session)
	if err != nil {
		return Reject("store_error", err.Error())
	}
	code := "cleared"
	if action == "resume" {
		code = "resumed"
	}
	return Reply{
		Version:   ProtocolVersion,
		Status:    "accepted",
		Code:      code,
		SessionID: session,
		Message:   fmt.Sprintf("%d entr%s affected", n, plural(n)),
	}
}

func (s *Server) voiceStop(req Request) Reply {
	if s.cfg.VoiceStop == nil {
		return Reject("receiver_unavailable", "voice control path not ready")
	}
	session := req.SessionID
	if session == "" {
		session = s.liveSession()
	}
	if session == "" || session != s.liveSession() {
		return Reject("wrong_session", "no target session")
	}
	status := s.cfg.VoiceStop()
	switch status {
	case VoiceStopCancelled:
		return Reply{Version: ProtocolVersion, Status: "cancelled", SessionID: session, Message: "active speech group cancelled"}
	case VoiceStopNothingPlaying:
		return Reply{Version: ProtocolVersion, Status: "nothing_playing", SessionID: session, Message: "no active speech group"}
	default:
		// receiver_not_ready (or any unexpected value): the UI loop could not
		// confirm cancellation or no-op — reject so the CLI exits non-zero.
		return Reject("receiver_unavailable", "voice control path not ready")
	}
}

func (s *Server) liveSession() string {
	if s.cfg.SessionID == nil {
		return ""
	}
	return s.cfg.SessionID()
}

func orDefault(v, def string) string {
	if v == "" {
		return def
	}
	return v
}

func plural(n int) string {
	if n == 1 {
		return "y"
	}
	return "ies"
}
