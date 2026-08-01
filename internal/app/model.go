package app

import (
	"context"
	"fmt"
	"time"

	"squid-os/internal/chat/provider"
	"squid-os/internal/config"
	"squid-os/internal/modelcache"
	"squid-os/internal/ui"
	"squid-os/internal/ui/component"

	tea "github.com/charmbracelet/bubbletea"
)

// modelsLoadedMsg signals that model scanning completed.
type modelsLoadedMsg struct {
	models []provider.ModelEntry
}

type pendingToolResumeMsg struct {
	msgIdx int
}

// scanModelsCmd launches an async model scan and returns the result as a modelsLoadedMsg.
func (m Model) scanModelsCmd() tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		models := provider.ScanModels(ctx, m.endpoints)
		_ = (modelcache.Store{Dir: m.paths.CacheDir}).Save(m.endpoints, models)
		return modelsLoadedMsg{models: models}
	}
}

// openModelPicker starts a model scan if we don't have cached entries,
// or immediately shows the picker from cache.
func (m Model) openModelPicker() (Model, tea.Cmd) {
	return m, (&m).showModelPicker()
}

// showModelPicker shows the model picker directly or triggers a scan first.
func (m *Model) showModelPicker() tea.Cmd {
	if len(m.modelEntries) > 0 {
		m.buildModelPicker(m.modelEntries)
		return nil
	}
	return m.scanModelsCmd()
}

// buildModelPicker constructs the Picker from a model entry list and sets it on the model.
func (m *Model) buildModelPicker(entries []provider.ModelEntry) {
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
		DefaultValue: m.session.EffectiveInference().Model,
		OnConfirm: func(item component.PickerItem, ctx any) tea.Cmd {
			m := ctx.(*Model)
			modelID := item.Value
			if modelID == "" {
				return m.setChatMode()
			}

			// Check if this is a sentinel (NeedsConfig) entry
			var entry *provider.ModelEntry
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
			// Try to resolve context length if we don't have it yet
			contextLength := entry.ContextLength
			if contextLength == 0 && !entry.NeedsConfig {
				if p := provider.Lookup(entry.Provider, config.ResolveProviderSettings(m.endpoints, entry.Provider)); p != nil {
					ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
					detail := p.ModelDetails(ctx, entry.ID)
					cancel()
					if detail != nil && detail.ContextLength > 0 {
						contextLength = detail.ContextLength
					}
				}
			}

			current := m.session.EffectiveInference()
			next := config.InferenceConfig{Provider: entry.Provider, Model: entry.ID, Thinking: current.Thinking}
			m.session.SetPendingInference(next)
			m.settings.Model = entry.ID
			m.settings.Provider = entry.Provider
			m.settings.ContextWindow = contextLength
			m.session.invalidateRenderAll()
			_ = config.SaveSettings(m.paths, m.settings)
			m.setNotification(ui.NotificationInfo, "Model will switch on next turn: "+modelBasename(m.settings.Model))
			return m.setChatMode()
		},
		OnCancel: func(ctx any) tea.Cmd {
			m := ctx.(*Model)
			return m.setChatMode()
		},
	}
	m.pickerPayload = entries
	m.refreshContextWindow(entries)
	m.setComponent(&picker)
}

// onModelsLoaded handles the async scan result — builds the picker and caches entries.
func (m Model) onModelsLoaded(msg modelsLoadedMsg) Model {
	(&m).buildModelPicker(msg.models)
	return m
}
