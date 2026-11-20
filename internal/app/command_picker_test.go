package app

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"squid-os/internal/config"
	"squid-os/internal/ui/component"
)

// confirmPaletteCommand exercises the real picker key path, not its callback.
func confirmPaletteCommand(t *testing.T, m Model, name string) Model {
	t.Helper()
	m.allCommands = buildCommandPickerItems()
	m.textarea.SetValue("/")
	m.openCommandPicker()
	updated, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(name)})
	m = updated.(Model)
	picker := m.activeComponent.(*component.Picker)
	if picker.SelectedItem().Value != name {
		t.Fatalf("selected %q, want %q", picker.SelectedItem().Value, name)
	}
	updated, _ = m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	return updated.(Model)
}

func TestCommandPickerVoiceEnter(t *testing.T) {
	for _, streaming := range []bool{false, true} {
		t.Run(fmt.Sprintf("streaming=%v", streaming), func(t *testing.T) {
			m := newVoiceTestModel(t, false) // Settings writes are isolated in t.TempDir().
			m.settings.Model = "preserve-model"
			m.settings.NormalizeVoice()
			wantMode := ModeChat
			if streaming {
				wantMode = ModeStreaming
				m.mode = wantMode
				m.turnActive = true
				m.session.Stream.Active = true
				m.session.Stream.Text = "partial response"
				m.session.Stream.CancelFn = func(string) { t.Error("stream cancelled") }
				m.session.UIStream.ID = "active-stream"
				m.session.UIStream.TokenCount = 42
				m.textarea.Placeholder = "ctrl+c to cancel..."
			}
			m.session.BeginTurnCtx(nil)
			defer m.session.CancelTurnCtx()
			ctx := m.session.TurnContext()
			uiStream := m.session.UIStream
			for _, voiceWant := range []config.NarrationMode{config.NarrationStep, config.NarrationTurn} {
				m = confirmPaletteCommand(t, m, "voice")
				if m.mode != wantMode || m.activeComponent != nil || !m.textarea.Focused() || m.textarea.Value() != "" {
					t.Fatalf("palette not dismissed: mode=%v component=%T", m.mode, m.activeComponent)
				}
				saved := config.LoadSettings(m.paths.Root)
				if saved.Voice.Mode != voiceWant || m.settings.Voice.Mode != voiceWant {
					t.Fatal("voice toggle not persisted")
				}
				if saved.Model != "preserve-model" || saved.Voice.Voices["en"] != "jane" {
					t.Fatal("unrelated settings changed")
				}
				if ctx.Err() != nil || m.session.TurnContext() != ctx || !reflect.DeepEqual(uiStream, m.session.UIStream) {
					t.Fatal("stream reset or turn cancelled")
				}
				if streaming && (!m.turnActive || !m.session.Stream.Active || m.session.Stream.StreamCancelled || m.session.Stream.Text != "partial response" || m.session.Stream.CancelFn == nil || m.textarea.Placeholder != "ctrl+c to cancel...") {
					t.Fatal("active stream disturbed")
				}
			}
		})
	}
}

func TestCommandPickerThinkingEnter(t *testing.T) {
	m := newVoiceTestModel(t, false)
	before := m.session.EffectiveInference().Thinking.Enabled
	m = confirmPaletteCommand(t, m, "thinking")
	if m.mode != ModeChat || m.activeComponent != nil || !m.textarea.Focused() {
		t.Fatal("thinking did not dismiss palette")
	}
	if m.session.EffectiveInference().Thinking.Enabled == before || config.LoadSettings(m.paths.Root).Thinking.Enabled == before {
		t.Fatal("thinking toggle not persisted")
	}
}

func TestCommandPickerBlockedDuringStream(t *testing.T) {
	for _, command := range AllCommands {
		if !command.BlockDuringStream {
			continue
		}
		t.Run(command.Name, func(t *testing.T) {
			m := newVoiceTestModel(t, false)
			m.mode = ModeStreaming
			m.turnActive = true
			m.session.Stream.Active = true
			m.session.UIStream.ID = "active-stream"
			session := m.session
			m = confirmPaletteCommand(t, m, command.Name)
			if m.mode != ModeStreaming || m.activeComponent != nil || !m.turnActive || m.session != session || !m.session.Stream.Active || m.session.UIStream.ID != "active-stream" {
				t.Fatal("blocked command disturbed stream or left palette open")
			}
			if !strings.Contains(m.notification.Message, "/"+command.Name+" will be available when assistant finishes") {
				t.Fatalf("missing blocked notification: %q", m.notification.Message)
			}
		})
	}
}

func TestCommandPickerOpensReplacementPicker(t *testing.T) {
	m := newVoiceTestModel(t, false)
	m = confirmPaletteCommand(t, m, "skill")
	picker, ok := m.activeComponent.(*component.Picker)
	if !ok || m.mode != ModeComponent || picker.Title != "Select Skill" {
		t.Fatalf("replacement picker lost: mode=%v component=%T", m.mode, m.activeComponent)
	}
}

func TestCommandPickerCancelPreservesStream(t *testing.T) {
	m := newVoiceTestModel(t, false)
	m.mode = ModeStreaming
	m.turnActive = true
	m.session.Stream.Active = true
	m.allCommands = buildCommandPickerItems()
	m.openCommandPicker()
	updated, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyEsc})
	m = updated.(Model)
	if m.mode != ModeStreaming || m.activeComponent != nil || !m.turnActive || !m.session.Stream.Active {
		t.Fatal("palette cancellation ended active turn")
	}
}
