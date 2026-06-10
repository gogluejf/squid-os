package app

import (
	"context"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"squid-os/internal/chat"
	"squid-os/internal/config"
	"squid-os/internal/ui"
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
	items := make([]ui.PickerItem, len(entries))
	for i, e := range entries {
		name := modelBasename(e.ID)
		ctxLabel := ""
		if e.ContextLength > 0 {
			ctxLabel = formatContextLength(e.ContextLength)
		}
		items[i] = ui.PickerItem{
			Label: name,
			Meta:  ctxLabel,
			Value: e.ID,
		}
	}

	m.modelEntries = entries
	m.activePicker = ui.Picker{
		Title:       "Select Model",
		Items:       items,
		DisplayMode: ui.ModeLabelValue,
	}
	m.pickerContext = "model"
	m.pickerPayload = entries
	(&m).refreshContextWindow(entries)
	m.mode = ModeModelPicker
	(&m).recalcLayout()
	return m
}

// onModelsLoaded handles the async scan result — builds the picker and caches entries.
func (m Model) onModelsLoaded(msg modelsLoadedMsg) Model {
	return m.buildModelPicker(msg.models)
}

// confirmModelPicker applies a model selection using the PickerItem.Value as the model ID.
func (m Model) confirmModelPicker(item ui.PickerItem) Model {
	modelID := item.Value
	if modelID == "" {
		return m
	}
	entries := m.pickerPayload.([]chat.ModelEntry)
	var entry *chat.ModelEntry
	for i := range entries {
		if entries[i].ID == modelID {
			entry = &entries[i]
			break
		}
	}
	if entry == nil {
		return m
	}
	name := modelBasename(entry.ID)
	if m.settings.Model != entry.ID {
		oldModel := modelBasename(m.settings.Model)
		(&m).session.pushModelSwitchMsg(oldModel, name)
	}
	(&m).session.updateConfigMsg(entry.Provider, entry.ID, m.settings.Thinking)
	m.settings.Model = entry.ID
	m.settings.Provider = entry.Provider
	m.settings.ContextWindow = entry.ContextLength
	(&m).session.invalidateRenderAll()
	_ = config.SaveSettings(m.paths, m.settings)
	(&m).setNotification(ui.NotificationInfo, "switched to model: "+modelBasename(m.settings.Model))
	return m
}
