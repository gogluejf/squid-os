package cli

import (
	"bufio"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"squid-os/internal/localinput"
)

// ── input CLI (design §5) ───────────────────────────────────────────────────
//
// squid-os input <list|send|status|clear> talks to a live TUI instance
// over its private Unix socket. Text is read from stdin, never argv.

type inputOptions struct {
	Instance string
	Session  string
	ID       string
	When     string
	Source   string
	JSON     bool
}

type inputCmd struct{}

var _ Command[inputOptions] = inputCmd{}

func (inputCmd) Spec() CommandSpec {
	return CommandSpec{Use: "input", Short: "Talk to a live TUI instance's input queue", Runnable: false}
}

func (inputCmd) Flags(f *pflag.FlagSet) *inputOptions {
	o := &inputOptions{}
	f.StringVar(&o.Instance, "instance", "", "target instance ID (default: auto-discover)")
	f.StringVar(&o.Session, "session", "", "target session ID (default: the instance's live session)")
	f.StringVar(&o.ID, "id", "", "stable request UUID (send; minted when omitted)")
	f.StringVar(&o.When, "when", "step", "boundary for send: step | turn")
	f.StringVar(&o.Source, "source", "cli", "submission source label")
	f.BoolVar(&o.JSON, "json", true, "emit JSON result")
	return o
}

func (inputCmd) Prepare(c *cobra.Command, o *inputOptions, args []string) error { return nil }
func (inputCmd) Run(o *inputOptions, io CommandIO) error                        { return nil }
func (inputCmd) Completions() []Completion                                      { return nil }

type inputSubCmd struct {
	name      string
	short     string
	execute   func(*inputOptions, CommandIO) error
	readStdin bool
}

func (c inputSubCmd) Spec() CommandSpec {
	return CommandSpec{Use: c.name, Short: c.short, Args: cobra.NoArgs, Runnable: true}
}
func (c inputSubCmd) Flags(f *pflag.FlagSet) *inputOptions { return inputCmd{}.Flags(f) }
func (c inputSubCmd) Prepare(cmd *cobra.Command, o *inputOptions, args []string) error {
	if c.readStdin && cmd.InOrStdin() == nil {
		cmd.SetIn(os.Stdin)
	}
	return nil
}
func (c inputSubCmd) Run(o *inputOptions, io CommandIO) error { return c.execute(o, io) }
func (c inputSubCmd) Completions() []Completion               { return nil }

// registerInputCommands attaches the input command tree to root.
func registerInputCommands(root *cobra.Command) {
	parent := makeCobra(inputCmd{})
	root.AddCommand(parent)
	parent.AddCommand(
		makeCobra(inputSubCmd{name: "list", short: "List pending queued input", execute: inputList}),
		makeCobra(inputSubCmd{name: "send", short: "Send text to the queue (stdin)", execute: inputSend, readStdin: true}),
		makeCobra(inputSubCmd{name: "status", short: "Status of one request or the queue", execute: inputStatus}),
		makeCobra(inputSubCmd{name: "clear", short: "Discard all pending queued input", execute: inputClear}),
	)
}

// ── discovery ───────────────────────────────────────────────────────────────

// discoverInstance resolves the target socket path. Exactly one compatible
// instance → use it; explicit --instance → use it; multiple → ambiguous.
func discoverInstance(instanceID string) (string, error) {
	dir, err := localinput.RuntimeDir()
	if err != nil {
		return "", err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("no running TUI found")
		}
		return "", err
	}
	var live []string
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".sock") {
			continue
		}
		p := filepath.Join(dir, e.Name())
		conn, derr := net.DialTimeout("unix", p, 150*time.Millisecond)
		if derr != nil {
			continue // stale socket file
		}
		conn.Close()
		id := strings.TrimSuffix(e.Name(), ".sock")
		if instanceID != "" && id != instanceID {
			continue
		}
		live = append(live, id)
	}
	switch {
	case instanceID != "":
		if len(live) == 0 {
			return "", fmt.Errorf("instance %q not found or not accepting", instanceID)
		}
		return localinput.SocketPath(instanceID)
	case len(live) == 1:
		return localinput.SocketPath(live[0])
	case len(live) > 1:
		return "", fmt.Errorf("ambiguous_target: %d instances running (%s); use --instance", len(live), strings.Join(live, ", "))
	default:
		return "", fmt.Errorf("no running TUI found")
	}
}

// ── transport ───────────────────────────────────────────────────────────────

