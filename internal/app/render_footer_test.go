package app

import (
	"testing"

	"squid-os/internal/chat"
	"squid-os/internal/config"
)

// TestBuildFooterDataNilTokenTallySafe verifies that buildFooterData does not
// panic when SessionDoc.TokenTally is nil (defensive nil-safety).
// Normal flow always keeps TokenTally non-nil, but this guards against edge cases.
func TestBuildFooterDataNilTokenTallySafe(t *testing.T) {
	// Build a minimal Model with a session whose TokenTally is nil.
	// We don't need a full Model — just need to verify no panic.
	sess := &chat.Session{
		Doc: config.SessionDoc{
			Version:    2,
			TokenTally: nil, // deliberately nil
		},
	}
	m := Model{
		session: &UISession{Session: sess},
		ready:   true,
		width:   80,
	}

	// Should not panic — nil-safe fallback to zero tally
	_ = m.buildFooterData()
}
