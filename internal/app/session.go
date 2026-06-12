package app

import (
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"squid-os/internal/config"
	"squid-os/internal/log"
	"squid-os/internal/style"
	"squid-os/internal/ui"
	"squid-os/internal/ui/component"
	"squid-os/internal/util"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/lipgloss"
)

// sessionSavePrompt holds the save-name input state for the session save overlay.
type sessionSavePrompt struct {
	Name string
}

func newSessionSavePrompt(lastName string) sessionSavePrompt {
	return sessionSavePrompt{Name: lastName}
}

// RenderHeight returns the same height as the picker so viewport layout stays stable.
func (s sessionSavePrompt) RenderHeight() int {
	return component.PickerMaxItems + 4 // matches Picker.RenderHeight: 3 + PickerMaxItems + 1
}

func (s sessionSavePrompt) Render(width int) string {
	var b strings.Builder
	bg := lipgloss.Color(style.P.BgFooter)

	// Leading blank line
	b.WriteString(" \n")

	// Title
	b.WriteString(lipgloss.NewStyle().Background(bg).Render(style.HeadingStyle.Render("   Save Session")) + "\n")

	// Separator blank line
	b.WriteString(" \n")

	// Name prompt — each segment carries bg to prevent ANSI reset holes
	var nameB strings.Builder
	nameB.WriteString(lipgloss.NewStyle().Background(bg).Foreground(lipgloss.Color(style.P.TextMuted)).Render("   Name: "))
	nameB.WriteString(lipgloss.NewStyle().Background(bg).Foreground(lipgloss.Color(style.P.TextAccent)).Bold(true).Render(s.Name + "_"))
	b.WriteString(nameB.String() + "\n")

	// Pad remaining slots to match picker height (PickerMaxItems - 1 padding lines after name)
	for i := 0; i < component.PickerMaxItems-1; i++ {
		b.WriteString(" \n")
	}

	return lipgloss.NewStyle().
		Background(bg).
		Width(width).
		Render(b.String())
}

// openSaveSessionPrompt opens the save prompt so the user can confirm or edit the session name.
func (m Model) openSaveSessionPrompt() (Model, tea.Cmd) {
	if m.incognito {
		return m, nil // no saving in incognito
	}
	name := m.settings.LastSessionName
	if name == "" {
		name = time.Now().Format("2006-01-02_15-04")
	}
	m.sessionSave = newSessionSavePrompt(name)
	m.mode = ModeSessionSave
	m.textarea.Blur()
	(&m).recalcLayout()
	return m, nil
}

// saveAs persists the current session under the given name and updates LastSessionName.
// Pass silent=true to skip setting a notification (e.g. for background auto-saves).
func (m Model) saveAs(name string, silent bool) (Model, tea.Cmd) {
	if name == "" || m.incognito {
		return m, nil
	}
	m.session.file.TotalTokens = m.session.totalTokens()
	err := config.SaveSession(m.paths, name, m.session.file)
	if err != nil {
		if !silent {
			(&m).setNotification(ui.NotificationError, "couldn't save session")
		}
	} else {
		m.settings.LastSessionName = name
		_ = config.SaveSettings(m.paths, m.settings)
		if !silent {
			(&m).setNotification(ui.NotificationInfo, "session saved to "+config.SessionPath(m.paths, name))
		}
	}
	return m, nil
}

// autoSave persists silently after each assistant reply when AutoSave is enabled.
func (m Model) autoSave() (Model, tea.Cmd) {
	if !m.settings.AutoSave || m.incognito {
		return m, nil
	}
	name := m.settings.LastSessionName
	if name == "" {
		name = time.Now().Format("2006-01-02_15-04")
	}
	return m.saveAs(name, true)
}

// clearSession resets all messages and session state to start fresh.
func (m Model) clearSession() (Model, tea.Cmd) {
	m.session.clear(m.settings, m.paths, m.workingDir)
	if !m.incognito {
		m.settings.LastSessionName = ""
		_ = config.SaveSettings(m.paths, m.settings)
	}
	if m.settings.AutoSave {
		(&m).setNotification(ui.NotificationInfo, "new session started, will auto-save")
	} else {
		(&m).setNotification(ui.NotificationInfo, "new session started  ·  ctrl+s to save")
	}
	m.updateViewportContent()
	return m, m.setChatMode()
}

