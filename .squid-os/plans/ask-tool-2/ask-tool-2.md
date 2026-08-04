# Ask Tool: Reusable Question Prompt Component

## Core Problem

The authorization prompt pattern (yes/no + optional text) is a specific case of a more general need: the AI needs to collect structured information from users in a UX-friendly way (interviews, clarifications, preferences). Currently the auth prompt is tightly coupled to tool authorization. We need a reusable QuestionPrompt component that supports multiple input modes (yes/no, radio, checkbox) with an optional text field, and an ask_tool that leverages it. The existing authorization flow should then be refactored to use this component.

## Goal

A reusable QuestionPrompt UI component supporting yes/no, radio, and checkbox modes. An ask_tool that returns structured answers to the AI. The existing authorization flow refactored to use the component. All prompt interactions share the same navigation pattern: left/right for selection, tab for text input, shift+tab back, enter to confirm.

---

## 1. Question Prompt Component

- **Pattern:** Component Extraction / Composite Pattern

**Objective:** Extract the yes/no + optional text input pattern from the authorization prompt into a reusable QuestionPrompt component that supports multiple question types.

**Success Criteria:** QuestionPrompt struct supports yes/no, radio, and checkbox question types. A dedicated text input area (not the chat textarea) for supplementary answers. Navigation: left/right or up/down for selection, Tab to switch to text, Shift+Tab back, Enter to confirm. Existing auth prompt code can be replaced by a thin wrapper.

```mermaid
QuestionPrompt struct → QuestionType (yesno/radio/checkbox) → Options []string → Selected indices → TextInput string → TextMode bool → Render() returns styled prompt → Update(key) handles navigation
```

### 1.1. Create QuestionPrompt component with types and state

**Type:** feature

**What:** Create internal/ui/question_prompt.go with QuestionType enum (QuestionYesNo, QuestionRadio, QuestionCheckbox), QuestionPrompt struct, and NewQuestionPrompt constructor.

**Why:** This is the foundational reusable component — replaces the AuthorizationPrompt's selection logic with a generalized question interface that any consumer (auth, ask_tool) can use.

**Files:**

- + internal/ui/question_prompt.go

**Snippet:**

```
type QuestionType int

const (
	QuestionYesNo   QuestionType = iota // yes/no binary
	QuestionRadio                       // single choice from list
	QuestionCheckbox                    // multiple choices from list
)

type QuestionPrompt struct {
	Type       QuestionType
	Question   string         // the prompt/question text
	Options    []string       // option labels (for radio/checkbox; yesno uses ["Yes","No"])
	Selected   int            // current cursor position (index into Options)
	MultiSel   []bool         // for checkbox: per-option selection state
	TextMode   bool           // true = user is typing supplementary text
	TextInput  string         // free-text supplementary answer
	TextPrompt string         // custom label for text input (e.g. "Elaborate...")
	Width      int
}

func NewQuestionPrompt(qt QuestionType, question string, options []string, width int) *QuestionPrompt {
	if qt == QuestionYesNo && len(options) == 0 {
		options = []string{"Yes", "No"}
	}
	multiSel := make([]bool, len(options))
	return &QuestionPrompt{
		Type:     qt,
		Question: question,
		Options:  options,
		Selected: 0,
		MultiSel: multiSel,
		Width:    width,
	}
}

func (p *QuestionPrompt) SetTextPrompt(prompt string) {
	p.TextPrompt = prompt
}
```

```
func (p *QuestionPrompt) GetAnswers() map[string]interface{} {
	// Returns structured answer based on type
	switch p.Type {
	case QuestionYesNo:
		return map[string]interface{}{
			"answer": p.Selected == 0, // true=Yes, false=No
		}
	case QuestionRadio:
		return map[string]interface{}{
			"answer": p.Selected, // index of selected option
		}
	case QuestionCheckbox:
		indices := []int{}
		for i, sel := range p.MultiSel {
			if sel { indices = append(indices, i) }
		}
		return map[string]interface{}{
			"answer": indices, // list of selected indices
		}
	}
	return nil
}

func (p *QuestionPrompt) GetTextAnswer() string {
	return p.TextInput
}
```

**Acceptance Criteria:**

