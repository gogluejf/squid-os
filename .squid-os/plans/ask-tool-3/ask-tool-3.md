# Ask Tool — Reusable Question Component

## Core Problem

The AI needs a UX-friendly way to collect structured information from users (interviews, confirmations, multi-choice questions). Currently tool authorization has its own prompt pattern. We want a reusable Question component that handles yes/no, radio, checkbox, and free-text inputs — with tab/shift-tab navigation between selection and contextual text input. The authorization mode will be refactored to use this same component, eliminating duplicate prompt logic.

## Goal

A generic Question component used by both the new ask-tool and the refactored authorization mode. Sequential wizard UI for multi-question interviews. Clean separation: Question component handles input, callers provide questions and receive answers.

---

## 1. Question Domain Model

- **Pattern:** Value Object / Enum

**Objective:** Define the data structures for questions, answers, and the question types supported by the component.

**Success Criteria:** QuestionType enum, Question struct with type-specific fields, Answer struct with value + text, and validation logic compile cleanly.

```mermaid
Question { id, type, label, options[], requiresText(bool per option) } -> Answer { value, text } -> Result = [Answer]
```

### 1.1. Question types, Question struct, and Answer struct

**Type:** feature

**What:** Add QuestionType enum (yesno, radio, checkbox, text), Question struct, and Answer struct to internal/tools/ask/ask_types.go.

**Why:** Core domain types that define what a question looks like and how answers are structured. Every question has an ID, type, label, and type-specific fields. Every answer has a value and optional text.

**Files:**

- + internal/tools/ask/ask_types.go

**Snippet:**

```
package ask

import "encoding/json"

type QuestionType string

const (
	QuestionYesNo    QuestionType = "yesno"
	QuestionRadio    QuestionType = "radio"
	QuestionCheckbox QuestionType = "checkbox"
	QuestionText     QuestionType = "text"
)

type Question struct {
	ID          string       
	Type        QuestionType 
	Label       string                 // prompt shown to user
	Options     []Option             // radio/checkbox choices
	Default     interface{}          // default selection (index int or value string)
	TextPrompt  string            // label for the text field (per-question fallback)
	Required    bool                // must answer before proceeding
	Multiple    bool                // checkbox: allow multiple selections
}

type Option struct {
	Value       string 
	Label       string 
	TextPrompt  string    // overrides question-level textPrompt when this option is selected
	RequiresText bool   // shows text input when this option is selected
}

type Answer struct {
	Value string         // "yes","no", option value, or free text
	Text  string           // additional context from text input
	Values []string      // checkbox: multiple selected values
}

func (a Answer) IsEmpty() bool {
	switch {
	case a.Value != "":
		return false
	case len(a.Values) > 0:
		return false
	default:
		return true
	}
}

func (q Question) NeedsTextInput() bool {
	return q.Type == QuestionText
}

func (q Question) GetOptionTextPrompt(selectedValue string) string {
	for _, opt := range q.Options {
		if opt.Value == selectedValue && opt.TextPrompt != "" {
			return opt.TextPrompt
		}
	}
	return q.TextPrompt
}

func (q Question) GetOptionRequiresText(selectedValue string) bool {
	for _, opt := range q.Options {
		if opt.Value == selectedValue {
			return opt.RequiresText
		}
	}
	return false
}
```

**Acceptance Criteria:**

- [ ] QuestionType enum has 4 values
- [ ] Question struct has ID, Type, Label, Options, Default, TextPrompt, Required, Multiple fields
- [ ] Answer struct has Value, Text, Values fields
- [ ] IsEmpty returns true only when all fields are empty
- [ ] GetOptionTextPrompt returns option-specific prompt or falls back to question-level
- [ ] GetOptionRequiresText returns requiresText for the selected option

**Verify:**

```bash
cd /home/goglue/src/squid-os && go build ./...
```

### 1.2. Question validation and serialization helpers

**Type:** feature

**What:** Add Validate method on Question and JSON marshal/unmarshal helpers for the ask-tool tool schema in internal/tools/ask/ask_schema.go.

**Why:** The AI sends questions as JSON args to the tool. We need to validate the structure matches expectations and provide a clean schema definition.