// roundTrip dials the socket, sends one frame, and reads the reply. A timeout
// yields an UNCERTAIN result (the server may have accepted): callers must
// retry with the SAME request ID, never a new one.
func roundTrip(path string, req localinput.Request) (localinput.Reply, error) {
	conn, err := net.DialTimeout("unix", path, 2*time.Second)
	if err != nil {
		return localinput.Reply{}, fmt.Errorf("unavailable: %w", err)
	}
	defer conn.Close()
	data, _ := json.Marshal(req)
	if _, err := conn.Write(append(data, '\n')); err != nil {
		return localinput.Reply{}, fmt.Errorf("unavailable: %w", err)
	}
	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	line, err := bufio.NewReader(conn).ReadBytes('\n')
	if err != nil {
		return localinput.Reply{}, fmt.Errorf("uncertain: no reply (%v) — retry with the same --id", err)
	}
	var r localinput.Reply
	if err := json.Unmarshal(line[:len(line)-1], &r); err != nil {
		return localinput.Reply{}, fmt.Errorf("bad reply from server: %w", err)
	}
	return r, nil
}

func printReply(out io.Writer, r localinput.Reply) {
	data, _ := json.MarshalIndent(r, "", "  ")
	fmt.Fprintln(out, string(data))
}

func exitCodeFor(r localinput.Reply) int {
	switch {
	case r.Status == "accepted", r.Status == "pending", r.Status == "injected":
		return 0
	case r.Code == "frame_too_large", r.Code == "invalid_request", r.Code == "unsupported_version",
		r.Code == "wrong_session", r.Code == "missing_request_id", r.Code == "unknown_op",
		r.Code == "queue_full", r.Code == "id_conflict":
		return 2 // permanent rejection
	default:
		return 1
	}
}

// ── subcommands ─────────────────────────────────────────────────────────────

func inputList(o *inputOptions, io CommandIO) error {
	path, err := discoverInstance(o.Instance)
	if err != nil {
		return err
	}
	r, err := roundTrip(path, localinput.Request{Version: localinput.ProtocolVersion, Op: "list"})
	if err != nil {
		return err
	}
	printReply(io.Out, r)
	return exitErr(exitCodeFor(r))
}

func inputSend(o *inputOptions, io CommandIO) error {
	text, err := bufio.NewReader(io.In).ReadString('\n')
	if err != nil && text == "" {
		return fmt.Errorf("empty input on stdin")
	}
	text = strings.TrimSuffix(text, "\n")
	if strings.TrimSpace(text) == "" {
		return fmt.Errorf("empty input on stdin")
	}
	id := o.ID
	if id == "" {
		id = mintUUID()
	}
	req := localinput.Request{
		Version: localinput.ProtocolVersion, Op: "submit", RequestID: id,
		SessionID: o.Session, When: o.When, Source: o.Source, Text: text,
	}
	path, err := discoverInstance(o.Instance)
	if err != nil {
		return err
	}
	r, err := roundTrip(path, req)
	if err != nil {
		return err
	}
	printReply(io.Out, r)
	return exitErr(exitCodeFor(r))
}

func inputStatus(o *inputOptions, io CommandIO) error {
	path, err := discoverInstance(o.Instance)
	if err != nil {
		return err
	}
	r, err := roundTrip(path, localinput.Request{Version: localinput.ProtocolVersion, Op: "status", RequestID: o.ID, SessionID: o.Session})
	if err != nil {
		return err
	}
	printReply(io.Out, r)
	return exitErr(exitCodeFor(r))
}

func inputClear(o *inputOptions, io CommandIO) error {
	path, err := discoverInstance(o.Instance)
	if err != nil {
		return err
	}
	r, err := roundTrip(path, localinput.Request{Version: localinput.ProtocolVersion, Op: "clear", SessionID: o.Session})
	if err != nil {
		return err
	}
	printReply(io.Out, r)
	return exitErr(exitCodeFor(r))
}

// ── helpers ─────────────────────────────────────────────────────────────────

// exitErr returns an error that carries an exit code without printing usage.
type exitError struct{ code int }

func (e exitError) Error() string { return fmt.Sprintf("exit %d", e.code) }
func exitErr(code int) error {
	if code == 0 {
		return nil
	}
	return exitError{code: code}
}

// mintUUID produces a v4-shaped UUID without pulling in google/uuid here.
func mintUUID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		// Fallback: time-based (still unique enough for CLI retries).
		now := time.Now().UnixNano()
		for i := 0; i < 16; i++ {
			b[i] = byte(now >> (8 * (i % 8)))
			now ^= int64(i + 1)
		}
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}
