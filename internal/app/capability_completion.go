package app

import (
	"sort"
	"strconv"
	"strings"
	"unicode"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/rivo/uniseg"
	"squid-os/internal/style"
)

type capabilityCandidate struct {
	kind string // "skill" or "agent"
	name string
}

func (c capabilityCandidate) qualified() string { return c.kind + "/" + c.name }
func (c capabilityCandidate) label() string     { return c.kind + "  " + c.name }

type capabilityCompletion struct {
	line       int
	start      int // rune offset of '@' in the logical line
	end        int // rune offset immediately after the reference
	query      string
	candidates []capabilityCandidate
}

// capabilityCandidates returns the skills and agents available to this session.
// Same-name capabilities are retained because their kinds disambiguate them.
func (m Model) capabilityCandidates() []capabilityCandidate {
	var candidates []capabilityCandidate

	if reg := m.session.Catalog.Skills; reg != nil {
		allowed := make(map[string]bool, len(m.session.Doc.Config.Skills))
		for _, ref := range m.session.Doc.Config.Skills {
			allowed[ref.Name] = true
		}
		for _, entry := range reg.List() {
			if allowed[entry.Name] {
				candidates = append(candidates, capabilityCandidate{kind: "skill", name: entry.Name})
			}
		}
	}

	if reg := m.session.Catalog.Agents; reg != nil {
		allowed := make(map[string]bool, len(m.session.Doc.Config.Agents))
		for _, ref := range m.session.Doc.Config.Agents {
			allowed[ref.Name] = true
		}
		for _, entry := range reg.List() {
			if allowed[entry.Name] {
				candidates = append(candidates, capabilityCandidate{kind: "agent", name: entry.Name})
			}
		}
	}

	for _, tool := range m.session.GetTools() {
		candidates = append(candidates, capabilityCandidate{kind: "tool", name: tool.Name})
	}

	sort.Slice(candidates, func(i, j int) bool {
		rank := func(kind string) int {
			switch kind {
			case "skill":
				return 0
			case "agent":
				return 1
			default: // tool
				return 2
			}
		}
		ri, rj := rank(candidates[i].kind), rank(candidates[j].kind)
		if ri != rj {
			return ri < rj
		}
		return candidates[i].name < candidates[j].name
	})
	return candidates
}

// capabilityCompletionAtCursor derives completion state from the current text
// and cursor, without applying Escape dismissal state. Users type a short
// @name prefix; unique completion inserts an explicit @skill/name or @agent/name.
func (m Model) capabilityCompletionAtCursor() (capabilityCompletion, bool) {
	lines := strings.Split(m.textarea.Value(), "\n")
	lineIndex := m.textarea.Line()
	if lineIndex < 0 || lineIndex >= len(lines) {
		return capabilityCompletion{}, false
	}

	line := []rune(lines[lineIndex])
	info := m.textarea.LineInfo()
	cursor := info.StartColumn + info.ColumnOffset
	if cursor < 0 || cursor > len(line) {
		return capabilityCompletion{}, false
	}

	start := cursor - 1
	for start >= 0 && isCapabilityNameRune(line[start]) {
		start--
	}
	if start < 0 || line[start] != '@' || !isCapabilityBoundary(line, start) {
		return capabilityCompletion{}, false
	}

	query := string(line[start+1 : cursor])
	var candidates []capabilityCandidate
	for _, candidate := range m.capabilityCandidates() {
		if strings.HasPrefix(candidate.name, query) {
			candidates = append(candidates, candidate)
		}
	}
	if len(candidates) == 0 {
		return capabilityCompletion{}, false
	}

	end := cursor
	for end < len(line) && isCapabilityNameRune(line[end]) {
		end++
	}
	return capabilityCompletion{
		line:       lineIndex,
		start:      start,
		end:        end,
		query:      query,
		candidates: candidates,
	}, true
}

// activeCapabilityCompletion applies Escape dismissal to the completion
// currently derived from the textarea.
func (m Model) activeCapabilityCompletion() (capabilityCompletion, bool) {
	completion, ok := m.capabilityCompletionAtCursor()
	if !ok || m.completionDismissed == completion.key() {
		return capabilityCompletion{}, false
	}
	return completion, true
}

