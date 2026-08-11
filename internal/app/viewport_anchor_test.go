package app

import (
	"strings"
	"testing"
)

func TestRenderInvalidationClearsMessageRanges(t *testing.T) {
	u := &UISession{
		renderedMessages: []string{"one", "two"},
		messageRanges: []MessageLineRange{
			{ID: "msg-1", Start: 0, End: 1},
			{ID: "msg-2", Start: 1, End: 2},
		},
	}

	u.invalidateRenderFrom(1)

	if u.messageRanges != nil {
		t.Fatalf("message ranges survived render invalidation: %#v", u.messageRanges)
	}
}

func TestViewportAnchorRestoresMessageScreenRow(t *testing.T) {
	m := newCompletionTestModel(t, nil, nil)
	m.viewport.Height = 10
	m.viewport.SetContent(strings.Repeat("old\n", 100))
	m.viewport.SetYOffset(20)
	m.session.messageRanges = []MessageLineRange{
		{ID: "msg-1", Start: 0, End: 15},
		{ID: "msg-2", Start: 24, End: 35},
	}

	anchor := m.captureViewportAnchor()
	if !anchor.valid || anchor.messageID != "msg-2" || anchor.screenRow != 4 {
		t.Fatalf("anchor = %#v, want msg-2 at screen row 4", anchor)
	}

	m.viewport.SetContent(strings.Repeat("new\n", 200))
	m.session.messageRanges = []MessageLineRange{
		{ID: "msg-1", Start: 0, End: 50},
		{ID: "msg-2", Start: 70, End: 100},
	}
	m.restoreViewportAnchor(anchor, 20)

	if m.viewport.YOffset != 66 {
		t.Fatalf("viewport offset = %d, want 66", m.viewport.YOffset)
	}
}