- [ ] QuestionPrompt compiles, NewQuestionPrompt creates valid state for all 3 types, GetAnswers returns correct structure per type

**Verify:**

```bash
cd /home/goglue/src/squid-os && go build ./...
```

### 1.2. Implement QuestionPrompt key handling and rendering

**Type:** feature

**What:** Add Update(key) method for navigation (left/right/up/down for selection, Tab/Shift+Tab for text mode, Enter to confirm, Escape to cancel) and Render() method with styled output for all 3 question types.

**Why:** The component needs its own input handling and rendering — independent of the chat textarea. The text input is a simple string buffer with runes/backspace handling, not the full textarea widget.

**Files:**

- ~ internal/ui/question_prompt.go

**Snippet:**

```
// Update handles key events. Returns true if the interaction is complete (Enter pressed).
func (p *QuestionPrompt) Update(msg tea.KeyMsg) bool {
	n := len(p.Options)
	switch {
	// Navigation in selection mode
	case !p.TextMode && (key.Matches(msg, keys.Left) || key.Matches(msg, keys.Up)):
		if p.Type == QuestionCheckbox && n > 0 {
			// Toggle current option
			p.MultiSel[p.Selected] = !p.MultiSel[p.Selected]
		} else {
			p.Selected = (p.Selected - 1 + n) % n
		}
		return false
	case !p.TextMode && (key.Matches(msg, keys.Right) || key.Matches(msg, keys.Down)):
		if p.Type == QuestionCheckbox && n > 0 {
			p.MultiSel[p.Selected] = !p.MultiSel[p.Selected]
		} else {
			p.Selected = (p.Selected + 1) % n
		}
		return false
	// Switch to text mode
	case msg.Type == tea.KeyTab && !msg.Modifiers.Contains(tea.ModShift):
		p.TextMode = true
		return false
	// Switch back to selection
	case msg.Type == tea.KeyTab && msg.Modifiers.Contains(tea.ModShift):
		p.TextMode = false
		return false
	// Confirm
	case key.Matches(msg, keys.Send):
		return true
	// Cancel
	case msg.Type == tea.KeyEscape:
		return true // handled by caller as cancellation
	// Text input
	case p.TextMode:
		switch msg.Type {
		case tea.KeyRunes:
			p.TextInput += string(msg.Runes)
		case tea.KeyBackspace:
			runes := []rune(p.TextInput)
			if len(runes) > 0 {
				p.TextInput = string(runes[:len(runes)-1])
			}
		case tea.KeySpace:
			p.TextInput += " "
		}
		return false
	}
	return false
}
```

```
// Render produces the styled prompt block
func (p *QuestionPrompt) Render() string {
	var sb strings.Builder

	// Question text
	if p.Question != "" {
		sb.WriteString(lipgloss.NewStyle().Bold(true).Foreground(style.ColorHeading).Render(p.Question) + "
")
	}

	if p.TextMode {
		label := p.TextPrompt
		if label == "" { label = "Additional notes" }
		sb.WriteString(fmt.Sprintf("  %s: %s█
", label, p.TextInput))
		return sb.String()
	}

	// Options
	for i, opt := range p.Options {
		var prefix string
		cursor := "  "
		if i == p.Selected {
			cursor = "▸ "
		}
		switch p.Type {
		case QuestionCheckbox:
			if p.MultiSel[i] {
				prefix = "☑ "
			} else {
				prefix = "☐ "
			}
		default:
			prefix = "  "
		}
		sb.WriteString(fmt.Sprintf("  %s%s%s
", cursor, prefix, opt))
	}

	// Hint
	sb.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("240")).Render(
		fmt.Sprintf("  [↑/↓] navigate  [Tab] %s  [Enter] confirm  [Esc] cancel",
			p.TextPromptIfEmpty("notes"))))

	return sb.String()
}
```

**Acceptance Criteria:**

- [ ] Update navigates options correctly for all 3 types, toggles checkbox, switches text mode with Tab/Shift+Tab, returns true on Enter/Escape. Render shows correct UI per type with cursor and checkbox state

**Verify:**

```bash
cd /home/goglue/src/squid-os && go build ./...
```

### 1.3. Refactor AuthorizationPrompt to delegate to QuestionPrompt

**Type:** refactor

