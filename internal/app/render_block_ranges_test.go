package app

import (
	"testing"

	"squid-os/internal/ui"
)

func TestTranslateBlockRangesStartsAfterAssistantHeader(t *testing.T) {
	got := translateBlockRanges("assistant", 12, []ui.BlockRange{
		{Key: "thinking", Start: 0, End: 2},
		{Key: "tool:call_1", Start: 5, End: 8},
	})

	want := []BlockLineRange{
		{Key: "assistant\x00thinking", Start: 12, End: 14},
		{Key: "assistant\x00tool:call_1", Start: 17, End: 20},
	}
	if len(got) != len(want) {
		t.Fatalf("got %d ranges, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("range %d = %#v, want %#v", i, got[i], want[i])
		}
	}
}
