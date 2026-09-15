package app

import (
	"testing"

	"squid-os/internal/ui"
)

func TestRenderInvalidationKeepsExpandOverrides(t *testing.T) {
	u := &UISession{expand: ui.NewExpandTracker(false)}
	u.expand.Toggle("m1", "tool:t1")

	u.invalidateRenderAll()

	if !u.expand.IsOpen("m1", "tool:t1") {
		t.Fatal("render invalidation cleared expansion state")
	}
}