**What:** Rewrite internal/ui/authorization.go so AuthorizationPrompt wraps QuestionPrompt internally (QuestionYesNo type) and adds tool-specific context (tool name, display value, destructive icon, diff preview) around it.

**Why:** Eliminates duplication — the authorization flow is just a specialized use of the question component with yes/no + optional text. The existing yes/no selection and text mode logic moves into the shared component.

**Files:**

- ~ internal/ui/authorization.go

**Snippet:**

```
// AuthorizationPrompt wraps QuestionPrompt for tool authorization context
type AuthorizationPrompt struct {
	*q.QuestionPrompt // embedded - delegates selection and text handling
	ToolName      string
	DisplayValue  string
	IsDestructive bool
	PreviewDiff   string
}

func NewAuthorizationPrompt(toolName, displayValue string, isDestructive bool, diff string, width int) *AuthorizationPrompt {
	qp := q.NewQuestionPrompt(q.QuestionYesNo, "", []string{"Yes", "No"}, width)
	qp.TextPrompt = "Additional instructions for the AI"
	return &AuthorizationPrompt{
		QuestionPrompt: qp,
		ToolName:       toolName,
		DisplayValue:   displayValue,
		IsDestructive:  isDestructive,
		PreviewDiff:    diff,
	}
}

func (p *AuthorizationPrompt) Render() string {
	var sb strings.Builder
	// Tool-specific header (icon, name, diff)
	// ...
	// Then delegate to QuestionPrompt for options + text
	sb.WriteString(p.QuestionPrompt.Render())
	return sb.String()
}

func (p *AuthorizationPrompt) HandleKey(msg tea.KeyMsg) bool {
	return p.QuestionPrompt.Update(msg)
}

func (p *AuthorizationPrompt) GetApproved() bool {
	return p.QuestionPrompt.Selected == 0 // Yes
}

func (p *AuthorizationPrompt) GetInstructions() string {
	return p.QuestionPrompt.GetTextAnswer()
}
```

**Acceptance Criteria:**

- [ ] AuthorizationPrompt compiles, delegates selection/text to QuestionPrompt, adds tool-specific rendering on top. No duplicate selection logic.

**Verify:**

```bash
cd /home/goglue/src/squid-os && go build ./...
```

### 1.4. Update app authorization flow to use refactored AuthorizationPrompt

**Type:** refactor

**What:** Update internal/app/app.go, input.go, and authorization.go to use the new AuthorizationPrompt pointer with embedded QuestionPrompt. Adjust handleAuthorizeKey and setAuthMode to call the delegated methods.

**Why:** Wires the refactored component back into the existing authorization flow without changing user-facing behavior.

**Files:**

- ~ internal/app/app.go
- ~ internal/app/input.go
- ~ internal/app/authorization.go

**Snippet:**

```
// app.go
	authPrompt *ui.AuthorizationPrompt

// input.go - handleAuthorizeKey simplifies to:
func (m Model) handleAuthorizeKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.authPrompt.HandleKey(msg) {
		// Enter or Escape
		if msg.Type == tea.KeyEscape {
			return m.resolveAuthorization(false, "")
		}
		approved := m.authPrompt.GetApproved()
		instructions := m.authPrompt.GetInstructions()
		return m.resolveAuthorization(approved, instructions)
	}
	return m, nil
}
```

**Acceptance Criteria:**

- [ ] Authorization flow behavior is identical to before — yes/no works, Tab switches to text, Enter confirms, Escape rejects. Code compiles cleanly.

**Verify:**

```bash
cd /home/goglue/src/squid-os && go build ./...
```

---

## 2. Ask Tool

- **Pattern:** Tool Schema / Interrupt Mode

**Objective:** Add an ask_tool that the LLM uses to collect structured user input through an array of questions. The tool returns structured answers to the AI for interviews, clarifications, and data collection.

**Success Criteria:** LLM can call ask_tool with an array of questions (each specifying type, text, options). The UI pauses in a new ModeAsk, renders each question sequentially using QuestionPrompt, collects answers, and returns them as structured JSON to the AI. The AI receives answers + any supplementary text the user added.

