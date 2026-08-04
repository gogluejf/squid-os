# Questionnaire Component & Auth Mode Refactor

## Core Problem

Users need a reusable question-input component for the AI to collect information interactively. The existing auth mode pattern (yes/no + instructions) should be extracted into a generic questionnaire component that supports multiple prompt modes. This avoids duplicating the interaction pattern and enables interviews, structured data collection, and UX-friendly user input throughout the application.

## Goal

A reusable Questionnaire component supporting yes/no, radio, checkbox, and free-text prompt modes. The auth mode is refactored to use this component. The tool accepts an array of questions and returns structured answers. Consistent keyboard navigation (Tab/Shift+Tab/Enter) across all modes.

---

## 1. Questionnaire Domain Model

- **Pattern:** Value Object / Enum

**Objective:** Define the domain types for question types, question structure, and answer structure that the tool and UI will share.

**Success Criteria:** QuestionType enum, Question struct, Answer struct compile and are importable by both the tool handler and the UI component.

```mermaid
QuestionType(yesno|radio|checkbox|text) → Question(id, type, prompt, options, required) → Answer(id, value, instructions) → QuestionnaireResult(answers array)
```

### 1.1. Add QuestionType enum

**What:** Add QuestionType enum with YesNo, Radio, Checkbox, Text constants in internal/domain/questionnaire.go.

**Why:** Single source of truth for supported prompt modes, used by both tool schema validation and UI rendering.

**Files:**

- + internal/domain/questionnaire.go

**Snippet:**

```
package domain

type QuestionType string

const (
	QuestionYesNo    QuestionType = "yesno"
	QuestionRadio    QuestionType = "radio"
	QuestionCheckbox QuestionType = "checkbox"
	QuestionText     QuestionType = "text"
)

func (qt QuestionType) Valid() bool {
	switch qt {
	case QuestionYesNo, QuestionRadio, QuestionCheckbox, QuestionText:
		return true
	}
	return false
}

```

**Acceptance Criteria:**

- [ ] Enum compiles, Valid() returns true for known types and false for unknown

**Verify:**

```bash
cd /home/goglue/src/squid-os && go build ./...
```

### 1.2. Add Question struct

**What:** Add Question struct with ID, Type, Prompt, Options, Required fields in internal/domain/questionnaire.go.

**Why:** Represents a single question sent by the AI tool, carrying all metadata needed for rendering and validation.

**Files:**

- ~ internal/domain/questionnaire.go

**Snippet:**

```
type Question struct {
	ID       string              // unique question identifier
	Type     QuestionType      // yesno, radio, checkbox, text
	Prompt   string          // the question text shown to user
	Options  []string       // available choices (for radio/checkbox)
	Required bool          // whether answer is mandatory
}

func (q Question) HasOptions() bool {
	return q.Type == QuestionRadio || q.Type == QuestionCheckbox
}

func (q Question) SupportsInstructions() bool {
	// All question types support an optional instructions/notes field
	return true
}

```

**Acceptance Criteria:**

- [ ] Struct compiles, HasOptions returns true for radio/checkbox, SupportsInstructions always true

**Verify:**

```bash
cd /home/goglue/src/squid-os && go build ./...
```

### 1.3. Add Answer struct

**What:** Add Answer struct with ID, Value (interface{}), Instructions fields in internal/domain/questionnaire.go.

**Why:** Carries the user's response back to the AI. Value is interface{} to handle string (yesno/radio/text) or []string (checkbox).

**Files:**

- ~ internal/domain/questionnaire.go

**Snippet:**

```
type Answer struct {
	ID           string                  // matches Question.ID
	Value        interface{}          // string or []string
	Instructions string        // optional extra context from user
}

type QuestionnaireResult struct {
	Answers []Answer 
}

```

**Acceptance Criteria:**

- [ ] Struct compiles, Answer.Value is interface{} to support multiple types

**Verify:**

```bash
cd /home/goglue/src/squid-os && go build ./...
```

---

## 2. Questionnaire UI Component

- **Pattern:** Composite Component / State Machine

**Objective:** Build the core QuestionnairePrompt UI component that renders questions sequentially, handles keyboard navigation between answer selection and text input, and collects structured answers.

**Success Criteria:** QuestionnairePrompt renders questions one at a time, supports all four prompt modes with keyboard navigation (Tab/Shift+Tab/Enter), and produces a QuestionnaireResult on completion.

