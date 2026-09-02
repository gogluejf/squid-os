package app

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"squid-os/internal/chat"
	"squid-os/internal/chat/provider"
)

// streamEventMsg wraps a StreamEvent for the Bubble Tea message loop
type streamEventMsg chat.StreamEvent

// uiTickMsg fires periodically to keep the UI fresh.
// While streaming: 200ms (live timer animation).
// When idle: 2s (footer git shortstat refresh).
type uiTickMsg struct{}

// uiTickCmd schedules the next tick at the appropriate interval.
func uiTickCmd(streaming bool) tea.Cmd {
	interval := 2 * time.Second
	if streaming {
		interval = 200 * time.Millisecond
	}
	return tea.Tick(interval, func(_ time.Time) tea.Msg {
		return uiTickMsg{}
	})
}

// contextRefreshMsg silently updates the context window from a background scan
type contextRefreshMsg struct {
	models []provider.ModelEntry
}

// waitForStreamEvent blocks on the stream channel and returns the next event as a Tea message.
func waitForStreamEvent(ch <-chan chat.StreamEvent) tea.Cmd {
	return func() tea.Msg {
		event, ok := <-ch
		if !ok {
			return streamEventMsg(chat.StreamEvent{Done: true})
		}
		return streamEventMsg(event)
	}
}