```mermaid
LLM → ask_tool({questions: [{type, text, options}]}) → app enters ModeAsk → iterates questions one by one using QuestionPrompt → user answers each → collects AnswerSet → returns {answers: [{answer, text}], instruction: string} to AI as tool result
```

### 2.1. Define ask_tool schema, types, and AnswerSet

**Type:** feature

**What:** Create internal/tools/ask_tool.go with the ask_tool schema, Question struct (type, text, options), and AnswerSet for collecting results. Add the tool to the registry.

**Why:** The LLM needs a structured schema to define what questions to ask. The AnswerSet collects all responses before returning them as the tool result.

**Files:**

- + internal/tools/ask_tool.go

**Snippet:**

```
// Question represents a single question the AI wants to ask the user
type Question struct {
	Type     string   // "yesno" | "radio" | "checkbox"
	Text     string   // The question text
	Options  []string // Option labels (for radio/checkbox; ignored for yesno)
	TextHint string   // Label for the optional text field (e.g. "Other: ")
}

// QuestionAnswer holds the user's response to a single question
type QuestionAnswer struct {
	Answer interface{} // bool for yesno, int for radio, []int for checkbox
	Text   string      // supplementary text if user typed anything
}

// AskToolState tracks multi-question state during ModeAsk
type AskToolState struct {
	Questions []Question
	Answers   []QuestionAnswer
	CurrentQ  int // index of current question being shown
}

func NewAskToolState(questions []Question) *AskToolState {
	return &AskToolState{
		Questions: questions,
		Answers:   make([]QuestionAnswer, len(questions)),
		CurrentQ:  0,
	}
}

// GetAnswerMap returns answers as a structured map for the AI
func (a *AskToolState) GetAnswerMap() map[string]interface{} {
	answers := make([]map[string]interface{}, len(a.Questions))
	for i, q := range a.Questions {
		ans := map[string]interface{}{
			"question": q.Text,
			"type":     q.Type,
		}
		switch q.Type {
		case "yesno":
			ans["answer"] = a.Answers[i].Answer // bool
		case "radio":
			idx := a.Answers[i].Answer.(int)
			if idx >= 0 && idx < len(q.Options) {
				ans["answer"] = q.Options[idx]
			}
		case "checkbox":
			indices := a.Answers[i].Answer.([]int)
			selected := make([]string, 0, len(indices))
			for _, idx := range indices {
				if idx >= 0 && idx < len(q.Options) {
					selected = append(selected, q.Options[idx])
				}
			}
			ans["answer"] = selected
		}
		if a.Answers[i].Text != "" {
			ans["text"] = a.Answers[i].Text
		}
		answers[i] = ans
	}
	return map[string]interface{}{"answers": answers}
}
```

**Acceptance Criteria:**

- [ ] Schema is valid JSON, Question/Answer structs compile, GetAnswerMap returns properly formatted response with labels resolved

**Verify:**

```bash
cd /home/goglue/src/squid-os && go build ./...
```

### 2.2. Implement ask_tool with ModeAsk interrupt flow

**Type:** feature

**What:** Add ModeAsk to modes.go. Create the ask_tool in internal/tools/ask_tool.go that, instead of executing inline, signals the app to enter ModeAsk. Add askState *AskToolState and askPrompt *ui.QuestionPrompt to the Model. Implement handleAskKey that iterates questions, renders QuestionPrompt for each, and on completion returns structured answers.

**Why:** Unlike regular tools that compute and return a result, ask_tool needs to interrupt the stream and engage the user interactively across potentially multiple questions. The answers are collected and then returned as the tool's result, resuming the stream.

**Files:**

- ~ internal/app/modes.go
- ~ internal/app/app.go
- ~ internal/app/input.go
- ~ internal/app/stream.go
- ~ internal/tools/ask_tool.go

**Snippet:**