**Files:**

- + internal/tools/ask/ask_schema.go

**Snippet:**

```
package ask

// GetSchema returns the JSON schema for the ask tool arguments.
func GetSchema() string {
  // Returns schema: { questions: array of Question objects }
  // questions is required, each must have id, type, label
  // type determines which additional fields are needed
}

func (q Question) Validate() error {
  switch q.Type {
  case QuestionYesNo:
    // no options needed
  case QuestionRadio, QuestionCheckbox:
    if len(q.Options) == 0 {
      return ErrMissingOptions
    }
  case QuestionText:
    // no options needed
  default:
    return ErrUnknownType(q.Type)
  }
  return nil
}

func (q Question) HasTextCapability() bool {
  switch q.Type {
  case QuestionYesNo, QuestionRadio, QuestionCheckbox:
    return q.TextPrompt != "" || anyOptionRequiresText(q.Options)
  case QuestionText:
    return true
  default:
    return false
  }
}

func anyOptionRequiresText(opts []Option) bool {
  for _, o := range opts {
    if o.RequiresText {
      return true
    }
  }
  return false
}
```

**Acceptance Criteria:**

- [ ] Validate returns error for radio/checkbox with no options
- [ ] Validate returns error for unknown question type
- [ ] HasTextCapability checks both question-level and option-level text prompts
- [ ] GetSchema returns valid JSON schema for the ask tool

**Verify:**

```bash
cd /home/goglue/src/squid-os && go build ./...
```

---

## 2. Question UI Component

- **Pattern:** Stateful TUI Component / Wizard Pattern

**Objective:** Build the reusable QuestionPrompt UI component that renders a single question, handles keyboard input (left/right for selection, tab/shift-tab for text navigation, enter to confirm), and tracks whether the answer is complete.

**Success Criteria:** QuestionPrompt component renders all 4 question types, handles navigation between selection and text input, validates required fields, and exposes IsComplete() and GetAnswer() methods.

```mermaid
QuestionPrompt { question, selectionIndex, textMode, textInput, answers[] } <-left/right-> toggle selection <-tab-> text mode <-shift+tab-> selection mode <-enter-> submit answer, advance to next question
```

### 2.1. QuestionPrompt component struct and yes/no rendering

**Type:** feature

**What:** Create internal/ui/question_prompt.go with QuestionPrompt struct and Render method handling yes/no question display and selection state.

**Why:** The core UI component. Starts with yes/no as the simplest case — renders label, two selectable buttons, optional text input. Tracks selection index and text mode state.

**Files:**

- + internal/ui/question_prompt.go

**Snippet:**

