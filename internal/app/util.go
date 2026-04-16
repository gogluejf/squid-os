package app

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// historyUp moves the prompt history cursor back one entry, saving the current draft first.
func (m Model) historyUp() (Model, tea.Cmd) {
	if len(m.history.Entries) == 0 {
		return m, nil
	}
	if m.historyIdx == -1 {
		m.draft = m.textarea.Value()
		m.historyIdx = len(m.history.Entries) - 1
	} else if m.historyIdx > 0 {
		m.historyIdx--
	}
	m.textarea.SetValue(m.history.Entries[m.historyIdx])
	return m, nil
}

// historyDown moves the prompt history cursor forward, restoring the draft when reaching the end.
func (m Model) historyDown() (Model, tea.Cmd) {
	if m.historyIdx == -1 {
		return m, nil
	}
	if m.historyIdx < len(m.history.Entries)-1 {
		m.historyIdx++
		m.textarea.SetValue(m.history.Entries[m.historyIdx])
	} else {
		m.historyIdx = -1
		m.textarea.SetValue(m.draft)
	}
	return m, nil
}

// calcTokPerSec returns the current tokens-per-second rate since the first token arrived.
func (m Model) calcTokPerSec() float64 {
	if m.stream.firstTokenTime.IsZero() || (m.stream.outputTokenCount == 0 && m.stream.thinkingTokenCount == 0) {
		return 0
	}
	elapsed := time.Since(m.stream.firstTokenTime).Seconds()
	if elapsed <= 0 {
		return 0
	}
	return float64(m.stream.outputTokenCount+m.stream.thinkingTokenCount) / elapsed
}

// countTokensApprox estimates token count as roughly one token per four characters.
func countTokensApprox(s string) int {
	n := len(s) / 4
	if n == 0 && len(s) > 0 {
		n = 1
	}
	return n
}

// SetAttachedImage sets the image to attach to the next message (from --image flag).
func (m *Model) SetAttachedImage(path string) {
	m.attachedImage = path
}

// SetInitialPrompt sets the textarea content (from --prompt flag).
func (m *Model) SetInitialPrompt(text string) {
	m.textarea.SetValue(text)
}