```
// modes.go
	ModeAsk // Awaiting structured user answers from ask_tool

// app.go
	askState   *tools.AskToolState
	askPrompt  *ui.QuestionPrompt

// stream.go - in executeTools, when tool name is "ask_tool":
if p.name == "ask_tool" {
	// Parse questions from args
	var questions []tools.Question
	if err := json.Unmarshal([]byte(p.args), &questions); err != nil {
		return config.ToolCallEntry{...error...}
	}
	m.askState = tools.NewAskToolState(questions)
	m.setupAskPrompt() // creates QuestionPrompt for first question
	m.mode = ModeAsk
	m.stream.active = false
	return nil // pause stream
}

func (m *Model) setupAskPrompt() {
	q := m.askState.Questions[m.askState.CurrentQ]
	qt := mapQuestionType(q.Type)
	m.askPrompt = ui.NewQuestionPrompt(qt, q.Text, q.Options, m.width)
	if q.TextHint != "" {
		m.askPrompt.TextPrompt = q.TextHint
	}
}

func mapQuestionType(t string) ui.QuestionType {
	switch t {
	case "yesno": return ui.QuestionYesNo
	case "radio": return ui.QuestionRadio
	case "checkbox": return ui.QuestionCheckbox
	default: return ui.QuestionYesNo
	}
}
```

```
// input.go - handleAskKey
func (m Model) handleAskKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.askPrompt.Update(msg) {
		// Question answered
		ans := m.askPrompt.GetAnswers()
		m.askState.Answers[m.askState.CurrentQ] = tools.QuestionAnswer{
			Answer: ans["answer"],
			Text:   m.askPrompt.GetTextAnswer(),
		}
		
		m.askState.CurrentQ++
		if m.askState.CurrentQ >= len(m.askState.Questions) {
			// All questions answered - return result and resume
			return m.completeAsk()
		}
		m.setupAskPrompt()
		return m, nil
	}
	return m, nil
}

func (m *Model) completeAsk() (tea.Model, tea.Cmd) {
	// Build tool result with answers
	resultJSON, _ := json.Marshal(m.askState.GetAnswerMap())
	// Append assistant message with tool result, then resume stream
	m.askState = nil
	m.askPrompt = nil
	m.mode = ModeStreaming
	return m, m.startStream()
}
```

**Acceptance Criteria:**

- [ ] ModeAsk compiles, ask_tool enters multi-question flow from executeTools, questions are answered sequentially, completion returns structured JSON and resumes stream

**Verify:**

```bash
cd /home/goglue/src/squid-os && go build ./...
```

### 2.3. Add ask_tool to system prompt

**Type:** infra

**What:** Add ask_tool usage instructions to the system prompt in internal/config/sys-prompt.go explaining the schema (questions array with type, text, options) and when to use it (interviews, clarifications, structured data collection).

**Why:** The LLM needs explicit instructions on how to structure ask_tool calls and when it's appropriate versus regular conversation.

**Files:**

- ~ internal/config/sys-prompt.go

**Snippet:**

```
## Ask Tool
- Use ask_tool when you need to collect structured information from the user through a series of questions.
- Ideal for interviews, preference gathering, decision trees, or any multi-step data collection.
- Call with: ask_tool({questions: [{type, text, options}]})
  - type: "yesno" for binary questions, "radio" for single-choice from a list, "checkbox" for multi-select
  - text: the question to display
  - options: array of option labels (required for radio/checkbox, ignored for yesno)
  - textHint: optional label for the user's supplementary text field
- The user can navigate with ↑/↓ (or ←/→), toggle checkbox with same keys, press Tab for extra notes, Enter to confirm each question.
- Results are returned as: {answers: [{question, type, answer, text?}]}
  - answer is a boolean for yesno, a string (option label) for radio, an array of strings for checkbox
  - text is only present if the user typed supplementary notes
```

**Acceptance Criteria:**

- [ ] System prompt contains clear ask_tool instructions with examples for each question type

**Verify:**

```bash
cd /home/goglue/src/squid-os && go build ./...
```

---

## 3. UI Integration

- **Pattern:** View Composition / Mode Switching

**Objective:** Integrate both ModeAsk and the refactored ModeAuthorize into the render pipeline so the question prompt displays properly in both contexts.

**Success Criteria:** ModeAsk renders QuestionPrompt with question text and progress indicator (e.g. 1/3). ModeAuthorize renders the tool-specific header + QuestionPrompt below it. Neither shows the chat textarea. Both return to normal flow on completion.

```mermaid
View() → switch mode → ModeAsk: render progress header + askPrompt.Render() + skip textarea. ModeAuthorize: render tool header + authPrompt.Render() + skip textarea. Input dispatch routes to handleAskKey / handleAuthorizeKey accordingly.
```

