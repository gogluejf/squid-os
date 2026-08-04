# EPIC: Refactor Picker into Reusable Component
Why: Picker logic is duplicated across 4+ sites (model, skill, session, file) with parallel raw data arrays, giant switch statements in handlePickerKey/confirmPicker, and pre-formatted display strings. This makes adding new pickers painful and evolution risky.
Outcomes: Single Picker component with typed items, self-contained key handling, configurable column layouts, and clean caller integration. Reduces picker-related code by ~60%.

## MILESTONE: 1 - Picker Component
Pattern: Component Extraction
Objective: Build a generic Picker component in the ui package with typed items, self-contained key handling, configurable display modes, and selection callbacks
Success: Picker type in ui/command.go replaces PickerList with full key handling, typed items, multi-column rendering, and selection-change callbacks. All existing unit tests or behavioral expectations remain intact.
Diagram: classDiagram
    class Picker {
        +Title string
        +Items []PickerItem
        +Filter string
        +Selected int
        +DefaultMatch string
        +Mode PickerDisplayMode
        +OnSelectChanged func(int, PickerItem)
        +FilteredItems() []PickerItem
        +SelectedItem() PickerItem
        +HandleKey(KeyMsg) PickerAction
        +Render(width) string
        +RenderHeight() int
        +SetDefaultSelected(string)
    }
    class PickerItem {
        +Label string
        +Value string
        +Meta string
    }
    class PickerAction {
        @@enum@@
        ActionNone
        ActionSelect
        ActionCancel
    }
    class PickerDisplayMode {
        @@enum@@
        ModeSingleCol
        ModeLabelMeta
        ModeLabelValue
        ModeLabelDesc
    }
    Picker --> PickerItem
    Picker --> PickerAction
    Picker --> PickerDisplayMode