func isCapabilityNameRune(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r) || r == '-' || r == '_'
}

func isCapabilityBoundary(line []rune, at int) bool {
	if at == 0 {
		return true
	}
	r := line[at-1]
	return unicode.IsSpace(r) || strings.ContainsRune("([{<\"'`:,;", r)
}

func (c capabilityCompletion) key() string {
	return strings.Join([]string{strconv.Itoa(c.line), strconv.Itoa(c.start), c.query}, "\x00")
}

func longestCandidateNamePrefix(values []capabilityCandidate) string {
	if len(values) == 0 {
		return ""
	}
	prefix := []rune(values[0].name)
	for _, value := range values[1:] {
		runes := []rune(value.name)
		n := len(prefix)
		if len(runes) < n {
			n = len(runes)
		}
		i := 0
		for i < n && prefix[i] == runes[i] {
			i++
		}
		prefix = prefix[:i]
	}
	return string(prefix)
}

// applyCapabilityCompletion qualifies a selected or unique candidate. For
// multiple unselected candidates it only extends their certain common prefix;
// if it cannot extend, Tab is deliberately non-destructive.
func (m *Model) applyCapabilityCompletion(c capabilityCompletion) (tea.Model, tea.Cmd) {
	target := ""
	selected, hasSelection := m.selectedCapabilityCandidate(c)
	if hasSelection {
		target = selected.qualified()
	} else if len(c.candidates) == 1 {
		target = c.candidates[0].qualified()
	} else {
		target = longestCandidateNamePrefix(c.candidates)
		if len([]rune(target)) <= len([]rune(c.query)) {
			return m, nil
		}
	}

	lines := strings.Split(m.textarea.Value(), "\n")
	line := []rune(lines[c.line])
	suffix := line[c.end:]
	if len(c.candidates) == 1 || hasSelection {
		// A qualified completion is a complete reference. Insert one trailing
		// space so the user can continue typing without manually terminating it.
		// Reuse an existing whitespace delimiter rather than duplicating it.
		if len(suffix) > 0 && unicode.IsSpace(suffix[0]) {
			suffix = suffix[1:]
		}
		target += " "
	}
	updated := append([]rune{}, line[:c.start+1]...)
	updated = append(updated, []rune(target)...)
	updated = append(updated, suffix...)
	lines[c.line] = string(updated)

	m.textarea.SetValue(strings.Join(lines, "\n"))
	for m.textarea.Line() > c.line {
		m.textarea.CursorUp()
	}
	m.textarea.SetCursor(c.start + 1 + len([]rune(target)))
	m.completionDismissed = ""
	m.clearCapabilitySelection()
	m.autoSizeTextarea()
	return m, nil
}

func (m *Model) clearCapabilitySelection() {
	m.completionSelected = 0
	m.completionSelectKey = ""
	m.completionWindow = 0
}

// moveCapabilitySelection enters selection mode on the first item, then moves
// left/right without wrapping. It returns false when no completion is active.
func (m *Model) moveCapabilitySelection(delta int) bool {
	completion, ok := m.activeCapabilityCompletion()
	if !ok {
		m.clearCapabilitySelection()
		return false
	}
	key := completion.key()
	if m.completionSelectKey != key {
		m.completionSelectKey = key
		m.completionSelected = 0
		m.completionWindow = 0
		return true
	}
	m.completionSelected += delta
	if m.completionSelected < 0 {
		m.completionSelected = 0
	}
	if last := len(completion.candidates) - 1; m.completionSelected > last {
		m.completionSelected = last
	}
	m.ensureCapabilitySelectionVisible(completion)
	return true
}

func (m *Model) ensureCapabilitySelectionVisible(completion capabilityCompletion) {
	available := capabilitySuggestionAvailable(m.width)
	if m.completionSelected < m.completionWindow {
		m.completionWindow = m.completionSelected
	}
	for m.completionSelected >= capabilityVisibleEnd(completion.candidates, m.completionWindow, available) {
		m.completionWindow++
	}
}