```mermaid
QuestionnairePrompt → iterates Questions → renders current Question → sub-component per type (YesNoPrompt / RadioPrompt / CheckboxPrompt / TextPrompt) → Tab switches to InstructionsInput → Shift+Tab returns to selector → Enter advances to next or submits
```

### 2.1. Add QuestionnairePrompt struct

**What:** Create QuestionnairePrompt struct in internal/ui/questionnaire.go with Questions, currentIndex, answers slice, textMode flag, and instructionBuffer.

**Why:** Manages state across a multi-question flow: tracks current question, accumulated answers, and whether the user is in answer-selection or instruction-input mode.

**Files:**

- + internal/ui/questionnaire.go

**Snippet:**

```
package ui

import (
	"squid-os/internal/domain"
)

type QuestionnairePrompt struct {
	Questions        []domain.Question
	CurrentIndex     int
	Answers          []domain.Answer
	TextMode         bool       // true = user typing instructions
	InstructionBuf   string     // instruction text for current question
	Width            int
	// Per-question answer state
	currentSelection int        // index for radio/checkbox, 0/1 for yesno
	currentText      string     // text input for text questions
}

func NewQuestionnairePrompt(questions []domain.Question, width int) *QuestionnairePrompt {
	return &QuestionnairePrompt{
		Questions:    questions,
		Width:        width,
		Answers:      make([]domain.Answer, 0, len(questions)),
	}
}

func (p *QuestionnairePrompt) Current() domain.Question {
	if p.CurrentIndex < len(p.Questions) {
		return p.Questions[p.CurrentIndex]
	}
	return domain.Question{}
}

func (p *QuestionnairePrompt) IsComplete() bool {
	return p.CurrentIndex >= len(p.Questions)
}

func (p *QuestionnairePrompt) Result() domain.QuestionnaireResult {
	return domain.QuestionnaireResult{Answers: p.Answers}
}

```

**Acceptance Criteria:**

- [ ] Struct compiles, NewQuestionnairePrompt initializes correctly, IsComplete returns true when all questions answered

**Verify:**

```bash
cd /home/goglue/src/squid-os && go build ./...
```

### 2.2. Implement QuestionnairePrompt Render

**What:** Add Render() method to QuestionnairePrompt that delegates to per-type renderers and shows progress indicator.

**Why:** Displays the current question with appropriate input UI, progress bar, and navigation hints.

**Files:**

- ~ internal/ui/questionnaire.go

**Snippet:**

```
func (p *QuestionnairePrompt) Render() string {
	if p.IsComplete() {
		return "Questionnaire complete."
	}
	q := p.Current()
	
	// Progress line: "Question 2/5"
	progress := fmt.Sprintf("[%d/%d]", p.CurrentIndex+1, len(p.Questions))
	
	// Prompt text
	promptLine := q.Prompt
	
	var answerBlock string
	if p.TextMode {
		answerBlock = p.renderInstructionInput()
	} else {
		switch q.Type {
		case domain.QuestionYesNo:
			answerBlock = p.renderYesNo(q)
		case domain.QuestionRadio:
			answerBlock = p.renderRadio(q)
		case domain.QuestionCheckbox:
			answerBlock = p.renderCheckbox(q)
		case domain.QuestionText:
			answerBlock = p.renderTextInput(q)
		}
	}
	
	hint := p.navigationHint()
	return style.StatusLineStyle.Width(p.Width).Render(
		fmt.Sprintf("  %s  %s
  %s  %s", progress, promptLine, answerBlock, hint))
}

func (p *QuestionnairePrompt) navigationHint() string {
	if p.TextMode {
		return "Enter to submit · Shift+Tab to return"
	}
	return "←→ select · Tab for instructions · Enter to confirm"
}

```

**Acceptance Criteria:**

- [ ] Render outputs progress, prompt, answer block, and navigation hint. Delegates correctly per question type.

**Verify:**

```bash
cd /home/goglue/src/squid-os && go build ./...
```

### 2.3. Implement YesNo renderer

**What:** Add renderYesNo method that shows [Y]es / [N]o with selection highlight on current choice.

**Why:** YesNo is the simplest mode and the basis for auth mode reuse. Selected option gets highlight style.

**Files:**

- ~ internal/ui/questionnaire.go

**Snippet:**

```
func (p *QuestionnairePrompt) renderYesNo(q domain.Question) string {
	var yes, no string
	if p.currentSelection == 0 {
		yes = style.SelectionStyle.Render("Yes")
		no = style.UnselectedStyle.Render("No")
	} else {
		yes = style.UnselectedStyle.Render("Yes")
		no = style.SelectionStyle.Render("No")
	}
	return fmt.Sprintf("  %s / %s", yes, no)
}

```