### 3.1. Wire ModeAsk and ModeAuthorize into render and input dispatch

**Type:** feature

**What:** Update render.go to handle ModeAsk (show progress indicator + QuestionPrompt) and refactored ModeAuthorize (show AuthorizationPrompt with embedded QuestionPrompt). Update input.go dispatch. Ensure chat textarea is hidden during both modes.

**Why:** Completes the visual and interactive integration — users see the question prompts rendered at the bottom of the viewport, with clear progress and navigation hints.

**Files:**

- ~ internal/app/render.go
- ~ internal/app/input.go

**Snippet:**

```
// render.go
case ModeAsk:
	if m.askState != nil && m.askPrompt != nil {
		progress := fmt.Sprintf("Question %d/%d", m.askState.CurrentQ+1, len(m.askState.Questions))
		sections = append(sections, lipgloss.NewStyle().Foreground(lipgloss.Color("12")).Bold(true).Render(progress) + "
")
		sections = append(sections, m.askPrompt.Render())
	}
	// Skip textarea
	return...

case ModeAuthorize:
	if m.authPrompt != nil {
		sections = append(sections, m.authPrompt.Render())
	}
	// Skip textarea
	return...
```

**Acceptance Criteria:**

- [ ] Both modes render correctly in View, textarea is hidden, input routes to correct handler, go build passes

**Verify:**

```bash
cd /home/goglue/src/squid-os && go build ./...
```

---

## 4. Stream Resumption

- **Pattern:** State Machine / Deferred Execution

**Objective:** Properly resume the stream after ask_tool collects all answers — saving the tool result and triggering the next AI turn with the structured answers.

**Success Criteria:** After the last question is answered, the tool result containing all answers is saved as a tool call entry, the assistant message is appended, and the stream resumes so the AI receives the answers in its next context window.

```mermaid
completeAsk() → build ToolResult with JSON answers → create ToolCallEntry with success status → append assistant message with tool result → reset ask state → transition to ModeStreaming → startStream()
```

### 4.1. Implement completeAsk stream resumption

**Type:** feature

**What:** Complete the completeAsk method: build the tool result with structured answers JSON, save it as a tool call entry on the assistant message, clean up ask state, and resume the stream.

**Why:** This is the critical bridge — the AI needs to receive the collected answers as if they were a normal tool result, so it can continue the conversation with the structured data.

**Files:**

- ~ internal/app/stream.go
- ~ internal/tools/ask_tool.go

**Snippet:**

```
// In ask_tool.go - provide the tool struct:
var AskTool = Tool{
	Name:        "ask_tool",
	Description: "Capture structured answers from the user through a series of questions. Returns answers to the AI for further processing.",
	Schema: []byte(),
	// Execute is a placeholder — real execution happens via ModeAsk interrupt
	Execute: func(args map[string]interface{}) ToolResult {
		return ToolResult{Status: ResultStatusError, Error: "ask_tool requires interactive mode"}
	},
}

// In stream.go - completeAsk:
func (m *Model) completeAsk() (tea.Model, tea.Cmd) {
	answersJSON, _ := json.Marshal(m.askState.GetAnswerMap())
	
	// Build the tool call entry for the assistant message
	entry := config.ToolCallEntry{
		ToolCall: config.ToolCall{
			Name: "ask_tool",
			Args: m.stream.partialTools[0].args,
		},
		Execution: tools.ToolResult{
			Status: tools.ResultStatusSuccess,
			Result: string(answersJSON),
		},
	}
	
	m.appendAssistantMsg(config.Message{
		ID:       fmt.Sprintf("msg_%d", len(m.session.file.Messages)+1),
		Role:     config.RoleAssistant,
		ToolCalls: []config.ToolCallEntry{entry},
		StopReason: "tool_calls",
	})
	
	m.askState = nil
	m.askPrompt = nil
	m.stream.reset()
	m.updateViewportContent()
	return m, m.startStream()
}
```

**Acceptance Criteria:**

- [ ] completeAsk builds correct tool result JSON, appends assistant message, cleans state, and resumes stream. Build passes.

**Verify:**

```bash
cd /home/goglue/src/squid-os && go build ./...
```
