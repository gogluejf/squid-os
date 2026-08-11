package ui

import (
	"strings"
	"testing"
)

// newFooterData returns a FooterData with reasonable defaults for testing.
// Callers can override specific fields.
func newFooterData() FooterData {
	return FooterData{
		Model:               "gpt-4o",
		Provider:            "openai",
		ContextInputTokens:  1500,
		ContextOutputTokens: 500,
		ContextTotalTokens:  2000,
		SavedContextTokens:  0,
		ContextCompaction:   true,
		ContextWindow:       8192,
		AuthorizationMode:   "auto",
		WorkingDir:          "/home/user/project",
	}
}

func TestFooterEnabledWithSavings(t *testing.T) {
	data := newFooterData()
	data.ContextInputTokens = 2400
	data.ContextOutputTokens = 600
	data.ContextTotalTokens = 3000
	data.SavedContextTokens = 2000

	out := RenderFooter(data, 160)
	lines := strings.Split(out, "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 lines, got %d", len(lines))
	}

	// Savings should be displayed when nonzero
	if !strings.Contains(out, "saved") {
		t.Error("expected 'saved' label in footer when savings > 0")
	}
	if !strings.Contains(out, "2.0k") {
		t.Error("expected '2.0k' (formatted 2000 tokens) in savings display")
	}

	// Context bar uses compacted tokens (3000/8192 = 36.6%)
	if !strings.Contains(out, "36.6%") {
		t.Errorf("expected '36.6%%' in context bar for compacted=3000, window=8192, got:\n%s", out)
	}
}

func TestFooterDisabledSuppressesSavingsButUsesPotentialCompactedBar(t *testing.T) {
	data := newFooterData()
	data.ContextCompaction = false
	data.ContextInputTokens = 2400
	data.ContextOutputTokens = 600
	data.ContextTotalTokens = 3000
	data.SavedContextTokens = 2000

	out := RenderFooter(data, 160)

	if strings.Contains(out, "saved") {
		t.Error("expected no 'saved' label when compaction is disabled")
	}
	if !strings.Contains(out, "36.6%") {
		t.Errorf("expected potential compacted usage '36.6%%', got:\n%s", out)
	}
}

func TestFooterZeroContextWindow(t *testing.T) {
	data := newFooterData()
	data.ContextWindow = 0
	data.ContextTotalTokens = 1500

	out := RenderFooter(data, 160)

	// No context bar or "/window" suffix when window is 0
	if strings.Contains(out, "%") {
		t.Error("expected no percentage when context window is 0")
	}
}

func TestFooterOverWindow(t *testing.T) {
	data := newFooterData()
	data.ContextTotalTokens = 10000
	data.ContextWindow = 8192

	out := RenderFooter(data, 160)

	// Capped at 100%
	if !strings.Contains(out, "100.0%") {
		t.Errorf("expected '100.0%%' when compacted > window, got:\n%s", out)
	}
}

func TestFooterNarrow(t *testing.T) {
	data := newFooterData()
	data.ContextInputTokens = 2400
	data.ContextOutputTokens = 600
	data.ContextTotalTokens = 3000
	data.SavedContextTokens = 2000

	// Narrow width (< 80): no context bar, no provider prefix, shortened labels
	out := RenderFooter(data, 60)

	// No context bar on narrow footers (< 120)
	if strings.Contains(out, "%") {
		t.Error("expected no context bar on narrow footer (width=60)")
	}

	// No provider prefix on narrow footers (< 120)
	if strings.Contains(out, "openai/gpt-4o") {
		t.Error("expected no provider prefix on narrow footer")
	}

	// Should still have 2 lines
	lines := strings.Split(out, "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 lines on narrow footer, got %d", len(lines))
	}
}

func TestFooterWide(t *testing.T) {
	data := newFooterData()
	data.ContextInputTokens = 2400
	data.ContextOutputTokens = 600
	data.ContextTotalTokens = 3000
	data.SavedContextTokens = 2000
	data.ContextWindow = 8192

	out := RenderFooter(data, 160)

	// Wide footer: provider prefix, context bar, savings
	if !strings.Contains(out, "openai/") {
		t.Error("expected provider prefix on wide footer")
	}
	if !strings.Contains(out, "36.6%") {
		t.Errorf("expected context bar on wide footer, got:\n%s", out)
	}
	if !strings.Contains(out, "saved") {
		t.Error("expected savings on wide footer")
	}
}

func TestFooterStreaming(t *testing.T) {
	data := newFooterData()
	data.Streaming = true
	data.StreamingOutputTokens = 150
	data.TokPerSec = 42.5
	data.SeqDurMs = 3500

	out := RenderFooter(data, 160)

	// Streaming indicators
	if !strings.Contains(out, "3.5sec") {
		t.Error("expected duration '3.5sec' during streaming")
	}
	if !strings.Contains(out, "42.5 tok/s") {
		t.Error("expected '42.5 tok/s' during streaming")
	}

	// Output tokens include streaming overlay
	// Context output (500) + streaming (150) = 650
	if !strings.Contains(out, "↓650") {
		t.Errorf("expected '↓650' (context output + stream overlay), got:\n%s", out)
	}
}