**Acceptance Criteria:**

- [ ] Yes/No render with highlight on selected option

**Verify:**

```bash
cd /home/goglue/src/squid-os && go build ./...
```

### 2.4. Implement Radio renderer

**What:** Add renderRadio method that lists options with index numbers, highlights selected.

**Why:** Single-select from a list of options. Each option gets a number prefix for keyboard navigation.

**Files:**

- ~ internal/ui/questionnaire.go

**Snippet:**

```
func (p *QuestionnairePrompt) renderRadio(q domain.Question) string {
	parts := make([]string, len(q.Options))
	for i, opt := range q.Options {
		if i == p.currentSelection {
			parts[i] = style.SelectionStyle.Render(fmt.Sprintf("[%d] %s", i+1, opt))
		} else {
			parts[i] = style.UnselectedStyle.Render(fmt.Sprintf("[%d] %s", i+1, opt))
		}
	}
	return "  " + strings.Join(parts, "  ")
}

```

**Acceptance Criteria:**

- [ ] Options render with numbered prefixes, selected option highlighted

**Verify:**

```bash
cd /home/goglue/src/squid-os && go build ./...
```

### 2.5. Implement Checkbox renderer

**What:** Add renderCheckbox method with toggle selection (space to toggle each option), shows checked/unchecked state.

**Why:** Multi-select from a list. Each option can be independently toggled. Selection tracked as a set of indices.

**Files:**

- ~ internal/ui/questionnaire.go

**Snippet:**

```
// Add to QuestionnairePrompt struct:
	checkedItems map[int]bool // tracks checked options for checkbox mode

func (p *QuestionnairePrompt) renderCheckbox(q domain.Question) string {
	if p.checkedItems == nil {
		p.checkedItems = make(map[int]bool)
	}
	parts := make([]string, len(q.Options))
	for i, opt := range q.Options {
		symbol := "☐"
		if p.checkedItems[i] {
			symbol = "☑"
		}
		parts[i] = style.UnselectedStyle.Render(fmt.Sprintf("%s [%d] %s", symbol, i+1, opt))
	}
	return "  " + strings.Join(parts, "  ") + "  (Space to toggle, ←→ to navigate)"
}

```

**Acceptance Criteria:**

- [ ] Options show ☐/☑ state, selection tracked in checkedItems map

**Verify:**

```bash
cd /home/goglue/src/squid-os && go build ./...
```

### 2.6. Implement Text input renderer

**What:** Add renderTextInput that shows a dedicated inline text field for free-text answers, distinct from the shared textarea.

**Why:** Free-text questions need their own input area scoped to this single question. Not the global textarea — a focused inline input with cursor.

**Files:**

- ~ internal/ui/questionnaire.go

**Snippet:**

```
func (p *QuestionnairePrompt) renderTextInput(q domain.Question) string {
	placeholder := q.Prompt
	if placeholder == "" {
		placeholder = "Type your answer..."
	}
	input := p.currentText
	if input == "" {
		input = style.PlaceholderStyle.Render(placeholder)
	}
	return "  > " + input + "█"
}

func (p *QuestionnairePrompt) renderInstructionInput() string {
	input := p.InstructionBuf
	if input == "" {
		input = style.PlaceholderStyle.Render("Optional instructions...")
	}
	return "  Instructions: " + input + "█"
}

```

**Acceptance Criteria:**

- [ ] Text input renders inline with cursor indicator, placeholder when empty. Instruction input separate from question text.

**Verify:**

```bash
cd /home/goglue/src/squid-os && go build ./...
```

### 2.7. Implement QuestionnairePrompt key handler

**What:** Add HandleKey(msg tea.KeyMsg) method that processes navigation (←→, Tab, Shift+Tab, Enter, Space, Backspace, Runes) per question type.

**Why:** Routes keyboard input to the correct action based on current question type and text mode. Core interaction logic for the component.

**Files:**

- ~ internal/ui/questionnaire.go

**Snippet:**