```
package ui

import "github.com/charmbracelet/lipgloss"

type QuestionPrompt struct {
  Question       ask.Question
  Selection      int       // index of selected option (0=yes, 1=no for yesno; option index for radio/checkbox)
  TextMode       bool      // true = cursor in text input
  TextInput      string
  SelectedValues []int     // checkbox: indices of selected options
  Width          int
}

func NewQuestionPrompt(q ask.Question, width int) *QuestionPrompt {
  qp := &QuestionPrompt{Question: q, Width: width}
  if q.Type == ask.QuestionCheckbox {
    qp.SelectedValues = []int{}
  }
  return qp
}

func (qp *QuestionPrompt) Render() string {
  var sb strings.Builder
  // 1. Question label
  sb.WriteString(style.Bold.Render(qp.Question.Label) + "

")
  
  switch qp.Question.Type {
  case ask.QuestionYesNo:
    sb.WriteString(qp.renderYesNo())
  case ask.QuestionRadio:
    sb.WriteString(qp.renderRadio())
  case ask.QuestionCheckbox:
    sb.WriteString(qp.renderCheckbox())
  case ask.QuestionText:
    sb.WriteString(qp.renderText())
  }
  
  // 2. Text input (if applicable and visible)
  if qp.shouldShowText() {
    prompt := qp.Question.GetOptionTextPrompt(qp.getCurrentValue())
    if prompt == "" {
      prompt = "Add details..."
    }
    sb.WriteString("
  " + style.Dim.Render(prompt) + "
")
    sb.WriteString(qp.renderTextInput())
  }
  
  // 3. Navigation hints
  sb.WriteString(qp.renderHints())
  
  return lipgloss.NewStyle().Width(qp.Width).Render(sb.String())
}

func (qp *QuestionPrompt) shouldShowText() bool {
  if qp.Question.Type == ask.QuestionText {
    return false // text IS the question, handled in renderText
  }
  if !qp.Question.HasTextCapability() {
    return false
  }
  // For checkbox, show text if any selected option requires it
  if qp.Question.Type == ask.QuestionCheckbox {
    for _, idx := range qp.SelectedValues {
      if idx < len(qp.Question.Options) && qp.Question.Options[idx].RequiresText {
        return true
      }
    }
    return false
  }
  // For yesno/radio, check current selection
  return qp.Question.GetOptionRequiresText(qp.getCurrentValue())
}

func (qp *QuestionPrompt) getCurrentValue() string {
  if qp.Question.Type == ask.QuestionYesNo {
    return map[int]string{0: "yes", 1: "no"}[qp.Selection]
  }
  if qp.Selection < len(qp.Question.Options) {
    return qp.Question.Options[qp.Selection].Value
  }
  return ""
}

func (qp *QuestionPrompt) renderYesNo() string {
  yesStyle, noStyle := style.SelectionStyle, style.Dim
  if qp.Selection == 0 && !qp.TextMode {
    yesStyle = style.SelectedStyle
  } else if qp.Selection == 1 && !qp.TextMode {
    noStyle = style.SelectedStyle
  }
  return "  " + yesStyle.Render("Yes") + "  /  " + noStyle.Render("No")
}

func (qp *QuestionPrompt) renderHints() string {
  var hints []string
  switch qp.Question.Type {
  case ask.QuestionYesNo, ask.QuestionRadio:
    hints = append(hints, "←/→ select")
  case ask.QuestionCheckbox:
    hints = append(hints, "←/→ navigate, Space toggle")
  case ask.QuestionText:
    hints = append(hints, "type freely")
  }
  if qp.shouldShowText() {
    if qp.TextMode {
      hints = append(hints, "Shift+Tab back")
    } else {
      hints = append(hints, "Tab details")
    }
  }
  hints = append(hints, "Enter confirm")
  return "
  " + style.Dim.Render(strings.Join(hints, "  ·  "))
}
```

**Acceptance Criteria:**

- [ ] QuestionPrompt struct has Selection, TextMode, TextInput, SelectedValues, Width fields
- [ ] NewQuestionPrompt initializes based on question type
- [ ] Render delegates to type-specific renderers
- [ ] shouldShowText checks option-level RequiresText and question-level HasTextCapability
- [ ] getCurrentValue returns correct value for yesno and radio types
- [ ] Hints adapt based on question type and text mode state

**Verify:**

```bash
cd /home/goglue/src/squid-os && go build ./...
```

### 2.2. Radio, checkbox, and text rendering plus keyboard handling

**Type:** feature

**What:** Add renderRadio, renderCheckbox, renderText methods and HandleKey for all question types in internal/ui/question_prompt.go.

**Why:** Complete the UI component with all 4 question type renderers and the keyboard input handler covering left/right selection, space for checkbox toggle, tab/shift-tab navigation, and enter to confirm.

**Files:**

- ~ internal/ui/question_prompt.go

**Snippet:**

