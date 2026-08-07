package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"squid-os/internal/agent"
	"squid-os/internal/config"
	runtimeconfig "squid-os/internal/runtime"
	"squid-os/internal/skills"
	"squid-os/internal/style"
)

func TestBareAtOpensSuggestionsAndTabDoesNotListify(t *testing.T) {
	m := newCompletionTestModel(t, []string{"sop-api", "sop-chain"}, []string{"support"})
	m.textarea.SetValue("use @")

	completion, ok := m.activeCapabilityCompletion()
	if !ok || completion.query != "" || len(completion.candidates) != 3 {
		t.Fatalf("bare @ completion = %+v, active = %v", completion, ok)
	}
	if suggestion := m.renderCapabilitySuggestion(); suggestion == "" {
		t.Fatal("bare @ should render the suggestion bar")
	}

	updated, _ := m.handleChatKey(tea.KeyMsg{Type: tea.KeyTab})
	got := updated.(*Model).textarea.Value()
	if got == "1> use @\n2> " {
		t.Fatal("Tab incorrectly fell through to listify")
	}
	if got != "use @s" {
		t.Fatalf("bare @ Tab = %q, want common prefix %q", got, "use @s")
	}
}

func TestCapabilityCompletionCombinesAllowedSkillsAndAgents(t *testing.T) {
	m := newCompletionTestModel(t, []string{"sop-api", "sop-chain"}, []string{"support"})
	m.textarea.SetValue("use @sop")

	completion, ok := m.activeCapabilityCompletion()
	if !ok {
		t.Fatal("expected active completion")
	}
	want := []capabilityCandidate{{kind: "skill", name: "sop-api"}, {kind: "skill", name: "sop-chain"}}
	if len(completion.candidates) != len(want) {
		t.Fatalf("candidates = %v, want %v", completion.candidates, want)
	}
	for i := range want {
		if completion.candidates[i] != want[i] {
			t.Fatalf("candidates = %v, want %v", completion.candidates, want)
		}
	}

	m.textarea.SetValue("ask @sup")
	completion, ok = m.activeCapabilityCompletion()
	if !ok || len(completion.candidates) != 1 || completion.candidates[0] != (capabilityCandidate{kind: "agent", name: "support"}) {
		t.Fatalf("agent candidates = %v, active = %v", completion.candidates, ok)
	}
}

func TestCapabilityCompletionUsesLongestCommonPrefixThenUniqueName(t *testing.T) {
	m := newCompletionTestModel(t, []string{"sop-api", "sop-chain"}, nil)
	m.textarea.SetValue("use @sop")

	completion, ok := m.activeCapabilityCompletion()
	if !ok {
		t.Fatal("expected active completion")
	}
	updated, _ := (&m).applyCapabilityCompletion(completion)
	m = *updated.(*Model)
	if got := m.textarea.Value(); got != "use @sop-" {
		t.Fatalf("first Tab = %q, want %q", got, "use @sop-")
	}

	m.textarea.InsertString("c")
	completion, ok = m.activeCapabilityCompletion()
	if !ok {
		t.Fatal("expected unique completion")
	}
	updated, _ = (&m).applyCapabilityCompletion(completion)
	m = *updated.(*Model)
	if got := m.textarea.Value(); got != "use @skill/sop-chain " {
		t.Fatalf("second Tab = %q, want %q", got, "use @skill/sop-chain ")
	}
	if _, ok := m.activeCapabilityCompletion(); ok {
		t.Fatal("completed exact reference should close suggestions")
	}
}

func TestUniqueCompletionAddsExactlyOneTrailingSpace(t *testing.T) {
	m := newCompletionTestModel(t, nil, []string{"trader"})
	m.textarea.SetValue("ask @trad next")
	m.textarea.SetCursor(len([]rune("ask @trad")))

	completion, ok := m.activeCapabilityCompletion()
	if !ok {
		t.Fatal("expected unique agent completion")
	}
	updated, _ := (&m).applyCapabilityCompletion(completion)
	got := updated.(*Model).textarea.Value()
	if got != "ask @agent/trader next" {
		t.Fatalf("completion spacing = %q", got)
	}
}