```
func (p *QuestionnairePrompt) HandleKey(msg tea.KeyMsg) bool {
	q := p.Current()
	
	// Text mode — typing instructions
	if p.TextMode {
		switch {
		case key.Matches(msg, keys.Send):
			p.submitCurrent()
			return true
		case msg.Type == tea.KeyTab && msg.Modifiers.Contains(tea.ModShift):
			p.TextMode = false
			return true
		case msg.Type == tea.KeyRunes:
			p.InstructionBuf += string(msg.Runes)
			return true
		case msg.Type == tea.KeyBackspace:
			p.backspace(&p.InstructionBuf)
			return true
		}
		return false
	}
	
	// Answer selection mode
	switch q.Type {
	case domain.QuestionYesNo:
		return p.handleYesNoKey(msg)
	case domain.QuestionRadio:
		return p.handleRadioKey(msg)
	case domain.QuestionCheckbox:
		return p.handleCheckboxKey(msg)
	case domain.QuestionText:
		return p.handleTextKey(msg)
	}
	return false
}

func (p *QuestionnairePrompt) handleYesNoKey(msg tea.KeyMsg) bool {
	switch {
	case key.Matches(msg, keys.Left):
		p.currentSelection = 0
		return true
	case key.Matches(msg, keys.Right):
		p.currentSelection = 1
		return true
	case msg.Type == tea.KeyTab:
		p.TextMode = true
		return true
	case key.Matches(msg, keys.Send):
		p.submitCurrent()
		return true
	}
	return false
}

func (p *QuestionnairePrompt) submitCurrent() {
	q := p.Current()
	var val interface{}
	switch q.Type {
	case domain.QuestionYesNo:
		val = p.currentSelection == 0
	case domain.QuestionRadio:
		val = q.Options[p.currentSelection]
	case domain.QuestionCheckbox:
		val = p.checkedIndices()
	case domain.QuestionText:
		val = p.currentText
	}
	p.Answers = append(p.Answers, domain.Answer{
		ID:           q.ID,
		Value:        val,
		Instructions: p.InstructionBuf,
	})
	p.CurrentIndex++
	p.TextMode = false
	p.InstructionBuf = ""
	p.currentSelection = 0
	p.currentText = ""
	p.checkedItems = nil
}

```

**Acceptance Criteria:**

- [ ] Key handler processes all modes correctly. submitCurrent builds the right Answer type per question. Advances index on submit.

**Verify:**

```bash
cd /home/goglue/src/squid-os && go build ./...
```

### 2.8. Implement Radio and Checkbox key handlers

**What:** Add handleRadioKey (←→ navigate, Enter submit, Tab instructions) and handleCheckboxKey (←→ navigate, Space toggle, Enter submit, Tab instructions).

**Why:** Radio navigates with arrows and submits single selection. Checkbox navigates with arrows, toggles with space, and submits all checked items.

**Files:**

- ~ internal/ui/questionnaire.go

**Snippet:**

```
func (p *QuestionnairePrompt) handleRadioKey(msg tea.KeyMsg) bool {
	q := p.Current()
	switch {
	case key.Matches(msg, keys.Left):
		if p.currentSelection > 0 {
			p.currentSelection--
		}
		return true
	case key.Matches(msg, keys.Right):
		if p.currentSelection < len(q.Options)-1 {
			p.currentSelection++
		}
		return true
	case msg.Type == tea.KeyTab:
		p.TextMode = true
		return true
	case key.Matches(msg, keys.Send):
		p.submitCurrent()
		return true
	}
	return false
}

func (p *QuestionnairePrompt) handleCheckboxKey(msg tea.KeyMsg) bool {
	q := p.Current()
	if p.checkedItems == nil {
		p.checkedItems = make(map[int]bool)
	}
	switch {
	case key.Matches(msg, keys.Left):
		if p.currentSelection > 0 {
			p.currentSelection--
		}
		return true
	case key.Matches(msg, keys.Right):
		if p.currentSelection < len(q.Options)-1 {
			p.currentSelection++
		}
		return true
	case msg.Type == tea.KeySpace:
		p.checkedItems[p.currentSelection] = !p.checkedItems[p.currentSelection]
		return true
	case msg.Type == tea.KeyTab:
		p.TextMode = true
		return true
	case key.Matches(msg, keys.Send):
		p.submitCurrent()
		return true
	}
	return false
}

func (p *QuestionnairePrompt) checkedIndices() []string {
	var indices []string
	for i, checked := range p.checkedItems {
		if checked {
			indices = append(indices, p.Questions[p.CurrentIndex].Options[i])
		}
	}
	return indices
}

```

**Acceptance Criteria:**

- [ ] Radio navigates with arrows, checkbox toggles with space and navigates with arrows, both support Tab for instructions and Enter to submit

