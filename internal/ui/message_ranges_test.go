package ui

import (
	"strings"
	"testing"

	"squid-os/internal/config"
)

func TestMetadataMessagesHaveClickableRanges(t *testing.T) {
	for _, role := range []string{config.RoleSystem, config.RoleInternal, config.RoleSynthetic} {
		t.Run(role, func(t *testing.T) {
			msg := config.Message{ID: "m1", Role: role, Label: role, Text: "details"}
			rendered, ranges := RenderMessage(msg, 100, NewExpandTracker(false), nil)
			if len(ranges) != 1 {
				t.Fatalf("got %d ranges, want 1", len(ranges))
			}
			if ranges[0].Key != msg.ID || ranges[0].Start != 0 || ranges[0].End != strings.Count(rendered, "\n") {
				t.Fatalf("range = %#v, rendered lines = %d", ranges[0], strings.Count(rendered, "\n"))
			}
		})
	}
}