func TestCandidatesOrderByKindThenName(t *testing.T) {
	m := newCompletionTestModel(t, []string{"z-skill", "a-skill"}, []string{"z-agent", "a-agent"})
	m.session.Doc.Config.Tools = []string{"write_file", "bash"}

	got := m.capabilityCandidates()
	want := []capabilityCandidate{
		{kind: "skill", name: "a-skill"},
		{kind: "skill", name: "z-skill"},
		{kind: "agent", name: "a-agent"},
		{kind: "agent", name: "z-agent"},
		{kind: "tool", name: "bash"},
		{kind: "tool", name: "write_file"},
	}
	if len(got) != len(want) {
		t.Fatalf("candidate order = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("candidate order = %v, want %v", got, want)
		}
	}
}

func TestToolCompletionUsesEnabledSessionTools(t *testing.T) {
	m := newCompletionTestModel(t, nil, nil)
	m.session.Doc.Config.Tools = []string{"read_file", "bash"}
	m.textarea.SetValue("use @read")

	completion, ok := m.activeCapabilityCompletion()
	if !ok || len(completion.candidates) != 1 || completion.candidates[0] != (capabilityCandidate{kind: "tool", name: "read_file"}) {
		t.Fatalf("tool completion = %+v, active = %v", completion, ok)
	}
	updated, _ := (&m).applyCapabilityCompletion(completion)
	if got := updated.(*Model).textarea.Value(); got != "use @tool/read_file " {
		t.Fatalf("tool completion text = %q", got)
	}
}

func TestCategoryStylesUseExistingPalette(t *testing.T) {
	if got := capabilityCandidateStyle("tool").GetForeground(); got != lipgloss.Color(style.P.TextAccent) {
		t.Fatalf("tool color = %v", got)
	}
	if got := capabilityCandidateStyle("skill").GetForeground(); got != lipgloss.Color(style.P.TextSkill) {
		t.Fatalf("skill color = %v", got)
	}
	if got := capabilityCandidateStyle("agent").GetForeground(); got != lipgloss.Color(style.P.TextAgent) {
		t.Fatalf("agent color = %v", got)
	}
}

func TestAmbiguousTabDoesNotGuess(t *testing.T) {
	m := newCompletionTestModel(t, []string{"sop-api", "sop-chain"}, nil)
	m.textarea.SetValue("use @sop-")

	completion, ok := m.activeCapabilityCompletion()
	if !ok {
		t.Fatal("expected ambiguous completion")
	}
	updated, _ := (&m).applyCapabilityCompletion(completion)
	m = *updated.(*Model)
	if got := m.textarea.Value(); got != "use @sop-" {
		t.Fatalf("ambiguous Tab changed text to %q", got)
	}
	if _, ok := m.activeCapabilityCompletion(); !ok {
		t.Fatal("ambiguous Tab should keep suggestions visible")
	}
}

func TestSameNameSkillAndAgentRemainAmbiguousAndLabeled(t *testing.T) {
	m := newCompletionTestModel(t, []string{"reviewer"}, []string{"reviewer"})
	m.textarea.SetValue("ask @reviewer")

	completion, ok := m.activeCapabilityCompletion()
	if !ok || len(completion.candidates) != 2 {
		t.Fatalf("collision completion = %+v, active = %v", completion, ok)
	}
	updated, _ := (&m).applyCapabilityCompletion(completion)
	if got := updated.(*Model).textarea.Value(); got != "ask @reviewer" {
		t.Fatalf("collision Tab guessed %q", got)
	}
	suggestion := m.renderCapabilitySuggestion()
	if !strings.Contains(suggestion, "skill/") || !strings.Contains(suggestion, "agent/") || !strings.Contains(suggestion, "reviewer") {
		t.Fatalf("collision labels missing from %q", suggestion)
	}
}