```
func (qp *QuestionPrompt) renderRadio() string {
  var sb strings.Builder
  for i, opt := range qp.Question.Options {
    prefix := "  ○ "
    if i == qp.Selection && !qp.TextMode {
      prefix = "  ● "
    }
    label := opt.Label
    if opt.Label == "" { label = opt.Value }
    if i == qp.Selection && !qp.TextMode {
      sb.WriteString(prefix + style.SelectedStyle.Render(label))
    } else {
      sb.WriteString(prefix + style.Dim.Render(label))
    }
    if i < len(qp.Question.Options)-1 { sb.WriteString("
") }
  }
  return sb.String()
}

func (qp *QuestionPrompt) renderCheckbox() string {
  var sb strings.Builder
  selectedSet := make(map[int]bool)
  for _, idx := range qp.SelectedValues { selectedSet[idx] = true }
  for i, opt := range qp.Question.Options {
    prefix := "  □ "
    if selectedSet[i] { prefix = "  ☑ " }
    cursor := "  "
    if i == qp.Selection && !qp.TextMode { cursor = "→ " }
    label := opt.Label
    if opt.Label == "" { label = opt.Value }
    sb.WriteString(cursor + prefix + style.Dim.Render(label))
    if i < len(qp.Question.Options)-1 { sb.WriteString("
") }
  }
  return sb.String()
}

func (qp *QuestionPrompt) renderText() string {
  placeholder := qp.Question.TextPrompt
  if placeholder == "" { placeholder = "Type your answer..." }
  content := qp.TextInput
  if content == "" { content = style.Placeholder.Render(placeholder) }
  return "  " + style.InputStyle.Render(content)
}

func (qp *QuestionPrompt) HandleKey(msg tea.KeyMsg) (complete bool, answer ask.Answer) {
  if qp.Question.Type == ask.QuestionText {
    return qp.handleTextKey(msg)
  }
  
  if qp.TextMode {
    return qp.handleTextModeKey(msg)
  }
  
  // Selection mode
  switch {
  case key.Matches(msg, keys.Left):
    qp.moveSelection(-1)
  case key.Matches(msg, keys.Right):
    qp.moveSelection(1)
  case msg.Type == tea.KeySpace && qp.Question.Type == ask.QuestionCheckbox:
    qp.toggleSelection()
  case msg.Type == tea.KeyTab && !msg.Modifiers.Contains(tea.ModShift):
    if qp.shouldShowText() { qp.TextMode = true }
  case msg.Type == tea.KeyTab && msg.Modifiers.Contains(tea.ModShift):
    qp.TextMode = false
  case key.Matches(msg, keys.Send):
    return true, qp.buildAnswer()
  }
  return false, ask.Answer{}
}

func (qp *QuestionPrompt) handleTextModeKey(msg tea.KeyMsg) (bool, ask.Answer) {
  switch {
  case msg.Type == tea.KeyTab && msg.Modifiers.Contains(tea.ModShift):
    qp.TextMode = false
  case key.Matches(msg, keys.Send):
    return true, qp.buildAnswer()
  default:
    // Handle runes, backspace, etc for text input
    qp.handleTextInput(msg)
  }
  return false, ask.Answer{}
}

func (qp *QuestionPrompt) handleTextKey(msg tea.KeyMsg) (bool, ask.Answer) {
  if key.Matches(msg, keys.Send) {
    return qp.Question.Required == false || qp.TextInput != "", 
      ask.Answer{Value: qp.TextInput}
  }
  qp.handleTextInput(msg)
  return false, ask.Answer{}
}

func (qp *QuestionPrompt) moveSelection(delta int) {
  maxIdx := 1 // yesno
  if qp.Question.Type == ask.QuestionRadio || qp.Question.Type == ask.QuestionCheckbox {
    maxIdx = len(qp.Question.Options) - 1
  }
  qp.Selection = max(0, min(qp.Selection+delta, maxIdx))
}

func (qp *QuestionPrompt) toggleSelection() {
  idx := qp.Selection
  exists := false
  for _, i := range qp.SelectedValues { if i == idx { exists = true; break } }
  if exists {
    qp.SelectedValues = removeInt(qp.SelectedValues, idx)
  } else {
    qp.SelectedValues = append(qp.SelectedValues, idx)
  }
}

func (qp *QuestionPrompt) buildAnswer() ask.Answer {
  switch qp.Question.Type {
  case ask.QuestionYesNo:
    return ask.Answer{
      Value: map[int]string{0: "yes", 1: "no"}[qp.Selection],
      Text:  qp.TextInput,
    }
  case ask.QuestionRadio:
    val := ""
    if qp.Selection < len(qp.Question.Options) {
      val = qp.Question.Options[qp.Selection].Value
    }
    return ask.Answer{Value: val, Text: qp.TextInput}
  case ask.QuestionCheckbox:
    var values []string
    for _, idx := range qp.SelectedValues {
      if idx < len(qp.Question.Options) {
        values = append(values, qp.Question.Options[idx].Value)
      }
    }
    return ask.Answer{Values: values, Text: qp.TextInput}
  default:
    return ask.Answer{}
  }
}
```

