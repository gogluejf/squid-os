// Package voice speaks text through the local squid-tts server.
//
// Two layers:
//   - Speaker: one sentence = one short-lived `squid-tts-say` process in its
//     own process group (spawn → teardown; TTS auto-detects language).
//   - Coordinator: owns speech-group state (StartGroup/Emit/EndGroup/
//     CancelGroup) and arbitrates groups via the onPreempt policy hook.
package voice

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"time"

	"squid-os/internal/config"
	"squid-os/internal/log"
)

const (
	// CLIName is resolved from $PATH at spawn time — never hardcoded.
	CLIName = "squid-tts-say"

	// DefaultServerURL is the local squid-tts server (overridable in tests).
	DefaultServerURL = "http://127.0.0.1:8811"

	// defaultDetectTimeout bounds a /detect round-trip; detection is ~75 ms,
	// so this only trips when the server is wedged.
	defaultDetectTimeout = 3 * time.Second

	// defaultSpeakTimeout bounds the whole speak pipeline (spawn + synthesize
	// + playback). Generous for long sentences; cancellation kills earlier.
	defaultSpeakTimeout = 60 * time.Second

	fallbackLang = "en"
)

// ErrVoiceNotFound is returned when no voice name resolves for the detected
// language and no fallback exists either.
var ErrVoiceNotFound = errors.New("voice: no voice configured for language")

// SpeakerOptions are test seams for the Speaker. Zero values use real
// defaults; tests override them to avoid hitting the real server or PATH.
type SpeakerOptions struct {
	ServerURL    string        // default http://127.0.0.1:8811
	DetectClient *http.Client  // default new client with detect timeout
	SpeakTimeout time.Duration // default 60s per chunk
	CLIPath      string        // default: exec.LookPath(CLIName)
}

// Speaker turns one sentence into audio. Each Speak call is fully
// self-contained: spawn → wait. No stream state.
type Speaker struct {
	opts SpeakerOptions
	http *http.Client
	cli  string
}

// NewSpeaker builds a Speaker over the local TTS server.
func NewSpeaker(opts SpeakerOptions) (*Speaker, error) {
	if opts.ServerURL == "" {
		opts.ServerURL = DefaultServerURL
	}
	if opts.DetectClient == nil {
		opts.DetectClient = &http.Client{Timeout: defaultDetectTimeout}
	}
	if opts.SpeakTimeout <= 0 {
		opts.SpeakTimeout = defaultSpeakTimeout
	}
	if opts.CLIPath == "" {
		path, err := exec.LookPath(CLIName)
		if err != nil {
			return nil, fmt.Errorf("voice: %w", err)
		}
		opts.CLIPath = path
	}
	return &Speaker{opts: opts, http: opts.DetectClient, cli: opts.CLIPath}, nil
}