func TestCapabilityCompletionBoundaryAndEscape(t *testing.T) {
	m := newCompletionTestModel(t, []string{"sop-chain"}, nil)

	m.textarea.SetValue("foo@sop")
	if _, ok := m.activeCapabilityCompletion(); ok {
		t.Fatal("embedded @ should not activate completion")
	}

	m.textarea.SetValue("foo (@sop")
	completion, ok := m.activeCapabilityCompletion()
	if !ok {
		t.Fatal("@ after opening punctuation should activate completion")
	}
	m.completionDismissed = completion.key()
	if _, ok := m.activeCapabilityCompletion(); ok {
		t.Fatal("Escape dismissal should hide the current completion")
	}

	m.textarea.InsertString("-")
	m.completionDismissed = ""
	if _, ok := m.activeCapabilityCompletion(); !ok {
		t.Fatal("editing the token should allow completion again")
	}
}

func TestEnterAcceptsUniqueCompletionBeforeSend(t *testing.T) {
	m := newCompletionTestModel(t, nil, []string{"trader"})
	m.textarea.SetValue("ask @tra")

	updated, _ := m.handleChatKey(tea.KeyMsg{Type: tea.KeyEnter})
	got := updated.(Model).textarea.Value()
	if got != "ask @agent/trader " {
		t.Fatalf("Enter did not accept unique completion: %q", got)
	}
}

func TestEnterDoesNotGuessAmbiguousCompletion(t *testing.T) {
	m := newCompletionTestModel(t, []string{"sop-api", "sop-chain"}, nil)
	m.textarea.SetValue("use @sop-")

	updated, _ := m.handleChatKey(tea.KeyMsg{Type: tea.KeyEnter})
	got := updated.(Model).textarea.Value()
	if got != "use @sop-" {
		t.Fatalf("Enter guessed ambiguous completion: %q", got)
	}
}

func TestArrowSelectionAndTabAccept(t *testing.T) {
	m := newCompletionTestModel(t, []string{"alpha", "beta"}, []string{"gamma"})
	m.textarea.SetValue("use @")

	updated, _ := m.handleChatKey(tea.KeyMsg{Type: tea.KeyRight})
	m = updated.(Model)
	if m.completionSelectKey == "" || m.completionSelected != 0 {
		t.Fatalf("first arrow selection = key %q index %d", m.completionSelectKey, m.completionSelected)
	}
	updated, _ = m.handleChatKey(tea.KeyMsg{Type: tea.KeyRight})
	m = updated.(Model)
	if m.completionSelected != 1 {
		t.Fatalf("second right index = %d", m.completionSelected)
	}
	updated, _ = m.handleChatKey(tea.KeyMsg{Type: tea.KeyTab})
	if got := updated.(*Model).textarea.Value(); got != "use @skill/beta " {
		t.Fatalf("selected Tab = %q", got)
	}
}

func TestArrowSelectionClampsAndTypingClears(t *testing.T) {
	m := newCompletionTestModel(t, []string{"alpha", "beta"}, nil)
	m.textarea.SetValue("use @")

	for range 5 {
		updated, _ := m.handleChatKey(tea.KeyMsg{Type: tea.KeyRight})
		m = updated.(Model)
	}
	if m.completionSelected != 1 {
		t.Fatalf("right should clamp at last item, got %d", m.completionSelected)
	}
	updated, _ := m.handleChatKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	m = updated.(Model)
	if m.completionSelectKey != "" {
		t.Fatal("typing should leave selection mode")
	}
}

func TestSuggestionNeverExceedsTerminalWidth(t *testing.T) {
	m := newCompletionTestModel(t, []string{"very-long-skill-name", "another-long-skill-name"}, []string{"very-long-agent-name"})
	m.session.Doc.Config.Tools = []string{"set_working_dir", "write_file"}
	m.textarea.SetValue("use @")

	for _, width := range []int{40, 58, 80, 120} {
		m.width = width
		m.completionWindow = 0
		rendered := m.renderCapabilitySuggestion()
		if got := lipgloss.Width(rendered); got > width {
			t.Fatalf("width %d rendered %d cells", width, got)
		}
	}
}