**Acceptance Criteria:**

- [ ] renderRadio shows filled circle for selected option
- [ ] renderCheckbox shows checked box for selected values with cursor navigation
- [ ] renderText shows placeholder or user input
- [ ] HandleKey routes to correct handler based on type and text mode
- [ ] Left/right cycles selection within bounds
- [ ] Space toggles checkbox selection
- [ ] Tab enters text mode only if shouldShowText is true
- [ ] Shift+Tab exits text mode
- [ ] Enter returns complete=true with the built answer
- [ ] buildAnswer returns correct structure for all 4 question types

**Verify:**

```bash
cd /home/goglue/src/squid-os && go build ./...
```

---

## 3. Ask Tool

- **Pattern:** Tool Definition / Sequential Wizard

**Objective:** Create the ask tool that accepts an array of Question objects from the AI, iterates through them sequentially using the QuestionPrompt component, and returns an array of Answers.

**Success Criteria:** The ask tool is registered in the tool registry, accepts a questions array, iterates through each question one at a time, and returns structured answers as JSON.

```mermaid
LLM -> ask({questions: [...]}) -> ModeAsk -> iterate questions one by one -> QuestionPrompt for each -> collect Answers[] -> return to AI as tool result
```

### 3.1. Ask tool definition and schema

**Type:** feature

**What:** Create internal/tools/ask/ask.go with the Tool definition, GetSchema, DisplayValue, and Execute that parses questions from args and stores them in the ask context.

**Why:** Registers the ask tool in the tool system. The tool takes a questions array, validates each question, and stores them for sequential rendering.

**Files:**

- + internal/tools/ask/ask.go

**Snippet:**

```
package ask

import (
  "encoding/json"
  "github.com/goglue/squid-os/internal/tools"
)

type AskContext struct {
  Questions []Question
  Answers   []Answer
  CurrentQ  int // index of current question being displayed
}

func GetTool(reg *tools.ToolRegistry) {
  tool := tools.Tool{
    Name:        "ask",
    Description: "Present questions to the user and collect structured answers. Supports yes/no, radio, checkbox, and free-text questions.",
    Schema:      GetSchema(),
    DisplayValue: func(args string) string {
      var req struct {
        Questions []Question 
      }
      json.Unmarshal([]byte(args), &req)
      if len(req.Questions) == 1 {
        return req.Questions[0].Label
      }
      return fmt.Sprintf("%d questions", len(req.Questions))
    },
    Execute: ExecuteAsk,
  }
  reg.Register(tool)
}

func ExecuteAsk(args map[string]interface{}, ctx *tools.ExecutionContext) tools.ToolResult {
  questionsJSON, ok := args["questions"].(string)
  if !ok {
    // Handle array form too
  }
  var questions []Question
  if err := json.Unmarshal([]byte(questionsJSON), &questions); err != nil {
    // Also try direct array parse
  }
  // Validate all questions
  for _, q := range questions {
    if err := q.Validate(); err != nil {
      return tools.ToolResult{
        Status: tools.ResultStatusError,
        Error:  fmt.Sprintf("Invalid question %s: %v", q.ID, err),
      }
    }
  }
  // This returns a special "pending" result that signals the UI to enter ModeAsk
  return tools.ToolResult{
    Status: tools.ResultStatusSuccess,
    Data:   json.Marshal(map[string]interface{}{"questions": questions}),
    Pending: true, // signals interactive mode needed
  }
}
```

**Acceptance Criteria:**

- [ ] Tool registers with name 'ask'
- [ ] Schema accepts questions array
- [ ] DisplayValue shows single question label or count of questions
- [ ] Execute validates all questions before entering interactive mode
- [ ] Execute returns pending=true to signal interactive UI mode

**Verify:**