// DetectLanguage POSTs the text to the server's /detect endpoint and returns
// the detected language code ("en"/"fr"). Empty result means undecidable.
// It never panics; server down or timeout yields an error.
func (s *Speaker) DetectLanguage(ctx context.Context, text string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, s.opts.DetectClient.Timeout)
	defer cancel()

	body, _ := json.Marshal(map[string]string{"text": text})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		strings.TrimRight(s.opts.ServerURL, "/")+"/detect", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("voice: detect: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		detail, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return "", fmt.Errorf("voice: detect: server returned %s: %s",
			resp.Status, strings.TrimSpace(string(detail)))
	}

	var out struct {
		Language string `json:"language"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", fmt.Errorf("voice: detect: bad response: %w", err)
	}
	return out.Language, nil
}

// resolveVoice picks the voice name for lang via cfg.Voices, falling back to
// the previous session language and then "en". An unknown language never
// fails the lookup as long as some fallback entry exists.
func resolveVoice(cfg *config.Voice, lang, prevLang string) (name string, ok bool) {
	for _, l := range []string{lang, prevLang, fallbackLang} {
		if l == "" {
			continue
		}
		if v, found := cfg.Voices[l]; found && v != "" {
			return v, true
		}
	}
	return "", false
}

// Speak delegates language detection to TTS. alertLang is only supplied for
// localized system alerts, selecting cfg.Voices without forcing TTS language.
// Narration uses a common configured voice when en/fr agree; otherwise it omits
// -v so the server selects its per-language default (no client detection).
//
// The process runs in a fresh process group (Setpgid) so Kill can SIGKILL
// the whole tree — including the aplay child — leaving no orphans.
func (s *Speaker) Speak(ctx context.Context, text string, cfg *config.Voice, alertLang string) (*Process, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil, errors.New("voice: nothing to speak")
	}

	if err := ctx.Err(); err != nil {
		return nil, err
	}
	synthesisLang := "auto"
	if alertLang == "fr" || alertLang == "en" {
		synthesisLang = alertLang
	}
	args := []string{"raw", "-q", "--stdin", "-l", synthesisLang, "-s", s.opts.ServerURL}
	name := ""
	if alertLang != "" {
		var ok bool
		name, ok = resolveVoice(cfg, alertLang, "")
		if !ok {
			return nil, fmt.Errorf("%w (%q)", ErrVoiceNotFound, alertLang)
		}
	} else if cfg.Voices["en"] == cfg.Voices["fr"] {
		name = cfg.Voices["en"]
	}
	if name != "" {
		args = append(args, "-v", name)
	}

	ctx, cancel := context.WithTimeout(ctx, s.opts.SpeakTimeout)
	// Ownership transfers to the process goroutine after Start succeeds.

	cmd := exec.CommandContext(ctx, s.cli, args...)
	// Own process group: killing -pid takes the CLI and its aplay child
	// together. Same pattern as internal/tools/bash.go.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return errors.New("process not started")
		}
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}

	stdin, err := cmd.StdinPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("voice: stdin pipe: %w", err)
	}
	log.LogVoice("command.attempt", "argv=%q stdin=%q", cmd.Args, text)
	if err := cmd.Start(); err != nil {
		log.LogVoice("command.start_failed", "argv=%q", cmd.Args)
		cancel()
		_ = stdin.Close()
		return nil, fmt.Errorf("voice: spawn: %w", err)
	}

	log.LogVoice("command.started", "pid=%d argv=%q", cmd.Process.Pid, cmd.Args)
	p := &Process{cmd: cmd, done: make(chan struct{})}
	go func() {
		defer cancel()
		p.run(stdin, text)
	}()
	return p, nil
}

// Process is one spawned chunk: the squid-tts-say process plus its watcher.
// Wait is called exactly once by Process.run.
type Process struct {
	cmd  *exec.Cmd
	done chan struct{}
	err  error

	once sync.Once
}

// run writes the sentence to stdin, waits exactly once, and records the
// exit status. The watcher retires here, so late kills cannot hit a reused
// PID.
func (p *Process) run(stdin io.WriteCloser, text string) {
	p.err = func() error {
		_, werr := io.WriteString(stdin, text)
		cerr := stdin.Close()
		waitErr := p.cmd.Wait() // Reap even when writing stdin failed.
		if werr != nil {
			return fmt.Errorf("voice: write: %w", werr)
		}
		if cerr != nil {
			return fmt.Errorf("voice: close stdin: %w", cerr)
		}
		if waitErr != nil {
			return fmt.Errorf("voice: play: %w", waitErr)
		}
		return nil
	}()
	log.LogVoice("command.finished", "pid=%d success=%t", p.Pid(), p.err == nil)
	p.once.Do(func() { close(p.done) })
}

// Done is closed when the process has fully exited (normal or killed).
func (p *Process) Done() <-chan struct{} { return p.done }

// Err reports the exit status after Done has closed.
func (p *Process) Err() error { return p.err }

// Pid is the CLI process ID (== its process group ID because of Setpgid).
func (p *Process) Pid() int { return p.cmd.Process.Pid }

// Kill terminates the whole process group (CLI + aplay). Safe to call
// multiple times; kills after normal exit hit no one.
func (p *Process) Kill() {
	select {
	case <-p.done:
		return
	default:
	}
	if p.cmd.Process == nil {
		return
	}
	_ = syscall.Kill(-p.cmd.Process.Pid, syscall.SIGKILL)
}
