package app

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"

	"squid-os/internal/chat/provider"
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
			m.textarea.SetValue(m.history.Entries[m.historyIdx].Text)
			m.autoSizeTextarea()
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
		m.autoSizeTextarea()
		return m, nil
	}

	// Otherwise, save draft and start browsing history
	m.draft = m.textarea.Value()
	m.historyIdx = len(m.history.Entries) - 1
	if m.historyIdx >= 0 {
		m.textarea.SetValue(m.history.Entries[m.historyIdx].Text)
		m.autoSizeTextarea()
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
			m.textarea.SetValue(m.history.Entries[m.historyIdx].Text)
			m.autoSizeTextarea()
		} else {
			// At end of history, restore draft
			m.textarea.SetValue(m.draft)
			m.draft = ""
			m.historyIdx = -1
			m.autoSizeTextarea()
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
	m.autoSizeTextarea()
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

// textareaVisualRows counts hard lines plus Bubbles' own soft-wrap height at
// the textarea's current content width. A temporary model avoids duplicating
// Bubbles' wrapping algorithm while keeping the dependency untouched.
func textareaVisualRows(value string, width int) int {
	if width < 1 {
		width = 1
	}
	probe := textarea.New()
	probe.ShowLineNumbers = false
	probe.SetWidth(width)

	rows := 0
	for _, line := range strings.Split(value, "\n") {
		probe.SetValue(line)
		height := probe.LineInfo().Height
		if height < 1 {
			height = 1
		}
		rows += height
	}
	return rows
}

// autoSizeTextarea adjusts the textarea height to its visual row count,
// including both hard newlines and soft wrapping, capped at MaxHeight (20).
func (m *Model) autoSizeTextarea() {
	rows := textareaVisualRows(m.textarea.Value(), m.textarea.Width())
	if rows < 2 {
		rows = 2
	}
	if rows > m.textarea.MaxHeight {
		rows = m.textarea.MaxHeight
	}
	m.textarea.SetHeight(rows)
	m.recalcLayout()
	m.updateViewportContent()
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
func (m *Model) refreshContextWindow(entries []provider.ModelEntry) {
	inference := m.session.EffectiveInference()
	for _, e := range entries {
		if e.ID == inference.Model && e.Provider == inference.Provider {
			if e.ContextLength != m.settings.ContextWindow {
				m.settings.ContextWindow = e.ContextLength
				_ = config.SaveSettings(m.paths, m.settings)
			}
			return
		}
	}
}