**Verify:**

```bash
cd /home/goglue/src/squid-os && go build ./...
```

---

## 3. Questionnaire Tool

- **Pattern:** Tool Definition / Schema

**Objective:** Register the questionnaire tool with the tool registry, defining its JSON schema (array of questions in, array of answers out) and execution handler that pauses for user input.

**Success Criteria:** Tool is registered, accepts a batch of questions, pauses the stream in ModeQuestionnaire, and returns structured answers to the AI.

```mermaid
LLM calls questionnaire({questions: [...]}) → schema validates → execute pauses stream → sets ModeQuestionnaire → QuestionnairePrompt renders → user answers → tool returns {answers: [...]} → stream resumes
```

### 3.1. Register questionnaire tool with schema

**What:** Create internal/tools/questionnaire.go with JSON schema defining the questions array parameter and register it in the tool registry.

**Why:** The AI needs a tool definition to call. Schema enforces question structure (id, type, prompt, optional options).

**Files:**

- + internal/tools/questionnaire.go

**Snippet:**

```
package tools

import "squid-os/internal/domain"

var Questionnaire = Tool{
	Name:        "questionnaire",
	Description: "Ask the user a series of questions and collect structured answers. Supports yesno, radio, checkbox, and text question types.",
	Schema: ,
	IsDestructive: func(args map[string]interface{}) bool { return false },
	Execute:       executeQuestionnaire,
}

```

**Acceptance Criteria:**

- [ ] Schema is valid JSON, tool is non-destructive, registered in tool registry

**Verify:**

```bash
cd /home/goglue/src/squid-os && go build ./...
```

### 3.2. Implement questionnaire Execute handler

**What:** Add executeQuestionnaire that parses questions from args, stores them on the stream for the UI to pick up, and returns ToolResult with status indicating user interaction needed.

**Why:** Unlike normal tools, questionnaire doesn't produce an immediate result — it pauses the stream and defers to the UI component. The execute handler sets up the questionnaire context and signals the stream to pause.

**Files:**

- ~ internal/tools/questionnaire.go

**Snippet:**

```
func executeQuestionnaire(args map[string]interface{}) ToolResult {
	questionsJSON, ok := args["questions"].([]interface{})
	if !ok || len(questionsJSON) == 0 {
		return ToolResult{Status: ResultStatusError, Error: "questions is required and must be a non-empty array"}
	}
	// Parse questions into domain type
	var questions []domain.Question
	// ... unmarshal questionsJSON into questions ...
	// Validation
	for _, q := range questions {
		if !q.Type.Valid() {
			return ToolResult{Status: ResultStatusError, Error: fmt.Sprintf("invalid question type: %s", q.Type)}
		}
		if q.HasOptions() && len(q.Options) == 0 {
			return ToolResult{Status: ResultStatusError, Error: fmt.Sprintf("question %s requires options", q.ID)}
		}
	}
	// Store for the stream layer to pick up
	// This is set on a shared context that the stream layer reads
	// Return a special status indicating UI interaction needed
	return ToolResult{
		Status:      ResultStatusPending,
		Result:      "questionnaire_paused",
		Questionnaire: questions, // custom field on ToolResult
	}
}

// Add to ToolResult struct:
type ToolResult struct {
	// ... existing fields ...
	Questionnaire []domain.Question // set when questionnaire tool needs UI interaction
}

```

**Acceptance Criteria:**

- [ ] Execute validates questions, returns ResultStatusPending with Questionnaire populated

**Verify:**

```bash
cd /home/goglue/src/squid-os && go build ./...
```

---

## 4. Stream Integration

- **Pattern:** Mode Interrupt / State Machine

**Objective:** Integrate the questionnaire into the stream loop as a new mode (ModeQuestionnaire), handling the pause/resume cycle and wiring it into the key event dispatch.

**Success Criteria:** When the questionnaire tool is called, the stream pauses in ModeQuestionnaire, the QuestionnairePrompt renders, user answers all questions, and the stream resumes with the structured result injected as a tool response.

```mermaid
stream → tool_calls → questionnaire returns Pending → set ModeQuestionnaire → init QuestionnairePrompt → render loop → user keys → HandleKey → submitCurrent per question → all complete → build ToolResult with answers JSON → resume stream
```

### 4.1. Add ModeQuestionnaire to modes

**What:** Add ModeQuestionnaire to the Mode enum in internal/app/modes.go and its String() method.