func TestFooterStreamingSeamlessContextOverlayAndPercentage(t *testing.T) {
	data := newFooterData()
	data.ContextInputTokens = 700
	data.ContextOutputTokens = 300
	data.ContextTotalTokens = 1000
	data.Streaming = true
	data.StreamingOutputTokens = 250
	data.ContextWindow = 2000

	out := RenderFooter(data, 160)

	if !strings.Contains(out, "↓550↑700") {
		t.Errorf("expected output overlay with unchanged input, got:\n%s", out)
	}
	if !strings.Contains(out, "[1.2k/2.0k]") {
		t.Errorf("expected displayed total to include live output, got:\n%s", out)
	}
	if !strings.Contains(out, "62.5%") {
		t.Errorf("expected percentage from displayed total/window, got:\n%s", out)
	}
}

func TestFooterStreamingNoSavings(t *testing.T) {
	data := newFooterData()
	data.Streaming = true
	data.StreamingOutputTokens = 50
	data.TokPerSec = 30.0
	data.SeqDurMs = 1000
	data.SavedContextTokens = 0

	out := RenderFooter(data, 160)

	// No savings when zero even during streaming
	if strings.Contains(out, "saved") {
		t.Error("expected no 'saved' label when savings == 0, even during streaming")
	}
}

func TestFooterZeroTokens(t *testing.T) {
	data := FooterData{
		Model:               "test-model",
		ContextInputTokens:  0,
		ContextOutputTokens: 0,
		ContextTotalTokens:  0,
		SavedContextTokens:  0,
		ContextWindow:       4096,
		AuthorizationMode:   "auto",
	}

	out := RenderFooter(data, 120)

	// Should render cleanly with zero tokens
	lines := strings.Split(out, "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 lines, got %d", len(lines))
	}

	// 0% context usage
	if !strings.Contains(out, "0.0%") {
		t.Errorf("expected '0.0%%' with zero tokens, got:\n%s", out)
	}
}

func TestFooterExactWidth(t *testing.T) {
	data := newFooterData()

	for _, w := range []int{40, 80, 100, 120, 160, 200} {
		out := RenderFooter(data, w)

		// The footer is exactly 2 logical lines. lipgloss.Width pads/truncates each
		// line to `w`, but very narrow widths may cause wrapping in the raw string.
		// We verify the output is non-empty and each rendered line has content.
		lines := strings.Split(out, "\n")
		if len(lines) < 2 {
			t.Errorf("width %d: expected at least 2 lines, got %d", w, len(lines))
		}
		// Check that no line is completely empty (all whitespace)
		for i, line := range lines {
			trimmed := strings.TrimSpace(line)
			if trimmed == "" {
				t.Errorf("width %d: line %d is empty", w, i)
			}
		}
	}
}

func TestFooterStreamingOverlaysContextOutput(t *testing.T) {
	data := newFooterData()
	data.ContextOutputTokens = 1000
	data.Streaming = true
	data.StreamingOutputTokens = 250

	out := RenderFooter(data, 160)

	// Output should be 1250 = 1000 + 250
	if !strings.Contains(out, "↓1.2k") {
		t.Errorf("expected '↓1.2k' (1000 + 250 = 1250), got:\n%s", out)
	}
}

func TestFooterSavingsDisplayedOnlyWhenNonzero(t *testing.T) {
	// Zero savings — no display
	data := newFooterData()
	data.SavedContextTokens = 0
	out := RenderFooter(data, 160)
	if strings.Contains(out, "saved") {
		t.Error("expected no 'saved' when savings == 0")
	}

	// Nonzero savings with compaction enabled — display
	data.ContextCompaction = true
	data.SavedContextTokens = 100
	out = RenderFooter(data, 160)
	if !strings.Contains(out, "saved") {
		t.Error("expected 'saved' when savings > 0")
	}
}

func TestFooterCompactedDrivesContextBar(t *testing.T) {
	// Verify the context bar percentage is based on compacted, not raw.
	data := newFooterData()
	data.ContextInputTokens = 3000
	data.ContextOutputTokens = 1096
	data.ContextTotalTokens = 4096 // exactly 50% of 8192
	data.SavedContextTokens = 3904
	data.ContextWindow = 8192

	out := RenderFooter(data, 160)

	if !strings.Contains(out, "50.0%") {
		t.Errorf("expected '50.0%%' (compacted 4096 / window 8192), got:\n%s", out)
	}

	// Raw (8000/8192 = 97.7%) should NOT appear
	if strings.Contains(out, "97.7%") {
		t.Error("context bar should use compacted tokens, not raw")
	}
}

func TestFooterCompactDataFields(t *testing.T) {
	// Verify that FooterData fields map correctly to the new structure
	data := FooterData{
		Model:               "test",
		ContextInputTokens:  300,
		ContextOutputTokens: 100,
		ContextTotalTokens:  400,
		SavedContextTokens:  100,
		ContextCompaction:   true,
		ContextWindow:       1000,
		AuthorizationMode:   "auto",
	}

	out := RenderFooter(data, 120)

	// Verify compacted input tokens (300)
	if !strings.Contains(out, "300") {
		t.Errorf("expected '300' (compacted input), got:\n%s", out)
	}

	// Verify compacted drives bar (400/1000 = 40.0%)
	if !strings.Contains(out, "40.0%") {
		t.Errorf("expected '40.0%%' (compacted 400 / window 1000), got:\n%s", out)
	}
}