```bash
cd /home/goglue/src/squid-os && go build ./...
```

### 3.2. ModeAsk state and sequential wizard integration

**Type:** feature

**What:** Add ModeAsk to modes.go, askCtx to streamState, and handleAskKey in input.go that iterates questions sequentially using QuestionPrompt, collecting answers until all are complete.

**Why:** Wires the ask tool into the app flow. ModeAsk pauses the stream, shows questions one at a time via QuestionPrompt, collects answers, and returns the full answer array as the tool result when done.

**Files:**

- ~ internal/app/modes.go
- ~ internal/app/stream.go
- ~ internal/app/input.go
- ~ internal/app/app.go

**Snippet:**

```
// modes.go
const (
  // ... existing ...
  ModeAsk // awaiting user answers to ask tool questions
)

// stream.go — in streamState
type streamState struct {
  // ... existing ...
  askCtx *ask.AskContext // non-nil when in ModeAsk
}

// app.go — in Model
func (m *Model) setAskMode(questions []ask.Question) {
  m.mode = ModeAsk
  m.stream.active = false
  m.stream.askCtx = &ask.AskContext{
    Questions: questions,
    Answers:   make([]ask.Answer, len(questions)),
    CurrentQ:  0,
  }
  m.askPrompt = ui.NewQuestionPrompt(questions[0], m.width)
}

func (m *Model) handleAskKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
  ctx := m.stream.askCtx
  complete, answer := m.askPrompt.HandleKey(msg)
  if !complete {
    return m, nil
  }
  
  // Store answer for current question
  ctx.Answers[ctx.CurrentQ] = answer
  
  // Check if question is valid (required fields filled)
  if ctx.Questions[ctx.CurrentQ].Required && answer.IsEmpty() {
    return m, nil // stay on this question
  }
  
  // Advance to next question or finish
  ctx.CurrentQ++
  if ctx.CurrentQ >= len(ctx.Questions) {
    return m.finishAsk(), nil
  }
  m.askPrompt = ui.NewQuestionPrompt(ctx.Questions[ctx.CurrentQ], m.width)
  return m, nil
}

func (m *Model) finishAsk() (tea.Model, tea.Cmd) {
  ctx := m.stream.askCtx
  resultJSON, _ := json.Marshal(map[string]interface{}{
    "answers": ctx.Answers,
  })
  
  // Return as tool result and resume stream
  m.stream.askCtx = nil
  // ... save tool result, resume stream ...
  return m, m.startStream()
}

// render.go
case ModeAsk:
  if m.stream.askCtx != nil {
    progress := fmt.Sprintf("Question %d/%d", m.stream.askCtx.CurrentQ+1, len(m.stream.askCtx.Questions))
    sections = append(sections, style.Dim.Render(progress))
    sections = append(sections, m.askPrompt.Render())
  }
  // No textarea in ask mode
```

**Acceptance Criteria:**

- [ ] ModeAsk added to mode enum
- [ ] setAskMode initializes AskContext and first QuestionPrompt
- [ ] handleAskKey processes input, stores answers, advances questions
- [ ] Required questions block advancement until answered
- [ ] finishAsk marshals answers and resumes stream
- [ ] Render shows progress indicator and question prompt

**Verify:**

```bash
cd /home/goglue/src/squid-os && go build ./...
```

### 3.3. Wire ask tool into tool execution and stream resume

**Type:** feature

**What:** Add ask tool to the tool registry initialization and wire ModeAsk into handleStreamEvent so that when ask returns pending, the app enters ModeAsk instead of immediately returning a result.

**Why:** Completes the integration: the ask tool gets invoked like any other tool, but its pending result transitions to interactive ModeAsk. After all questions are answered, the result is returned to the AI and the stream resumes with the answer data.

**Files:**

- ~ internal/app/stream.go
- ~ internal/tools/tools.go

**Snippet:**

