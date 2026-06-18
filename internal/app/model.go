package app

import (
	"context"
	"fmt"
	"time"

	"squid-os/internal/chat"
	"squid-os/internal/config"
	"squid-os/internal/ui"
	"squid-os/internal/ui/component"

	tea "github.com/charmbracelet/bubbletea"
)

// modelsLoadedMsg signals that model scanning completed.
type modelsLoadedMsg struct {
	models []chat.ModelEntry
}

// scanModelsCmd launches an async model scan and returns the result as a modelsLoadedMsg.
func (m Model) scanModelsCmd() tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		models := chat.ScanModels(ctx, m.endpoints)
		return modelsLoadedMsg{models: models}
	}
}

// openModelPicker starts a model scan if we don't have cached entries,
// or immediately shows the picker from cache.
func (m Model) openModelPicker() (Model, tea.Cmd) {
	if len(m.modelEntries) > 0 {
		m = m.buildModelPicker(m.modelEntries)
		return m, nil
	}
	return m, m.scanModelsCmd()
}

// buildModelPicker constructs the Picker from a model entry list.
func (m Model) buildModelPicker(entries []chat.ModelEntry) Model {
	items := make([]component.PickerItem, 0, len(entries))
	for _, e := range entries {
		if e.NeedsConfig {
			// Sentinel entry: show as "provider: action"
			action := "configure"
			if e.ID == "<auth expired>" {
				action = "re-authenticate"
			} else if e.ID == "<unreachable>" {
				action = "fix connection"
			}
			items = append(items, component.PickerItem{
				Label: fmt.Sprintf("%s: %s", e.Provider, action),
				Meta:  []string{e.ID},
				Value: e.Provider, // use provider name as value for sentinels
			})
		} else {
			name := modelBasename(e.ID)
			ctxLabel := ""
			if e.ContextLength > 0 {
				ctxLabel = formatContextLength(e.ContextLength)
			}
			items = append(items, component.PickerItem{
				Label: name,
				Meta:  []string{e.Provider, ctxLabel},
				Value: e.ID,
			})
		}
	}

	m.modelEntries = entries
	picker := component.Picker{
		Title:        "Select Model",
		Items:        items,
		DefaultValue: m.settings.Model,
		OnConfirm: func(item component.PickerItem, ctx any) tea.Cmd {
			m := ctx.(*Model)
			modelID := item.Value
			if modelID == "" {
				return m.setChatMode()
			}

			// Check if this is a sentinel (NeedsConfig) entry
			var entry *chat.ModelEntry
			for i := range entries {
				if entries[i].ID == modelID || (entries[i].NeedsConfig && entries[i].Provider == modelID) {
					entry = &entries[i]
					break
				}
			}

			if entry == nil {
				return m.setChatMode()
			}

			// Sentinel: trigger config wizard
			if entry.NeedsConfig {
				p := config.ResolveProviderSettings(m.endpoints, entry.Provider)
				if p == nil {
					// No settings entry yet — create one for the wizard
					p = &config.ProviderSettings{Name: entry.Provider}
				}
				return m.showProviderConfig(p)
			}

			// Normal model selection
			name := modelBasename(entry.ID)
			if m.settings.Model != entry.ID {
				oldModel := modelBasename(m.settings.Model)
				m.session.pushModelSwitchMsg(oldModel, name)
			}
			m.session.updateConfigMsg(entry.Provider, entry.ID, m.settings.Thinking)
			m.settings.Model = entry.ID
			m.settings.Provider = entry.Provider
			m.settings.ContextWindow = entry.ContextLength
			m.session.invalidateRenderAll()
			_ = config.SaveSettings(m.paths, m.settings)
			m.setNotification(ui.NotificationInfo, "switched to model: "+modelBasename(m.settings.Model))
			return m.setChatMode()
		},
		OnCancel: func(ctx any) tea.Cmd {
			m := ctx.(*Model)
			return m.setChatMode()
		},
	}
	m.pickerPayload = entries
	(&m).refreshContextWindow(entries)
	(&m).setComponent(&picker)
	return m
}

// onModelsLoaded handles the async scan result — builds the picker and caches entries.
func (m Model) onModelsLoaded(msg modelsLoadedMsg) Model {
	return m.buildModelPicker(msg.models)
}