// handleSessionSaveKey handles key input while the save-name prompt overlay is active.
func (m Model) handleSessionSaveKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, keys.Escape), key.Matches(msg, keys.Cancel):
		return m, m.setChatMode()

	case key.Matches(msg, keys.Send):
		nm, _ := m.saveAs(m.sessionSave.Name, false)
		return nm, nm.setChatMode()

	default:
		s := msg.String()
		if s == "backspace" {
			if len(m.sessionSave.Name) > 0 {
				m.sessionSave.Name = m.sessionSave.Name[:len(m.sessionSave.Name)-1]
			}
		} else if len(s) == 1 {
			m.sessionSave.Name += s
		}
		return m, nil
	}
}

// toggleIncognito switches incognito mode on/off and resets the chat either way.
func (m Model) toggleIncognito() (Model, tea.Cmd) {
	m.incognito = !m.incognito
	m.session.clear(m.settings, m.paths, m.workingDir)
	if !m.incognito {
		// Leaving incognito: also reset last session name so auto-save doesn't
		// accidentally write to the previous session.
		m.settings.LastSessionName = ""
		_ = config.SaveSettings(m.paths, m.settings)
	}
	// Disable logging in incognito, re-enable if debug is on
	log.SetEnabled(m.settings.DebugEnabled && !m.incognito)
	if m.incognito {
		(&m).setNotification(ui.NotificationInfo, "incognito is on")
	} else {
		(&m).setNotification(ui.NotificationInfo, "incognito is off")
	}
	m.updateViewportContent()
	return m, m.setChatMode()
}

// openSessionPicker opens the session picker, snapshots current state so Esc can restore it,
// and immediately previews the first (or last-used) session.
func (m Model) openSessionPicker() (Model, tea.Cmd) {
	sessions := config.ListSessions(m.paths)
	if len(sessions) == 0 {
		return m, nil
	}

	// Snapshot current state so Esc can restore it
	snap := m.session
	m.sessionSnapshot = &snap

	items := make([]component.PickerItem, len(sessions))
	for i, s := range sessions {
		items[i] = component.PickerItem{
			Label: s.Name,
			Meta:  []string{util.FriendlyModDate(s.ModTime)},
			Value: s.Name,
		}
	}

	m.activePicker = component.Picker{
		Title:        "Load Session",
		Items:        items,
		DefaultValue: m.settings.LastSessionName,
		OnSelectionChange: func(idx int, item component.PickerItem, ctx any) {
			m := ctx.(*Model)
			name := item.Value
			if name == "" {
				return
			}
			sf, err := config.LoadSession(m.paths, name)
			if err != nil {
				return
			}
			m.session.setFrom(sf, false)
			if sf.Session.WorkingDir != "" {
				m.applyWorkingDir(sf.Session.WorkingDir)
			}
			m.updateViewportContent()
		},
		OnConfirm: func(item component.PickerItem, ctx any) tea.Cmd {
			m := ctx.(*Model)
			selected := item.Value
			if selected == "" {
				return m.setChatMode()
			}
			if !m.incognito {
				m.settings.LastSessionName = selected
				_ = config.SaveSettings(m.paths, m.settings)
			}
			sf, err := config.LoadSession(m.paths, selected)
			if err != nil {
				return m.setChatMode()
			}
			m.session.setFrom(sf)
			m.sessionSnapshot = nil
			m.setNotification(ui.NotificationInfo, "session loaded from "+config.SessionPath(m.paths, selected))
			cmd := m.setChatMode()
			m.updateViewportContent()
			return cmd
		},
		OnCancel: func(ctx any) tea.Cmd {
			m := ctx.(*Model)
			if m.sessionSnapshot != nil {
				m.session = *m.sessionSnapshot
				m.sessionSnapshot = nil
				if m.session.file.Session.WorkingDir != "" {
					m.applyWorkingDir(m.session.file.Session.WorkingDir)
				}
			}
			cmd := m.setChatMode()
			m.updateViewportContent()
			return cmd
		},
	}

	m.pickerContext = "session"
	m.mode = ModeComponentPicker
	(&m).recalcLayout()

	return m, nil
}