**Why:** New mode for the questionnaire interaction flow, distinct from ModeAuthorize.

**Files:**

- ~ internal/app/modes.go

**Snippet:**

```
const (
	// ... existing modes ...
	ModeAuthorize
	ModeQuestionnaire  // awaiting user questionnaire responses
)

func (m Mode) String() string {
	// ...
	case ModeQuestionnaire:
		return "questionnaire"
	// ...
}

```

**Acceptance Criteria:**

- [ ] ModeQuestionnaire compiles, String() returns "questionnaire"

**Verify:**

```bash
cd /home/goglue/src/squid-os && go build ./...
```

### 4.2. Add questionnaire context to streamState

**What:** Add questionnairePrompt *ui.QuestionnairePrompt and questionnaireToolCall partialTool to streamState in stream.go.

**Why:** The stream needs to hold the questionnaire UI state and know which tool call to resolve upon completion.

**Files:**

- ~ internal/app/stream.go

**Snippet:**

```
// In streamState struct:
	questionnairePrompt  *ui.QuestionnairePrompt  // non-nil when in ModeQuestionnaire
	questionnaireToolIdx int                      // index into partialTools for the questionnaire call

func (m *Model) setQuestionnaireMode(questions []domain.Question, toolIdx int) {
	m.mode = ModeQuestionnaire
	m.stream.active = false
	m.stream.questionnairePrompt = ui.NewQuestionnairePrompt(questions, m.width)
	m.stream.questionnaireToolIdx = toolIdx
	m.updateViewportContent()
}

```

**Acceptance Criteria:**

- [ ] streamState holds questionnaire context, setQuestionnaireMode initializes it from questions

**Verify:**

```bash
cd /home/goglue/src/squid-os && go build ./...
```

### 4.3. Detect questionnaire tool in executeTools

**What:** In executeTools, check if the current partial is the questionnaire tool returning Pending. If so, call setQuestionnaireMode and return nil to pause the stream.

**Why:** The stream needs to intercept the questionnaire's pending result and switch to the questionnaire mode instead of treating it as a normal tool result.

**Files:**

- ~ internal/app/stream.go

**Snippet:**

```
// In executeTools, after tool.Execute(args):
	if result.Status == tools.ResultStatusPending && result.Questionnaire != nil {
		m.setQuestionnaireMode(result.Questionnaire, i)
		return nil // pause stream
	}

```

**Acceptance Criteria:**

- [ ] When questionnaire tool returns Pending, stream enters ModeQuestionnaire instead of continuing

**Verify:**

```bash
cd /home/goglue/src/squid-os && go build ./...
```

### 4.4. Render questionnaire in View and wire key dispatch

**What:** In render.go View(), add case for ModeQuestionnaire that renders questionnairePrompt. In update.go handleKey, add case for ModeQuestionnaire that routes to questionnairePrompt.HandleKey and handles completion.

**Why:** Wires the questionnaire into the main View/Update cycle. On completion (IsComplete), resolves the tool call with the answers JSON and resumes the stream.

**Files:**

- ~ internal/app/render.go
- ~ internal/app/update.go

**Snippet:**

```
// render.go - in View():
case ModeQuestionnaire:
	if m.stream.questionnairePrompt != nil {
		sections = append(sections, m.stream.questionnairePrompt.Render())
	}

// update.go - in handleKey():
case ModeQuestionnaire:
	return m.handleQuestionnaireKey(msg)

// handleQuestionnaireKey:
func (m Model) handleQuestionnaireKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
 qp := m.stream.questionnairePrompt
 if qp == nil {
  return m, nil
 }
 if qp.HandleKey(msg) {
  m.updateViewportContent()
  if qp.IsComplete() {
   return m.resolveQuestionnaire()
  }
  return m, nil
 }
 return m, nil
}

func (m *Model) resolveQuestionnaire() (tea.Model, tea.Cmd) {
 qp := m.stream.questionnairePrompt
 result := qp.Result()
 
 // Build the tool result with answers JSON
 answersJSON, _ := json.Marshal(result)
 
 // Mark the questionnaire tool call as completed with the answers
 idx := m.stream.questionnaireToolIdx
 // Update the partial tool's result
 m.stream.partialTools[idx].result = ToolResult{
  Status: ResultStatusSuccess,
  Result: string(answersJSON),
 }
 
 // Clear questionnaire state
 m.stream.questionnairePrompt = nil
 m.stream.questionnaireToolIdx = -1
 
 // Resume stream with the questionnaire answer
 return m.resumeStreamAfterTool(idx)
}

```

