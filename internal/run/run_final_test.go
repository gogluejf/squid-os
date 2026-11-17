package run

import (
	"testing"

	"squid-os/internal/config"
)

func TestResolveEmptyFinal(t *testing.T) {
	t.Run("recovers the thinking-only turn", func(t *testing.T) {
		msgs := []config.Message{
			{ID: "msg_1", Role: config.RoleAssistant, Text: "mid-run answer", ThinkingText: "mid-run reasoning"},
			{ID: "msg_2", Role: config.RoleUser, Text: "nudge"},
			{ID: "msg_3", Role: config.RoleAssistant, Text: "", ThinkingText: "thinking-only turn"},
		}
		got, err := resolveEmptyFinal(msgs)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		want := thinkingFallbackPrefix + "thinking-only turn"
		if got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	})

	t.Run("skips thinking-only turn after empty nudge turn", func(t *testing.T) {
		msgs := []config.Message{
			{ID: "msg_1", Role: config.RoleAssistant, Text: "", ThinkingText: "original reasoning"},
			{ID: "msg_2", Role: config.RoleUser, Text: "nudge"},
			{ID: "msg_3", Role: config.RoleAssistant, Text: "", ThinkingText: ""},
		}
		got, err := resolveEmptyFinal(msgs)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		want := thinkingFallbackPrefix + "original reasoning"
		if got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	})

	t.Run("mid-run thinking with text is not returned", func(t *testing.T) {
		msgs := []config.Message{
			{ID: "msg_1", Role: config.RoleAssistant, Text: "I did the work", ThinkingText: "stale reasoning"},
			{ID: "msg_2", Role: config.RoleAssistant, Text: "", ThinkingText: ""},
		}
		if _, err := resolveEmptyFinal(msgs); err == nil {
			t.Fatal("expected error (no thinking-only turn), got nil")
		}
	})

	t.Run("no reasoning fails loudly", func(t *testing.T) {
		msgs := []config.Message{
			{ID: "msg_1", Role: config.RoleAssistant},
			{ID: "msg_2", Role: config.RoleUser, Text: "nudge"},
		}
		if _, err := resolveEmptyFinal(msgs); err == nil {
			t.Fatal("expected error, got nil")
		}
	})

	t.Run("no messages fails loudly", func(t *testing.T) {
		if _, err := resolveEmptyFinal(nil); err == nil {
			t.Fatal("expected error, got nil")
		}
	})
}