```
// tools.go — registration
func init() {
  ask.GetTool(Registry)
  // ... other tools ...
}

// stream.go — in handleStreamEvent tool_calls section
if event.StopReason == "tool_calls" && len(m.stream.partialTools) > 0 {
  toolEntries := (&m).executeTools(m.stream.partialTools)
  if toolEntries == nil {
    // Check if it's authorization or ask mode
    if m.stream.authorizationCtx != nil {
      (&m).setAuthMode()
      m.updateViewportContent()
      return m, nil
    }
    if m.stream.askCtx != nil {
      m.updateViewportContent()
      return m, nil
    }
  }
  // ... existing flow ...
}

// finishAsk — resume with tool result
func (m *Model) finishAsk() (tea.Model, tea.Cmd) {
  ctx := m.stream.askCtx
  answersJSON, _ := json.Marshal(ctx.Answers)
  
  // Create the tool result entry
  entry := m.buildEmptyEntry(m.stream.partialTools[0])
  entry.Execution.Status = tools.ResultStatusSuccess
  entry.Execution.Content = string(answersJSON)
  
  // Save assistant message with tool result
  (&m).appendAssistantMsg(config.Message{
    Role:      config.RoleAssistant,
    ToolCalls: []config.ToolCallEntry{entry},
    StopReason: "tool_calls",
  })
  
  m.stream.askCtx = nil
  m.stream.reset()
  m.mode = ModeChat
  m.updateViewportContent()
  return &m, (&m).startStream()
}
```

**Acceptance Criteria:**

- [ ] Ask tool is registered in tool registry init
- [ ] handleStreamEvent checks for askCtx pending state
- [ ] finishAsk creates proper tool result entry with answers JSON
- [ ] Stream resumes after all questions answered
- [ ] Tool result is returned to AI as structured answer array

**Verify:**

```bash
cd /home/goglue/src/squid-os && go build ./...
```

---

## 4. Authorization Refactor

- **Pattern:** Extract Common Component / Adapter

**Objective:** Refactor the existing authorization prompt to use the QuestionPrompt component instead of its own yes/no logic, eliminating duplicate prompt patterns.

**Success Criteria:** Authorization mode uses QuestionPrompt with a yesno question type. The old AuthorizationPrompt struct is removed. All authorization keyboard handling flows through QuestionPrompt.HandleKey.

```mermaid
AuthorizationContext -> creates Question(yesno) -> QuestionPrompt handles UI -> AuthResult extracted from Answer
```

### 4.1. Refactor authorization to use QuestionPrompt component

**Type:** refactor

**What:** Replace AuthorizationPrompt struct with QuestionPrompt in internal/app/app.go. Convert AuthResult to use Answer value. Update setAuthMode to create a yesno Question and initialize QuestionPrompt. Remove old authorization.go UI code.

**Why:** Eliminates the duplicate prompt pattern. Authorization becomes a specific use of the Question component with a yesno question type. The answer maps directly: yes=approved, no=rejected, text=instructions.

**Files:**

- ~ internal/app/app.go
- ~ internal/app/input.go
- ~ internal/app/render.go
- ~ internal/app/stream.go
- - internal/ui/authorization.go

**Snippet:**

```
// app.go — replace authPrompt field
type Model struct {
  // ...
  askPrompt *ui.QuestionPrompt // reused for both ModeAsk and ModeAuthorize
}

// setAuthMode — now creates a yesno question
func (m *Model) setAuthMode() {
  m.mode = ModeAuthorize
  m.stream.active = false
  ctx := m.stream.authorizationCtx
  
  // Build preview diff
  var previewText string
  if ctx != nil {
    tool := m.toolReg.Get(ctx.ToolName)
    if tool != nil && tool.Preview != nil {
      result := tool.Preview(ctx.Args)
      if result.Status == tools.ResultStatusSuccess && len(result.Files) > 0 {
        previewText = result.Files[0].Diff
      }
    }
  }
  
  // Create a yesno question for authorization
  q := ask.Question{
    ID:   "auth_" + ctx.ToolName,
    Type: ask.QuestionYesNo,
    Label: fmt.Sprintf("Execute %s?%s", ctx.ToolName, 
      func() string { if ctx.IsDestructive { return " ⚠️" } return "" }()),
    TextPrompt: "Add instructions (optional)...",
  }
  // Make options carry the instruction context
  q.Options = []ask.Option{
    {Value: "yes", Label: "Yes", RequiresText: true, TextPrompt: "Add instructions for the AI..."},
    {Value: "no", Label: "No", RequiresText: true, TextPrompt: "Reason (optional)..."},
  }
  
  m.askPrompt = ui.NewQuestionPrompt(q, m.width)
  m.askPrompt.PreviewDiff = previewText
}

// handleAuthorizeKey — now delegates to QuestionPrompt
func (m *Model) handleAuthorizeKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
  complete, answer := m.askPrompt.HandleKey(msg)
  if !complete {
    return m, nil
  }
  
  approved := answer.Value == "yes"
  instructions := answer.Text
  return m.resolveAuthorization(approved, instructions)
}

// Add PreviewDiff to QuestionPrompt struct
type QuestionPrompt struct {
  // ... existing fields ...
  PreviewDiff string // for authorization: shows diff preview
}

// In Render — after question, before hints
if qp.PreviewDiff != "" {
  // Show condensed diff preview
  lines := strings.Count(qp.PreviewDiff, "
")
  sb.WriteString("
  " + style.Dim.Render(fmt.Sprintf("%d lines changed", lines)) + "
")
}
```

