package app

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"rig-chat/internal/chat"
)

// streamEventMsg wraps a StreamEvent for the Bubble Tea message loop
type streamEventMsg chat.StreamEvent

// streamTickMsg fires periodically while streaming to keep the live timer
// in the message header animated even when no tokens are arriving yet.
type streamTickMsg struct{}

// streamTickCmd schedules the next tick while streaming is active.
func streamTickCmd() tea.Cmd {
	return tea.Tick(20*time.Millisecond, func(_ time.Time) tea.Msg {
		return streamTickMsg{}
	})
}

// modelsLoadedMsg signals that model scanning completed
type modelsLoadedMsg struct {
	models []chat.ModelEntry
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
