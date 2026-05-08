package app

import (
	"fmt"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"squid-os/internal/chat"
	"squid-os/internal/config"
)

// modelBasename strips any path prefix from a model ID, e.g.
// "Lorbus/Qwen3.6-27B-int4-AutoRound" -> "Qwen3.6-27B-int4-AutoRound".
func modelBasename(id string) string {
	if idx := strings.LastIndex(id, "/"); idx >= 0 {
		return id[idx+1:]
	}
	if idx := strings.LastIndex(id, "\\"); idx >= 0 {
		return id[idx+1:]
	}
	return filepath.Base(id)
}

// historyUp moves the prompt history cursor back one entry.
// New behavior:
//   - If not in history mode:
//   - If textarea is empty and draft exists → load draft, clear draft
//   - If textarea has text → save draft, start browsing history (go to last entry)
//   - If in history mode → go to previous entry
func (m Model) historyUp() (Model, tea.Cmd) {
	// If in history mode, navigate through history entries
	if m.historyIdx != -1 {
		if m.historyIdx > 0 {
			m.historyIdx--
			m.textarea.SetValue(m.history.Entries[m.historyIdx])
		}
		return m, nil
	}

	// Not in history mode yet
	if len(m.history.Entries) == 0 {
		return m, nil
	}

	// If textarea is empty and draft exists, load draft
	if m.textarea.Value() == "" && m.draft != "" {
		m.textarea.SetValue(m.draft)
		m.draft = ""
		return m, nil
	}

	// Otherwise, save draft and start browsing history
	m.draft = m.textarea.Value()
	m.historyIdx = len(m.history.Entries) - 1
	if m.historyIdx >= 0 {
		m.textarea.SetValue(m.history.Entries[m.historyIdx])
	}
	return m, nil
}

// historyDown moves the prompt history cursor forward.
// New behavior:
//   - If not in history mode:
//   - Save draft, clear textarea
//   - If in history mode → go to next entry, restore draft at end
func (m Model) historyDown() (Model, tea.Cmd) {
	// If in history mode, navigate through history entries
	if m.historyIdx != -1 {
		if m.historyIdx < len(m.history.Entries)-1 {
			m.historyIdx++
			m.textarea.SetValue(m.history.Entries[m.historyIdx])
		} else {
			// At end of history, restore draft
			m.textarea.SetValue(m.draft)
			m.draft = ""
			m.historyIdx = -1
		}
		return m, nil
	}

	// Not in history mode yet
	if m.textarea.Value() == "" {
		// Nothing to save, don't touch the draft
		return m, nil
	}
	m.draft = m.textarea.Value()
	m.textarea.SetValue("")
	return m, nil
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

// formatContextLength returns a human-readable context window label (e.g. "128k", "32k").
func formatContextLength(ctxLen int) string {
	if ctxLen == 0 {
		return ""
	}
	if ctxLen >= 1000 {
		k := ctxLen / 1000
		// Round to nearest for nice display
		rem := ctxLen % 1000
		if rem >= 500 {
			k++
		}
		return fmt.Sprintf("%dk", k)
	}
	return fmt.Sprintf("%d", ctxLen)
}

// refreshContextWindow looks up the current model in the entries and updates
// settings.ContextWindow, then persists to disk.
func (m *Model) refreshContextWindow(entries []chat.ModelEntry) {
	for _, e := range entries {
		if e.ID == m.settings.Model && e.Provider == m.settings.Provider {
			if e.ContextLength != m.settings.ContextWindow {
				m.settings.ContextWindow = e.ContextLength
				_ = config.SaveSettings(m.paths, m.settings)
			}
			return
		}
	}
}