**Acceptance Criteria:**

- [ ] AuthorizationPrompt struct is removed
- [ ] askPrompt field reused for both ModeAsk and ModeAuthorize
- [ ] setAuthMode creates a yesno Question with RequiresText on both options
- [ ] handleAuthorizeKey delegates to QuestionPrompt.HandleKey
- [ ] Answer maps to AuthResult: yes=approved, no=rejected, text=instructions
- [ ] PreviewDiff field added to QuestionPrompt for diff display
- [ ] Old authorization.go UI file is removed

**Verify:**

```bash
cd /home/goglue/src/squid-os && go build ./...
```

---

## 5. System Prompt

- **Pattern:** Instruction Injection

**Objective:** Update the system prompt so the LLM knows about the ask tool and understands when and how to use it.

**Success Criteria:** System prompt includes ask tool usage guidelines: when to collect info from user, how to structure questions, what types to use for what scenarios.

```mermaid
sys-prompt.go -> DefaultAssistantPrompt() includes ask tool section -> injected into every API request
```

### 5.1. Add ask tool documentation to system prompt

**Type:** feature

**What:** Add ask tool section to internal/config/sys-prompt.go explaining the 4 question types, when to use the tool (interviews, confirmations, structured data collection), and how to structure the questions array.

**Why:** The LLM needs to know about the ask tool so it can use it proactively when it needs to collect structured information from the user rather than just asking open-ended questions in chat.

**Files:**

- ~ internal/config/sys-prompt.go

**Snippet:**

```
## Ask Tool
- Use the "ask" tool when you need to collect structured information from the user.
- Ideal for: interviews, multi-step questionnaires, confirmations with options, preference gathering.
- The tool accepts an array of questions and returns an array of answers.

### Question Types
- **yesno**: Yes/No binary choice. Use for confirmations and boolean questions.
- **radio**: Single choice from a list of options. Use when the user must pick exactly one.
- **checkbox**: Multiple choices from a list. Use when the user can select multiple options.
- **text**: Free-form text input. Use for open-ended questions.

### Structure
Each question has: id, type, label, options (for radio/checkbox), textPrompt (optional), required (optional).
Each option can have: value, label, requiresText, textPrompt.

### Example
{
  "questions": [
    {"id": "q1", "type": "radio", "label": "What is your role?",
     "options": [{"value": "dev", "label": "Developer"}, {"value": "designer", "label": "Designer"}]},
    {"id": "q2", "type": "text", "label": "Tell me about your experience", "required": true}
  ]
}

The result is an array of answers with value, text, and values (for checkbox) fields.
```

**Acceptance Criteria:**

- [ ] System prompt includes ask tool section with all 4 question types
- [ ] Examples show proper JSON structure for questions
- [ ] Describes when to use the tool vs regular chat
- [ ] Mentions option-level requiresText and textPrompt

**Verify:**

```bash
cd /home/goglue/src/squid-os && go build ./...
```