**Acceptance Criteria:**

- [ ] Questionnaire renders in View, key events route through HandleKey, on completion resolves with answers JSON and resumes stream

**Verify:**

```bash
cd /home/goglue/src/squid-os && go build ./...
```

---

## 5. Auth Mode Refactor

- **Pattern:** Extract Component / Reuse

**Objective:** Refactor the authorization mode (from tool-authorization plan) to use the questionnaire component instead of its own yes/no + instructions UI. The auth mode becomes a thin wrapper that builds a single-question questionnaire and interprets the result.

**Success Criteria:** ModeAuthorize uses the questionnaire component with a single yesno question. The existing AuthorizationContext maps to a Question/Answer pair. No duplicated UI code.

```mermaid
ModeAuthorize → builds QuestionnairePrompt with single yesno question("Proceed with <tool>?") → user answers yes/no + optional instructions → resolveAuthorization maps Answer.Value (bool) + Answer.Instructions to AuthResult
```

### 5.1. Replace AuthorizationPrompt with QuestionnairePrompt for auth mode

**What:** In setAuthMode(), replace the old ui.AuthorizationPrompt initialization with a QuestionnairePrompt containing a single yesno question. Update ModeAuthorize rendering to use the questionnaire prompt.

**Why:** Eliminates code duplication — auth mode reuses the same questionnaire component instead of maintaining its own yes/no + instructions UI.

**Files:**

- ~ internal/app/stream.go
- ~ internal/app/render.go

**Snippet:**

```
// setAuthMode now uses questionnaire:
func (m *Model) setAuthMode() {
	ctx := m.stream.authorizationCtx
	questions := []domain.Question{
		{
			ID:     "auth_" + ctx.ToolName,
			Type:   domain.QuestionYesNo,
			Prompt: fmt.Sprintf("Proceed with %s?", ctx.ToolName),
			Required: true,
		},
	}
	m.mode = ModeAuthorize
	m.stream.active = false
	m.stream.questionnairePrompt = ui.NewQuestionnairePrompt(questions, m.width)
	m.stream.questionnaireToolIdx = m.stream.pendingToolIndex
	m.updateViewportContent()
}

// render.go — ModeAuthorize now uses the same questionnaire rendering:
case ModeAuthorize:
	if m.stream.questionnairePrompt != nil {
		sections = append(sections, m.stream.questionnairePrompt.Render())
	}

```

**Acceptance Criteria:**

- [ ] setAuthMode initializes a questionnaire with a single yesno question. ModeAuthorize renders via questionnairePrompt

**Verify:**

```bash
cd /home/goglue/src/squid-os && go build ./...
```

### 5.2. Wire ModeAuthorize key handling through questionnaire

**What:** In handleKey, update ModeAuthorize case to use questionnairePrompt.HandleKey. On completion, map the questionnaire answer to AuthResult and call the existing resolveAuthorization.

**Why:** Auth mode key handling now goes through the questionnaire component. On completion, the boolean answer maps to Accepted/Rejected and instructions map to WithInstructions variants.

**Files:**

- ~ internal/app/update.go

**Snippet:**

```
// In handleKey():
case ModeAuthorize:
	return m.handleAuthQuestionnaireKey(msg)

func (m Model) handleAuthQuestionnaireKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	qp := m.stream.questionnairePrompt
	if qp == nil {
		return m, nil
	}
	if qp.HandleKey(msg) {
		m.updateViewportContent()
		if qp.IsComplete() {
			return m.resolveAuthFromQuestionnaire()
		}
		return m, nil
	}
	return m, nil
}

func (m *Model) resolveAuthFromQuestionnaire() (tea.Model, tea.Cmd) {
	qp := m.stream.questionnairePrompt
	result := qp.Result()
	if len(result.Answers) == 0 {
		return m, nil
	}
	answer := result.Answers[0]
	
	// Map bool answer to AuthResult
	accepted := false
	if boolVal, ok := answer.Value.(bool); ok {
		accepted = boolVal
	}
	
	var authResult AuthResult
	if accepted {
		if answer.Instructions != "" {
			authResult = AuthAcceptedWithInstructions
		} else {
			authResult = AuthAccepted
		}
	} else {
		if answer.Instructions != "" {
			authResult = AuthRejectedWithInstructions
		} else {
			authResult = AuthRejected
		}
	}
	
	// Clear questionnaire state
	m.stream.questionnairePrompt = nil
	return m.resolveAuthorization(authResult, answer.Instructions)
}

```

