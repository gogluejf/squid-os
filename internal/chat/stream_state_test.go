package chat

import (
	"testing"
	"time"
)

func TestStreamStateBeginInitializesRequestMetrics(t *testing.T) {
	state := StreamState{Text: "stale", Active: false}
	before := time.Now()

	state.Begin()

	if state.Metrics.Start.Before(before) || state.Metrics.Start.After(time.Now()) {
		t.Fatalf("unexpected stream start: %v", state.Metrics.Start)
	}
	if !state.Active {
		t.Fatal("stream should be active")
	}
	if state.Text != "" {
		t.Fatalf("begin should clear stale state, got %q", state.Text)
	}
}

func TestProcessStreamEventPersistsFiniteTimingAfterBegin(t *testing.T) {
	session := &Session{}
	session.Stream.Begin()
	session.Stream.Metrics.Start = time.Now().Add(-25 * time.Millisecond)

	result := ProcessStreamEvent(session, StreamEvent{
		Text:       "hello",
		Done:       true,
		StopReason: "stop",
	})
	if result.Action != LoopDone {
		t.Fatalf("expected done, got %v", result.Action)
	}

	message := session.Doc.Messages[result.MsgIdx]
	if message.CreatedAt.IsZero() {
		t.Fatal("assistant created_at must not be zero")
	}
	if message.TimeToFirstTokenMs < 0 || message.TimeToFirstTokenMs > 10_000 {
		t.Fatalf("invalid TTFT: %dms", message.TimeToFirstTokenMs)
	}
	if message.DurationTimeMs < 0 || message.DurationTimeMs > 10_000 {
		t.Fatalf("invalid duration: %dms", message.DurationTimeMs)
	}
	if message.TextMetrics.TimeToFirstTokenMs < 0 || message.TextMetrics.TimeToFirstTokenMs > 10_000 {
		t.Fatalf("invalid text TTFT: %dms", message.TextMetrics.TimeToFirstTokenMs)
	}
}