func TestSelectionWindowShiftsByWidth(t *testing.T) {
	m := newCompletionTestModel(t, []string{"aaaaaaaa", "bbbbbbbb", "cccccccc", "dddddddd"}, nil)
	m.width = 58
	m.textarea.SetValue("use @")

	updated, _ := m.handleChatKey(tea.KeyMsg{Type: tea.KeyRight})
	m = updated.(Model)
	updated, _ = m.handleChatKey(tea.KeyMsg{Type: tea.KeyRight})
	m = updated.(Model)
	if m.completionSelected != 1 || m.completionWindow != 1 {
		t.Fatalf("narrow window selection=%d start=%d", m.completionSelected, m.completionWindow)
	}
}

func TestSelectedEnterAcceptsAmbiguousCandidate(t *testing.T) {
	m := newCompletionTestModel(t, []string{"alpha", "beta"}, nil)
	m.textarea.SetValue("use @")
	updated, _ := m.handleChatKey(tea.KeyMsg{Type: tea.KeyRight})
	m = updated.(Model)

	updated, _ = m.handleChatKey(tea.KeyMsg{Type: tea.KeyEnter})
	if got := updated.(Model).textarea.Value(); got != "use @skill/alpha " {
		t.Fatalf("selected Enter = %q", got)
	}
}

func TestTabFallsBackToListifyWithoutCompletion(t *testing.T) {
	m := newCompletionTestModel(t, []string{"sop-chain"}, nil)
	m.textarea.SetValue("plain text")
	updated, _ := m.handleChatKey(tea.KeyMsg{Type: tea.KeyTab})
	got := updated.(*Model).textarea.Value()
	if got != "1> plain text\n2> " {
		t.Fatalf("Tab fallback = %q", got)
	}
}

func newCompletionTestModel(t *testing.T, skillNames, agentNames []string) Model {
	t.Helper()
	globalSkills, workspace, globalAgents := t.TempDir(), t.TempDir(), t.TempDir()
	workspaceSkills := filepath.Join(workspace, ".squid-os", "skills")
	workspaceAgents := filepath.Join(workspace, ".squid-os", "agents")

	var skillRefs []config.CapabilityRef
	for _, name := range skillNames {
		writePickerSkill(t, workspaceSkills, name)
		skillRefs = append(skillRefs, config.CapabilityRef{Scope: config.CapabilityScopeWorkspace, Name: name})
	}
	var agentRefs []config.CapabilityRef
	for _, name := range agentNames {
		dir := filepath.Join(workspaceAgents, name)
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatal(err)
		}
		data := []byte("name: " + name + "\ndescription: completion test\nsystem_prompt: test\n")
		if err := os.WriteFile(filepath.Join(dir, "agent.yaml"), data, 0644); err != nil {
			t.Fatal(err)
		}
		agentRefs = append(agentRefs, config.CapabilityRef{Scope: config.CapabilityScopeWorkspace, Name: name})
	}

	skillRegistry, err := skills.LoadRegistry(globalSkills, workspaceSkills)
	if err != nil {
		t.Fatal(err)
	}
	agentRegistry, err := agent.LoadRegistry(globalAgents, workspaceAgents)
	if err != nil {
		t.Fatal(err)
	}

	m := New(StartupOptions{Session: runtimeconfig.SessionRequest{
		Config: config.SessionConfig{Skills: skillRefs, Agents: agentRefs},
		Catalog: runtimeconfig.Catalog{
			WorkingDir: workspace,
			Skills:     skillRegistry,
			Agents:     agentRegistry,
		},
	}})
	m.width, m.height, m.ready = 100, 30, true
	m.recalcLayout()
	return m
}
