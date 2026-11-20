package log

import (
	"log"
	"strings"
	"testing"
	"time"
)

type voiceTestWriter chan string

func (w voiceTestWriter) Write(p []byte) (int, error) { w <- string(p); return len(p), nil }

func TestVoiceDebugGateAndFullQuotedText(t *testing.T) {
	out := make(voiceTestWriter, 8)
	sseOut := make(voiceTestWriter, 8)
	mu.Lock()
	oldSSE := sseLogger
	sseLogger = log.New(sseOut, "", 0)
	old := voiceLogger
	voiceLogger = log.New(out, "", 0)
	mu.Unlock()
	defer func() { SetEnabled(false); mu.Lock(); voiceLogger = old; sseLogger = oldSSE; mu.Unlock() }()
	SetEnabled(false)
	LogVoice("hidden", "text=%q", "private")
	select {
	case line := <-out:
		t.Fatal(line)
	case <-time.After(20 * time.Millisecond):
	}
	SetEnabled(true)
	text := strings.Repeat("bonjour ", 30) + "\nfrançais"
	LogVoice("submission", "submission=%d text=%q", 7, text)
	select {
	case line := <-out:
		if !strings.Contains(line, "voice.submission submission=7") || !strings.Contains(line, `\nfrançais`) || strings.Count(line, "bonjour") != 30 {
			t.Fatal(line)
		}
		select {
		case line := <-sseOut:
			t.Fatalf("voice leaked into SSE log: %s", line)
		default:
		}
	case <-time.After(time.Second):
		t.Fatal("missing trace")
	}
}