### TASK: 1.1 - Picker types and core logic
Type: feature
What: Add Picker, PickerItem, PickerAction, PickerDisplayMode types and core methods (FilteredItems, SelectedItem, HandleKey, RenderHeight) to ui/command.go
Why: Establishes the reusable component interface that replaces PickerList across all picker sites.
Files: ~ internal/ui/command.go
Snippet: type PickerAction int\nconst (\n\tActionNone   PickerAction = iota\n\tActionSelect               // user pressed Enter\n\tActionCancel               // user pressed Esc\n)\n\ntype PickerDisplayMode int\nconst (\n\tModeSingleCol   PickerDisplayMode = iota // just Label\n\tModeLabelMeta                           // Label + Meta (e.g. name + date)\n\tModeLabelDesc                           // Label + Description (e.g. skill + desc)\n\tModeLabelValue                          // Label + Value (e.g. provider + model ID)\n)\n\ntype PickerItem struct {\n\tLabel       string // left column\n\tMeta        string // right column short (date, context length, etc.)\n\tDescription string // extended description (truncated in display)\n\tValue       string // internal value used for matching/default selection\n}\n\ntype Picker struct {\n\tTitle          string\n\tItems          []PickerItem\n\tFilter         string\n\tSelected       int\n\tDefaultMatch   string\n\tDisplayMode    PickerDisplayMode\n\tOnSelectionChange func(int, PickerItem)\n}\n\nfunc (p *Picker) FilteredItems() []PickerItem { ... }\nfunc (p *Picker) SelectedItem() PickerItem { ... }\nfunc (p *Picker) HandleKey(msg tea.KeyMsg) PickerAction { ... }\nfunc (p *Picker) RenderHeight() int { ... }\nfunc (p *Picker) SetDefaultSelected(match string) { ... }
Acceptance: PickerAction enum has 3 values: None, Select, Cancel
Acceptance: PickerDisplayMode enum has 4 values for column layouts
Acceptance: FilteredItems returns items matching filter on Label and Meta fields (case-insensitive)
Acceptance: HandleKey returns ActionSelect on Enter, ActionCancel on Esc, handles Up/Down/Tab navigation, and filters on single-char input with backspace support
Acceptance: HandleKey calls OnSelectionChange callback when selection changes
Acceptance: SetDefaultSelected scans items and sets Selected index when Value or Label matches
Acceptance: SelectedItem returns PickerItem (not just string) with all fields intact
Verification: cd ~/src/squid-os && go build ./...

### TASK: 1.2 - Picker rendering with column modes
Type: feature
What: Implement Picker.Render(width) with support for all 4 display modes, selection highlighting, windowing, and filter display
Why: Replaces per-site fmt.Sprintf two-column formatting with a unified renderer that handles column widths dynamically.
Files: ~ internal/ui/command.go
Snippet: func (p *Picker) Render(width int) string {\n    items := p.FilteredItems()\n    // Calculate column width based on mode\n    // ModeSingleCol: full width\n    // ModeLabelMeta: find max Label len, Meta gets remainder\n    // ModeLabelDesc: find max Label len, Description gets remainder (truncated)\n    // ModeLabelValue: find max Label len, Value gets remainder\n    // Apply styles from style package\n    // Show window around Selected (max 15 visible)\n}
Acceptance: ModeSingleCol renders just the Label across full width (file picker use case)
Acceptance: ModeLabelMeta renders Label left-padded + Meta right (session picker: name + date)
Acceptance: ModeLabelDesc renders Label left-padded + Description truncated (skill picker: name + description)
Acceptance: Selected row uses BgSelected background and accent text
Acceptance: Non-selected rows use BgFooter background and muted text
Acceptance: Shows filter hint line when filter is non-empty
Acceptance: Shows 'No matches' line when filtered list is empty
Acceptance: Windowing shows max 15 items centered around selection, matching RenderHeight()
Acceptance: Total render height matches RenderHeight() return value for layout calculations
Verification: cd ~/src/squid-os && go build ./...

## MILESTONE: 2 - Migration
Pattern: Strangler Fig
Objective: Replace all 4 PickerList usages with the new Picker component, eliminating the giant switch statements and parallel data arrays
Success: All pickers use the new Picker component. handlePickerKey and confirmPicker are gone or drastically reduced. PickerList type is removed or deprecated.
Diagram: graph TD
    A[handlePickerKey] -->|Before| B[switch on pickerType string]
    B --> C[model case]
    B --> D[skill case]
    B --> E[session case]
    B --> F[file case]
    G[confirmPicker] -->|Before| H[switch on pickerType string]
    I[handleKey] -->|After| J[picker.HandleKey returns Action]
    J --> K[ActionSelect: thin wrapper]
    J --> L[ActionCancel: setChatMode]

### TASK: 2.1 - Replace PickerList in Model with single Picker field
Type: refactor
What: Replace the 4 PickerList fields and sessionPickerRaw in app.go with a single activePicker ui.Picker and pickerContext string. Update New() constructor and all picker creation sites.
Why: Eliminates parallel state (modelPicker, skillPicker, sessionPicker, filePicker, sessionPickerRaw, filePickerFor) and collapses it into one component that callers configure.
Files: ~ internal/app/app.go
Snippet: type Model struct {\n    // Pickers - single reusable component\n    activePicker   ui.Picker\n    pickerContext  string // identifies which picker: "model", "skill", "session", "image", "system"\n    pickerPayload  interface{} // additional context (e.g. modelEntries for model picker)\n}
Acceptance: Model struct no longer has modelPicker, skillPicker, sessionPicker, filePicker, sessionPickerRaw, filePickerFor fields
Acceptance: activePicker and pickerContext fields added to Model
Acceptance: All picker creation sites updated to use activePicker with appropriate DisplayMode and PickerItem slices
Acceptance: go build passes with no errors
Verification: cd ~/src/squid-os && go build ./...

### TASK: 2.2 - Refactor key handling and confirm logic
Type: refactor
What: Replace handlePickerKey and confirmPicker in pickers.go with a single handleActivePicker method that calls activePicker.HandleKey() and dispatches Select/Cancel. Update handleKey in input.go and recalcLayout in update.go.
Why: Eliminates 120+ line switch-on-string dispatchers and collapses them into a ~30-line handler that uses the component's built-in key handling.
Files: ~ internal/app/pickers.go
Files: ~ internal/app/input.go
Files: ~ internal/app/update.go
Files: ~ internal/app/render.go
Snippet: // In input.go - all picker modes route through one handler\ncase ModeModelPicker, ModeSkillPicker, ModeSessionPicker, ModeFilePicker:\n    return m.handleActivePicker(msg)\n\n// In pickers.go\nfunc (m Model) handleActivePicker(msg tea.KeyMsg) (tea.Model, tea.Cmd) {\n    action := m.activePicker.HandleKey(msg)\n    switch action {\n    case ui.ActionCancel:\n        // Restore session snapshot if needed\n        return m, m.setChatMode()\n    case ui.ActionSelect:\n        return m.confirmActivePicker()\n    }\n    // Handle viewport scroll keys (Up/Down still handled by picker,\n    // but Scroll/Page keys go to viewport)\n    m.recalcLayout()\n    return m, nil\n}\n\nfunc (m Model) confirmActivePicker() (tea.Model, tea.Cmd) {\n    // Thin switch on pickerContext to apply domain-specific logic\n    // This switch stays because the *consequence* of selection is domain-specific\n}
Acceptance: handlePickerKey function removed or reduced to handleActivePicker
Acceptance: input.go routes all picker modes through handleActivePicker instead of individual calls
Acceptance: confirmActivePicker has a switch on pickerContext for domain-specific apply logic (model switch, skill set, session load, file attach)
Acceptance: recalcLayout uses m.activePicker.RenderHeight() instead of individual picker references
Acceptance: render.go uses m.activePicker.Render(m.width) for all picker modes
Acceptance: go build passes with no errors
Verification: cd ~/src/squid-os && go build ./...

### TASK: 2.3 - Migrate all picker creation sites
Type: refactor
What: Update openSkillPicker, startLoad, scanModelsCmd handler, and image/system picker creation to build PickerItem slices and use the new Picker API with appropriate DisplayMode.
Why: Completes the migration — each call site constructs typed PickerItems instead of pre-formatted strings, eliminating parallel raw arrays and string parsing in confirm logic.
Files: ~ internal/app/skill.go
Files: ~ internal/app/session.go
Files: ~ internal/app/update.go
Files: ~ internal/app/pickers.go
Snippet: // skill.go - ModeLabelDesc\nitems := make([]ui.PickerItem, 0, len(entries))\nfor _, e := range entries {\n    items = append(items, ui.PickerItem{\n        Label:       e.name,\n        Description: e.description,\n        Value:       e.name,\n    })\n}\nm.activePicker = ui.Picker{\n    Title:       "Select Skill",\n    Items:       items,\n    DisplayMode: ui.ModeLabelDesc,\n    DefaultMatch: currentSkill,\n}\n\n// session.go - ModeLabelMeta\nitems := make([]ui.PickerItem, len(sessions))\nfor i, s := range sessions {\n    items[i] = ui.PickerItem{\n        Label: s.Name,\n        Meta:  util.FriendlyModDate(s.ModTime),\n        Value: s.Name,\n    }\n}\nm.activePicker = ui.Picker{\n    Title:       "Load Session",\n    Items:       items,\n    DisplayMode: ui.ModeLabelMeta,\n    DefaultMatch: m.settings.LastSessionName,\n    OnSelectionChange: func(idx int, item ui.PickerItem) {\n        m = m.previewSession(item.Value)\n    },\n}\n\n// update.go - ModeLabelValue (provider + model)\n// pickers.go - ModeSingleCol for file paths
Acceptance: Skill picker uses ModeLabelDesc with PickerItem.Label=skill name, Description=skill description
Acceptance: Session picker uses ModeLabelMeta with PickerItem.Label=session name, Meta=date string, Value=raw name
Acceptance: Session picker sets OnSelectionChange callback for live preview
Acceptance: Model picker uses ModeLabelValue with PickerItem.Label=model name, Meta=context length, Value=full model ID, provider in Label or separate field
Acceptance: File picker uses ModeSingleCol with PickerItem.Label=path, Value=path
Acceptance: confirmActivePicker extracts typed values from PickerItem instead of parsing pre-formatted strings
Acceptance: sessionPickerRaw parallel array is gone — raw name comes from PickerItem.Value
Acceptance: No calls to ui.NewPickerList remain in the codebase
Acceptance: go build passes with no errors
Verification: cd ~/src/squid-os && go build ./...

### TASK: 2.4 - Clean up and remove old PickerList code
Type: chore
What: Remove PickerList type, NewPickerList function, and any dead code. Verify PickerList is not referenced anywhere in the codebase.
Why: Completes the strangler pattern — the old type is fully replaced and can be removed.
Files: ~ internal/ui/command.go
Snippet: // Remove from command.go:\n// type PickerList struct { ... }\n// func NewPickerList(...) PickerList\n// func (pl *PickerList) FilteredItems()\n// func (pl *PickerList) MoveUp()\n// func (pl *PickerList) MoveDown()\n// func (pl *PickerList) SelectedItem()\n// func (pl *PickerList) RenderHeight()\n// func (pl *PickerList) Render()
Acceptance: PickerList type is removed from command.go
Acceptance: grep for PickerList and NewPickerList returns no results in internal/
Acceptance: maxPickerItems constant removed or renamed to PickerMaxItems
Acceptance: go build passes with no errors
Verification: cd ~/src/squid-os && go build ./... && grep -r 'PickerList\|NewPickerList' internal/ || echo 'CLEAN'