func (m Model) selectedCapabilityCandidate(c capabilityCompletion) (capabilityCandidate, bool) {
	if m.completionSelectKey != c.key() || m.completionSelected < 0 || m.completionSelected >= len(c.candidates) {
		return capabilityCandidate{}, false
	}
	return c.candidates[m.completionSelected], true
}

func capabilityCandidateStyle(kind string) lipgloss.Style {
	color := style.P.TextAccent
	switch kind {
	case "skill":
		color = style.P.TextSkill
	case "agent":
		color = style.P.TextAgent
	}
	return lipgloss.NewStyle().Foreground(lipgloss.Color(color))
}

const (
	capabilitySuggestionPrefix = "complete  "
	capabilitySuggestionHelp   = "  [←/→, tab/enter, esc]"
	capabilitySuggestionGap    = 2
	capabilityOverflowWidth    = 5 // "‹ " before and "  ›" after candidates.
)

func capabilitySuggestionAvailable(width int) int {
	// Reserve both possible overflow markers so fitting remains stable while
	// navigating. Measure terminal cells rather than bytes or rune count.
	chrome := uniseg.StringWidth(capabilitySuggestionPrefix) +
		uniseg.StringWidth(capabilitySuggestionHelp) + capabilityOverflowWidth
	available := width - chrome
	if available < 1 {
		return 1
	}
	return available
}

func capabilityVisibleEnd(candidates []capabilityCandidate, start, available int) int {
	used := 0
	end := start
	for end < len(candidates) {
		width := uniseg.StringWidth(candidates[end].qualified())
		if end > start {
			width += capabilitySuggestionGap
		}
		if used+width > available {
			break
		}
		used += width
		end++
	}
	// On very narrow terminals, always expose one complete selectable item.
	if end == start && start < len(candidates) {
		end++
	}
	return end
}

func (m *Model) capabilityVisibleRange(completion capabilityCompletion) (int, int) {
	const chromeWidth = 38 // "complete", navigation help, and breathing room.
	available := m.width - chromeWidth
	if available < 1 {
		available = 1
	}
	start := m.completionWindow
	if start < 0 || start >= len(completion.candidates) {
		start = 0
	}
	if m.completionSelectKey == completion.key() {
		if m.completionSelected < start {
			start = m.completionSelected
		}
		for {
			end := capabilityVisibleEnd(completion.candidates, start, available)
			if m.completionSelected < end || start >= m.completionSelected {
				break
			}
			start++
		}
	}
	m.completionWindow = start
	return start, capabilityVisibleEnd(completion.candidates, start, available)
}

func (m *Model) renderCapabilitySuggestion() string {
	completion, ok := m.activeCapabilityCompletion()
	if !ok {
		return ""
	}

	start, end := m.capabilityVisibleRange(completion)
	shown := completion.candidates[start:end]

	dim := lipgloss.NewStyle().Foreground(lipgloss.Color(style.P.TextDim))
	arrow := lipgloss.NewStyle().Foreground(lipgloss.Color(style.P.TextPrimary))
	items := make([]string, len(shown))
	for i, candidate := range shown {
		candidateStyle := capabilityCandidateStyle(candidate.kind)
		if m.completionSelectKey == completion.key() && start+i == m.completionSelected {
			candidateStyle = candidateStyle.Background(lipgloss.Color(style.P.BgSelected)).Bold(true)
		}
		items[i] = candidateStyle.Render(candidate.qualified())
	}
	text := dim.Render(capabilitySuggestionPrefix) + strings.Join(items, "  ")
	if start > 0 {
		text = dim.Render(capabilitySuggestionPrefix) + arrow.Render("‹ ") + strings.Join(items, "  ")
	}
	if end < len(completion.candidates) {
		text += arrow.Render("  ›")
	}
	text += dim.Render(capabilitySuggestionHelp)
	return lipgloss.NewStyle().Width(m.width).Render(text)
}