**Acceptance Criteria:**

- [ ] ModeAuthorize key handling goes through questionnaire. On completion, answer maps correctly to the 4 AuthResult variants. resolveAuthorization receives the mapped result.

**Verify:**

```bash
cd /home/goglue/src/squid-os && go build ./...
```

### 5.3. Remove old AuthorizationPrompt struct

**What:** Delete internal/ui/authorization.go (or deprecate the AuthorizationPrompt struct) since its functionality is now subsumed by the questionnaire component.

**Why:** Eliminates the duplicated yes/no + instructions UI. Auth mode now purely uses the questionnaire component.

**Files:**

- - internal/ui/authorization.go

**Snippet:**

```
// Remove or comment out the entire AuthorizationPrompt struct and its methods.
// The auth mode now uses QuestionnairePrompt with a single yesno question.

```

**Acceptance Criteria:**

- [ ] File removed, no references to AuthorizationPrompt remain in the codebase

**Verify:**

```bash
cd /home/goglue/src/squid-os && go build ./...
```

---

## 6. System Prompt Update

- **Pattern:** Instruction Injection

**Objective:** Add instructions to the system prompt explaining the questionnaire tool — when to use it, how to structure questions, and how to interpret answers.

**Success Criteria:** System prompt includes a questionnaire tool section explaining all four question types with examples.

```mermaid
sys-prompt.go → DefaultAssistantPrompt() adds questionnaire section → explains types (yesno, radio, checkbox, text) → explains answer format
```

### 6.1. Add questionnaire tool instructions to system prompt

**What:** Add a questionnaire tool section to the default system prompt in internal/config/sys-prompt.go explaining when and how to use it.

**Why:** The LLM needs to know when to use the questionnaire tool (interviews, structured data collection, confirmation) and how to format questions properly.

**Files:**

- ~ internal/config/sys-prompt.go

**Snippet:**

```
## Questionnaire Tool
- Use the questionnaire tool when you need to collect structured information from the user through an interactive interview.
- Question types:
  - yesno: Simple yes/no question. Answer returns a boolean.
  - radio: Single selection from a list. Provide options array. Answer returns the selected option string.
  - checkbox: Multiple selection from a list. Provide options array. Answer returns an array of selected option strings.
  - text: Free-text answer. Answer returns a string.
- Each question must have a unique id, type, and prompt.
- For radio/checkbox, provide an options array with the available choices.
- The user can add optional instructions/notes to any answer.
- Answers are returned as {"answers": [{"id": "...", "value": ..., "instructions": "..."}]}
- Use this for interviews, onboarding, configuration, or any multi-step information gathering.

```

**Acceptance Criteria:**

- [ ] System prompt contains questionnaire instructions with all four types and usage guidelines

**Verify:**

```bash
cd /home/goglue/src/squid-os && go build ./...
```

---

## 7. ToolResult Extension

- **Pattern:** Schema Extension

**Objective:** Extend the ToolResult struct to support the Pending status and the Questionnaire field, enabling tools to signal that they need UI interaction before completing.

**Success Criteria:** ToolResult has ResultStatusPending constant and Questionnaire field. Stream layer checks for this status to decide whether to enter questionnaire mode.

```mermaid
ToolResult → adds ResultStatusPending, Questionnaire []domain.Question → executeTools checks status → if Pending with Questionnaire set, switches mode
```

### 7.1. Add ResultStatusPending and Questionnaire to ToolResult

**What:** Add ResultStatusPending constant and Questionnaire []domain.Question field to ToolResult struct in internal/tools/tools.go.

**Why:** Enables tools to signal that they need UI interaction. The stream layer detects this status and enters the appropriate mode (questionnaire).

**Files:**

- ~ internal/tools/tools.go

**Snippet:**

```
const (
	ResultStatusSuccess   = success
	ResultStatusError     = error
	ResultStatusPending   = pending // tool requires UI interaction
)

type ToolResult struct {
	Status        string
	Result        string
	Error         string
	Destructive   bool
	Files         []config.FileEntry
	Questionnaire []domain.Question // populated when Status == ResultStatusPending
}

```

**Acceptance Criteria:**

- [ ] ResultStatusPending constant added, Questionnaire field on ToolResult, compiles cleanly

**Verify:**

```bash
cd /home/goglue/src/squid-os && go build ./...
```
