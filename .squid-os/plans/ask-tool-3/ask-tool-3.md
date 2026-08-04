# EPIC: Ask Tool — Reusable Question Component
Why: The AI needs a UX-friendly way to collect structured information from users (interviews, confirmations, multi-choice questions). Currently tool authorization has its own prompt pattern. We want a reusable Question component that handles yes/no, radio, checkbox, and free-text inputs — with tab/shift-tab navigation between selection and contextual text input. The authorization mode will be refactored to use this same component, eliminating duplicate prompt logic.
Outcomes: A generic Question component used by both the new ask-tool and the refactored authorization mode. Sequential wizard UI for multi-question interviews. Clean separation: Question component handles input, callers provide questions and receive answers.

## MILESTONE: 1 - Question Domain Model
Pattern: Value Object / Enum
Objective: Define the data structures for questions, answers, and the question types supported by the component.
Success: QuestionType enum, Question struct with type-specific fields, Answer struct with value + text, and validation logic compile cleanly.
Diagram: Question { id, type, label, options[], requiresText(bool per option) } -> Answer { value, text } -> Result = [Answer]

### TASK: 1.1 - Question types, Question struct, and Answer struct
Type: feature
What: Add QuestionType enum (yesno, radio, checkbox, text), Question struct, and Answer struct to internal/tools/ask/ask_types.go.
Why: Core domain types that define what a question looks like and how answers are structured. Every question has an ID, type, label, and type-specific fields. Every answer has a value and optional text.
Files: + internal/tools/ask/ask_types.go
Snippet: package ask\n\nimport "encoding/json"\n\ntype QuestionType string\n\nconst (\n\tQuestionYesNo    QuestionType = "yesno"\n\tQuestionRadio    QuestionType = "radio"\n\tQuestionCheckbox QuestionType = "checkbox"\n\tQuestionText     QuestionType = "text"\n)\n\ntype Question struct {\n\tID          string       \n\tType        QuestionType \n\tLabel       string                 // prompt shown to user\n\tOptions     []Option             // radio/checkbox choices\n\tDefault     interface{}          // default selection (index int or value string)\n\tTextPrompt  string            // label for the text field (per-question fallback)\n\tRequired    bool                // must answer before proceeding\n\tMultiple    bool                // checkbox: allow multiple selections\n}\n\ntype Option struct {\n\tValue       string \n\tLabel       string \n\tTextPrompt  string    // overrides question-level textPrompt when this option is selected\n\tRequiresText bool   // shows text input when this option is selected\n}\n\ntype Answer struct {\n\tValue string         // "yes","no", option value, or free text\n\tText  string           // additional context from text input\n\tValues []string      // checkbox: multiple selected values\n}\n\nfunc (a Answer) IsEmpty() bool {\n\tswitch {\n\tcase a.Value != "":\n\t\treturn false\n\tcase len(a.Values) > 0:\n\t\treturn false\n\tdefault:\n\t\treturn true\n\t}\n}\n\nfunc (q Question) NeedsTextInput() bool {\n\treturn q.Type == QuestionText\n}\n\nfunc (q Question) GetOptionTextPrompt(selectedValue string) string {\n\tfor _, opt := range q.Options {\n\t\tif opt.Value == selectedValue && opt.TextPrompt != "" {\n\t\t\treturn opt.TextPrompt\n\t\t}\n\t}\n\treturn q.TextPrompt\n}\n\nfunc (q Question) GetOptionRequiresText(selectedValue string) bool {\n\tfor _, opt := range q.Options {\n\t\tif opt.Value == selectedValue {\n\t\t\treturn opt.RequiresText\n\t\t}\n\t}\n\treturn false\n}
Acceptance: QuestionType enum has 4 values
Acceptance: Question struct has ID, Type, Label, Options, Default, TextPrompt, Required, Multiple fields
Acceptance: Answer struct has Value, Text, Values fields
Acceptance: IsEmpty returns true only when all fields are empty
Acceptance: GetOptionTextPrompt returns option-specific prompt or falls back to question-level
Acceptance: GetOptionRequiresText returns requiresText for the selected option
Verification: cd /home/goglue/src/squid-os && go build ./...

### TASK: 1.2 - Question validation and serialization helpers
Type: feature
What: Add Validate method on Question and JSON marshal/unmarshal helpers for the ask-tool tool schema in internal/tools/ask/ask_schema.go.
Why: The AI sends questions as JSON args to the tool. We need to validate the structure matches expectations and provide a clean schema definition.
Files: + internal/tools/ask/ask_schema.go
Snippet: package ask\n\n// GetSchema returns the JSON schema for the ask tool arguments.\nfunc GetSchema() string {\n  // Returns schema: { questions: array of Question objects }\n  // questions is required, each must have id, type, label\n  // type determines which additional fields are needed\n}\n\nfunc (q Question) Validate() error {\n  switch q.Type {\n  case QuestionYesNo:\n    // no options needed\n  case QuestionRadio, QuestionCheckbox:\n    if len(q.Options) == 0 {\n      return ErrMissingOptions\n    }\n  case QuestionText:\n    // no options needed\n  default:\n    return ErrUnknownType(q.Type)\n  }\n  return nil\n}\n\nfunc (q Question) HasTextCapability() bool {\n  switch q.Type {\n  case QuestionYesNo, QuestionRadio, QuestionCheckbox:\n    return q.TextPrompt != "" || anyOptionRequiresText(q.Options)\n  case QuestionText:\n    return true\n  default:\n    return false\n  }\n}\n\nfunc anyOptionRequiresText(opts []Option) bool {\n  for _, o := range opts {\n    if o.RequiresText {\n      return true\n    }\n  }\n  return false\n}
Acceptance: Validate returns error for radio/checkbox with no options
Acceptance: Validate returns error for unknown question type
Acceptance: HasTextCapability checks both question-level and option-level text prompts
Acceptance: GetSchema returns valid JSON schema for the ask tool
Verification: cd /home/goglue/src/squid-os && go build ./...

## MILESTONE: 2 - Question UI Component
Pattern: Stateful TUI Component / Wizard Pattern
Objective: Build the reusable QuestionPrompt UI component that renders a single question, handles keyboard input (left/right for selection, tab/shift-tab for text navigation, enter to confirm), and tracks whether the answer is complete.
Success: QuestionPrompt component renders all 4 question types, handles navigation between selection and text input, validates required fields, and exposes IsComplete() and GetAnswer() methods.
Diagram: QuestionPrompt { question, selectionIndex, textMode, textInput, answers[] } <-left/right-> toggle selection <-tab-> text mode <-shift+tab-> selection mode <-enter-> submit answer, advance to next question

### TASK: 2.1 - QuestionPrompt component struct and yes/no rendering
Type: feature
What: Create internal/ui/question_prompt.go with QuestionPrompt struct and Render method handling yes/no question display and selection state.
Why: The core UI component. Starts with yes/no as the simplest case — renders label, two selectable buttons, optional text input. Tracks selection index and text mode state.
Files: + internal/ui/question_prompt.go
Snippet: package ui\n\nimport "github.com/charmbracelet/lipgloss"\n\ntype QuestionPrompt struct {\n  Question       ask.Question\n  Selection      int       // index of selected option (0=yes, 1=no for yesno; option index for radio/checkbox)\n  TextMode       bool      // true = cursor in text input\n  TextInput      string\n  SelectedValues []int     // checkbox: indices of selected options\n  Width          int\n}\n\nfunc NewQuestionPrompt(q ask.Question, width int) *QuestionPrompt {\n  qp := &QuestionPrompt{Question: q, Width: width}\n  if q.Type == ask.QuestionCheckbox {\n    qp.SelectedValues = []int{}\n  }\n  return qp\n}\n\nfunc (qp *QuestionPrompt) Render() string {\n  var sb strings.Builder\n  // 1. Question label\n  sb.WriteString(style.Bold.Render(qp.Question.Label) + "\n\n")\n  \n  switch qp.Question.Type {\n  case ask.QuestionYesNo:\n    sb.WriteString(qp.renderYesNo())\n  case ask.QuestionRadio:\n    sb.WriteString(qp.renderRadio())\n  case ask.QuestionCheckbox:\n    sb.WriteString(qp.renderCheckbox())\n  case ask.QuestionText:\n    sb.WriteString(qp.renderText())\n  }\n  \n  // 2. Text input (if applicable and visible)\n  if qp.shouldShowText() {\n    prompt := qp.Question.GetOptionTextPrompt(qp.getCurrentValue())\n    if prompt == "" {\n      prompt = "Add details..."\n    }\n    sb.WriteString("\n  " + style.Dim.Render(prompt) + "\n")\n    sb.WriteString(qp.renderTextInput())\n  }\n  \n  // 3. Navigation hints\n  sb.WriteString(qp.renderHints())\n  \n  return lipgloss.NewStyle().Width(qp.Width).Render(sb.String())\n}\n\nfunc (qp *QuestionPrompt) shouldShowText() bool {\n  if qp.Question.Type == ask.QuestionText {\n    return false // text IS the question, handled in renderText\n  }\n  if !qp.Question.HasTextCapability() {\n    return false\n  }\n  // For checkbox, show text if any selected option requires it\n  if qp.Question.Type == ask.QuestionCheckbox {\n    for _, idx := range qp.SelectedValues {\n      if idx < len(qp.Question.Options) && qp.Question.Options[idx].RequiresText {\n        return true\n      }\n    }\n    return false\n  }\n  // For yesno/radio, check current selection\n  return qp.Question.GetOptionRequiresText(qp.getCurrentValue())\n}\n\nfunc (qp *QuestionPrompt) getCurrentValue() string {\n  if qp.Question.Type == ask.QuestionYesNo {\n    return map[int]string{0: "yes", 1: "no"}[qp.Selection]\n  }\n  if qp.Selection < len(qp.Question.Options) {\n    return qp.Question.Options[qp.Selection].Value\n  }\n  return ""\n}\n\nfunc (qp *QuestionPrompt) renderYesNo() string {\n  yesStyle, noStyle := style.SelectionStyle, style.Dim\n  if qp.Selection == 0 && !qp.TextMode {\n    yesStyle = style.SelectedStyle\n  } else if qp.Selection == 1 && !qp.TextMode {\n    noStyle = style.SelectedStyle\n  }\n  return "  " + yesStyle.Render("Yes") + "  /  " + noStyle.Render("No")\n}\n\nfunc (qp *QuestionPrompt) renderHints() string {\n  var hints []string\n  switch qp.Question.Type {\n  case ask.QuestionYesNo, ask.QuestionRadio:\n    hints = append(hints, "←/→ select")\n  case ask.QuestionCheckbox:\n    hints = append(hints, "←/→ navigate, Space toggle")\n  case ask.QuestionText:\n    hints = append(hints, "type freely")\n  }\n  if qp.shouldShowText() {\n    if qp.TextMode {\n      hints = append(hints, "Shift+Tab back")\n    } else {\n      hints = append(hints, "Tab details")\n    }\n  }\n  hints = append(hints, "Enter confirm")\n  return "\n  " + style.Dim.Render(strings.Join(hints, "  ·  "))\n}
Acceptance: QuestionPrompt struct has Selection, TextMode, TextInput, SelectedValues, Width fields
Acceptance: NewQuestionPrompt initializes based on question type
Acceptance: Render delegates to type-specific renderers
Acceptance: shouldShowText checks option-level RequiresText and question-level HasTextCapability
Acceptance: getCurrentValue returns correct value for yesno and radio types
Acceptance: Hints adapt based on question type and text mode state
Verification: cd /home/goglue/src/squid-os && go build ./...

### TASK: 2.2 - Radio, checkbox, and text rendering plus keyboard handling
Type: feature
What: Add renderRadio, renderCheckbox, renderText methods and HandleKey for all question types in internal/ui/question_prompt.go.
Why: Complete the UI component with all 4 question type renderers and the keyboard input handler covering left/right selection, space for checkbox toggle, tab/shift-tab navigation, and enter to confirm.
Files: ~ internal/ui/question_prompt.go
Snippet: func (qp *QuestionPrompt) renderRadio() string {\n  var sb strings.Builder\n  for i, opt := range qp.Question.Options {\n    prefix := "  ○ "\n    if i == qp.Selection && !qp.TextMode {\n      prefix = "  ● "\n    }\n    label := opt.Label\n    if opt.Label == "" { label = opt.Value }\n    if i == qp.Selection && !qp.TextMode {\n      sb.WriteString(prefix + style.SelectedStyle.Render(label))\n    } else {\n      sb.WriteString(prefix + style.Dim.Render(label))\n    }\n    if i < len(qp.Question.Options)-1 { sb.WriteString("\n") }\n  }\n  return sb.String()\n}\n\nfunc (qp *QuestionPrompt) renderCheckbox() string {\n  var sb strings.Builder\n  selectedSet := make(map[int]bool)\n  for _, idx := range qp.SelectedValues { selectedSet[idx] = true }\n  for i, opt := range qp.Question.Options {\n    prefix := "  □ "\n    if selectedSet[i] { prefix = "  ☑ " }\n    cursor := "  "\n    if i == qp.Selection && !qp.TextMode { cursor = "→ " }\n    label := opt.Label\n    if opt.Label == "" { label = opt.Value }\n    sb.WriteString(cursor + prefix + style.Dim.Render(label))\n    if i < len(qp.Question.Options)-1 { sb.WriteString("\n") }\n  }\n  return sb.String()\n}\n\nfunc (qp *QuestionPrompt) renderText() string {\n  placeholder := qp.Question.TextPrompt\n  if placeholder == "" { placeholder = "Type your answer..." }\n  content := qp.TextInput\n  if content == "" { content = style.Placeholder.Render(placeholder) }\n  return "  " + style.InputStyle.Render(content)\n}\n\nfunc (qp *QuestionPrompt) HandleKey(msg tea.KeyMsg) (complete bool, answer ask.Answer) {\n  if qp.Question.Type == ask.QuestionText {\n    return qp.handleTextKey(msg)\n  }\n  \n  if qp.TextMode {\n    return qp.handleTextModeKey(msg)\n  }\n  \n  // Selection mode\n  switch {\n  case key.Matches(msg, keys.Left):\n    qp.moveSelection(-1)\n  case key.Matches(msg, keys.Right):\n    qp.moveSelection(1)\n  case msg.Type == tea.KeySpace && qp.Question.Type == ask.QuestionCheckbox:\n    qp.toggleSelection()\n  case msg.Type == tea.KeyTab && !msg.Modifiers.Contains(tea.ModShift):\n    if qp.shouldShowText() { qp.TextMode = true }\n  case msg.Type == tea.KeyTab && msg.Modifiers.Contains(tea.ModShift):\n    qp.TextMode = false\n  case key.Matches(msg, keys.Send):\n    return true, qp.buildAnswer()\n  }\n  return false, ask.Answer{}\n}\n\nfunc (qp *QuestionPrompt) handleTextModeKey(msg tea.KeyMsg) (bool, ask.Answer) {\n  switch {\n  case msg.Type == tea.KeyTab && msg.Modifiers.Contains(tea.ModShift):\n    qp.TextMode = false\n  case key.Matches(msg, keys.Send):\n    return true, qp.buildAnswer()\n  default:\n    // Handle runes, backspace, etc for text input\n    qp.handleTextInput(msg)\n  }\n  return false, ask.Answer{}\n}\n\nfunc (qp *QuestionPrompt) handleTextKey(msg tea.KeyMsg) (bool, ask.Answer) {\n  if key.Matches(msg, keys.Send) {\n    return qp.Question.Required == false || qp.TextInput != "", \n      ask.Answer{Value: qp.TextInput}\n  }\n  qp.handleTextInput(msg)\n  return false, ask.Answer{}\n}\n\nfunc (qp *QuestionPrompt) moveSelection(delta int) {\n  maxIdx := 1 // yesno\n  if qp.Question.Type == ask.QuestionRadio || qp.Question.Type == ask.QuestionCheckbox {\n    maxIdx = len(qp.Question.Options) - 1\n  }\n  qp.Selection = max(0, min(qp.Selection+delta, maxIdx))\n}\n\nfunc (qp *QuestionPrompt) toggleSelection() {\n  idx := qp.Selection\n  exists := false\n  for _, i := range qp.SelectedValues { if i == idx { exists = true; break } }\n  if exists {\n    qp.SelectedValues = removeInt(qp.SelectedValues, idx)\n  } else {\n    qp.SelectedValues = append(qp.SelectedValues, idx)\n  }\n}\n\nfunc (qp *QuestionPrompt) buildAnswer() ask.Answer {\n  switch qp.Question.Type {\n  case ask.QuestionYesNo:\n    return ask.Answer{\n      Value: map[int]string{0: "yes", 1: "no"}[qp.Selection],\n      Text:  qp.TextInput,\n    }\n  case ask.QuestionRadio:\n    val := ""\n    if qp.Selection < len(qp.Question.Options) {\n      val = qp.Question.Options[qp.Selection].Value\n    }\n    return ask.Answer{Value: val, Text: qp.TextInput}\n  case ask.QuestionCheckbox:\n    var values []string\n    for _, idx := range qp.SelectedValues {\n      if idx < len(qp.Question.Options) {\n        values = append(values, qp.Question.Options[idx].Value)\n      }\n    }\n    return ask.Answer{Values: values, Text: qp.TextInput}\n  default:\n    return ask.Answer{}\n  }\n}
Acceptance: renderRadio shows filled circle for selected option
Acceptance: renderCheckbox shows checked box for selected values with cursor navigation
Acceptance: renderText shows placeholder or user input
Acceptance: HandleKey routes to correct handler based on type and text mode
Acceptance: Left/right cycles selection within bounds
Acceptance: Space toggles checkbox selection
Acceptance: Tab enters text mode only if shouldShowText is true
Acceptance: Shift+Tab exits text mode
Acceptance: Enter returns complete=true with the built answer
Acceptance: buildAnswer returns correct structure for all 4 question types
Verification: cd /home/goglue/src/squid-os && go build ./...

## MILESTONE: 3 - Ask Tool
Pattern: Tool Definition / Sequential Wizard
Objective: Create the ask tool that accepts an array of Question objects from the AI, iterates through them sequentially using the QuestionPrompt component, and returns an array of Answers.
Success: The ask tool is registered in the tool registry, accepts a questions array, iterates through each question one at a time, and returns structured answers as JSON.
Diagram: LLM -> ask({questions: [...]}) -> ModeAsk -> iterate questions one by one -> QuestionPrompt for each -> collect Answers[] -> return to AI as tool result

### TASK: 3.1 - Ask tool definition and schema
Type: feature
What: Create internal/tools/ask/ask.go with the Tool definition, GetSchema, DisplayValue, and Execute that parses questions from args and stores them in the ask context.
Why: Registers the ask tool in the tool system. The tool takes a questions array, validates each question, and stores them for sequential rendering.
Files: + internal/tools/ask/ask.go
Snippet: package ask\n\nimport (\n  "encoding/json"\n  "github.com/goglue/squid-os/internal/tools"\n)\n\ntype AskContext struct {\n  Questions []Question\n  Answers   []Answer\n  CurrentQ  int // index of current question being displayed\n}\n\nfunc GetTool(reg *tools.ToolRegistry) {\n  tool := tools.Tool{\n    Name:        "ask",\n    Description: "Present questions to the user and collect structured answers. Supports yes/no, radio, checkbox, and free-text questions.",\n    Schema:      GetSchema(),\n    DisplayValue: func(args string) string {\n      var req struct {\n        Questions []Question \n      }\n      json.Unmarshal([]byte(args), &req)\n      if len(req.Questions) == 1 {\n        return req.Questions[0].Label\n      }\n      return fmt.Sprintf("%d questions", len(req.Questions))\n    },\n    Execute: ExecuteAsk,\n  }\n  reg.Register(tool)\n}\n\nfunc ExecuteAsk(args map[string]interface{}, ctx *tools.ExecutionContext) tools.ToolResult {\n  questionsJSON, ok := args["questions"].(string)\n  if !ok {\n    // Handle array form too\n  }\n  var questions []Question\n  if err := json.Unmarshal([]byte(questionsJSON), &questions); err != nil {\n    // Also try direct array parse\n  }\n  // Validate all questions\n  for _, q := range questions {\n    if err := q.Validate(); err != nil {\n      return tools.ToolResult{\n        Status: tools.ResultStatusError,\n        Error:  fmt.Sprintf("Invalid question %s: %v", q.ID, err),\n      }\n    }\n  }\n  // This returns a special "pending" result that signals the UI to enter ModeAsk\n  return tools.ToolResult{\n    Status: tools.ResultStatusSuccess,\n    Data:   json.Marshal(map[string]interface{}{"questions": questions}),\n    Pending: true, // signals interactive mode needed\n  }\n}
Acceptance: Tool registers with name 'ask'
Acceptance: Schema accepts questions array
Acceptance: DisplayValue shows single question label or count of questions
Acceptance: Execute validates all questions before entering interactive mode
Acceptance: Execute returns pending=true to signal interactive UI mode
Verification: cd /home/goglue/src/squid-os && go build ./...

### TASK: 3.2 - ModeAsk state and sequential wizard integration
Type: feature
What: Add ModeAsk to modes.go, askCtx to streamState, and handleAskKey in input.go that iterates questions sequentially using QuestionPrompt, collecting answers until all are complete.
Why: Wires the ask tool into the app flow. ModeAsk pauses the stream, shows questions one at a time via QuestionPrompt, collects answers, and returns the full answer array as the tool result when done.
Files: ~ internal/app/modes.go
Files: ~ internal/app/stream.go
Files: ~ internal/app/input.go
Files: ~ internal/app/app.go
Snippet: // modes.go\nconst (\n  // ... existing ...\n  ModeAsk // awaiting user answers to ask tool questions\n)\n\n// stream.go — in streamState\ntype streamState struct {\n  // ... existing ...\n  askCtx *ask.AskContext // non-nil when in ModeAsk\n}\n\n// app.go — in Model\nfunc (m *Model) setAskMode(questions []ask.Question) {\n  m.mode = ModeAsk\n  m.stream.active = false\n  m.stream.askCtx = &ask.AskContext{\n    Questions: questions,\n    Answers:   make([]ask.Answer, len(questions)),\n    CurrentQ:  0,\n  }\n  m.askPrompt = ui.NewQuestionPrompt(questions[0], m.width)\n}\n\nfunc (m *Model) handleAskKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {\n  ctx := m.stream.askCtx\n  complete, answer := m.askPrompt.HandleKey(msg)\n  if !complete {\n    return m, nil\n  }\n  \n  // Store answer for current question\n  ctx.Answers[ctx.CurrentQ] = answer\n  \n  // Check if question is valid (required fields filled)\n  if ctx.Questions[ctx.CurrentQ].Required && answer.IsEmpty() {\n    return m, nil // stay on this question\n  }\n  \n  // Advance to next question or finish\n  ctx.CurrentQ++\n  if ctx.CurrentQ >= len(ctx.Questions) {\n    return m.finishAsk(), nil\n  }\n  m.askPrompt = ui.NewQuestionPrompt(ctx.Questions[ctx.CurrentQ], m.width)\n  return m, nil\n}\n\nfunc (m *Model) finishAsk() (tea.Model, tea.Cmd) {\n  ctx := m.stream.askCtx\n  resultJSON, _ := json.Marshal(map[string]interface{}{\n    "answers": ctx.Answers,\n  })\n  \n  // Return as tool result and resume stream\n  m.stream.askCtx = nil\n  // ... save tool result, resume stream ...\n  return m, m.startStream()\n}\n\n// render.go\ncase ModeAsk:\n  if m.stream.askCtx != nil {\n    progress := fmt.Sprintf("Question %d/%d", m.stream.askCtx.CurrentQ+1, len(m.stream.askCtx.Questions))\n    sections = append(sections, style.Dim.Render(progress))\n    sections = append(sections, m.askPrompt.Render())\n  }\n  // No textarea in ask mode
Acceptance: ModeAsk added to mode enum
Acceptance: setAskMode initializes AskContext and first QuestionPrompt
Acceptance: handleAskKey processes input, stores answers, advances questions
Acceptance: Required questions block advancement until answered
Acceptance: finishAsk marshals answers and resumes stream
Acceptance: Render shows progress indicator and question prompt
Verification: cd /home/goglue/src/squid-os && go build ./...

### TASK: 3.3 - Wire ask tool into tool execution and stream resume
Type: feature
What: Add ask tool to the tool registry initialization and wire ModeAsk into handleStreamEvent so that when ask returns pending, the app enters ModeAsk instead of immediately returning a result.
Why: Completes the integration: the ask tool gets invoked like any other tool, but its pending result transitions to interactive ModeAsk. After all questions are answered, the result is returned to the AI and the stream resumes with the answer data.
Files: ~ internal/app/stream.go
Files: ~ internal/tools/tools.go
Snippet: // tools.go — registration\nfunc init() {\n  ask.GetTool(Registry)\n  // ... other tools ...\n}\n\n// stream.go — in handleStreamEvent tool_calls section\nif event.StopReason == "tool_calls" && len(m.stream.partialTools) > 0 {\n  toolEntries := (&m).executeTools(m.stream.partialTools)\n  if toolEntries == nil {\n    // Check if it's authorization or ask mode\n    if m.stream.authorizationCtx != nil {\n      (&m).setAuthMode()\n      m.updateViewportContent()\n      return m, nil\n    }\n    if m.stream.askCtx != nil {\n      m.updateViewportContent()\n      return m, nil\n    }\n  }\n  // ... existing flow ...\n}\n\n// finishAsk — resume with tool result\nfunc (m *Model) finishAsk() (tea.Model, tea.Cmd) {\n  ctx := m.stream.askCtx\n  answersJSON, _ := json.Marshal(ctx.Answers)\n  \n  // Create the tool result entry\n  entry := m.buildEmptyEntry(m.stream.partialTools[0])\n  entry.Execution.Status = tools.ResultStatusSuccess\n  entry.Execution.Content = string(answersJSON)\n  \n  // Save assistant message with tool result\n  (&m).appendAssistantMsg(config.Message{\n    Role:      config.RoleAssistant,\n    ToolCalls: []config.ToolCallEntry{entry},\n    StopReason: "tool_calls",\n  })\n  \n  m.stream.askCtx = nil\n  m.stream.reset()\n  m.mode = ModeChat\n  m.updateViewportContent()\n  return &m, (&m).startStream()\n}
Acceptance: Ask tool is registered in tool registry init
Acceptance: handleStreamEvent checks for askCtx pending state
Acceptance: finishAsk creates proper tool result entry with answers JSON
Acceptance: Stream resumes after all questions answered
Acceptance: Tool result is returned to AI as structured answer array
Verification: cd /home/goglue/src/squid-os && go build ./...

## MILESTONE: 4 - Authorization Refactor
Pattern: Extract Common Component / Adapter
Objective: Refactor the existing authorization prompt to use the QuestionPrompt component instead of its own yes/no logic, eliminating duplicate prompt patterns.
Success: Authorization mode uses QuestionPrompt with a yesno question type. The old AuthorizationPrompt struct is removed. All authorization keyboard handling flows through QuestionPrompt.HandleKey.
Diagram: AuthorizationContext -> creates Question(yesno) -> QuestionPrompt handles UI -> AuthResult extracted from Answer

### TASK: 4.1 - Refactor authorization to use QuestionPrompt component
Type: refactor
What: Replace AuthorizationPrompt struct with QuestionPrompt in internal/app/app.go. Convert AuthResult to use Answer value. Update setAuthMode to create a yesno Question and initialize QuestionPrompt. Remove old authorization.go UI code.
Why: Eliminates the duplicate prompt pattern. Authorization becomes a specific use of the Question component with a yesno question type. The answer maps directly: yes=approved, no=rejected, text=instructions.
Files: ~ internal/app/app.go
Files: ~ internal/app/input.go
Files: ~ internal/app/render.go
Files: ~ internal/app/stream.go
Files: - internal/ui/authorization.go
Snippet: // app.go — replace authPrompt field\ntype Model struct {\n  // ...\n  askPrompt *ui.QuestionPrompt // reused for both ModeAsk and ModeAuthorize\n}\n\n// setAuthMode — now creates a yesno question\nfunc (m *Model) setAuthMode() {\n  m.mode = ModeAuthorize\n  m.stream.active = false\n  ctx := m.stream.authorizationCtx\n  \n  // Build preview diff\n  var previewText string\n  if ctx != nil {\n    tool := m.toolReg.Get(ctx.ToolName)\n    if tool != nil && tool.Preview != nil {\n      result := tool.Preview(ctx.Args)\n      if result.Status == tools.ResultStatusSuccess && len(result.Files) > 0 {\n        previewText = result.Files[0].Diff\n      }\n    }\n  }\n  \n  // Create a yesno question for authorization\n  q := ask.Question{\n    ID:   "auth_" + ctx.ToolName,\n    Type: ask.QuestionYesNo,\n    Label: fmt.Sprintf("Execute %s?%s", ctx.ToolName, \n      func() string { if ctx.IsDestructive { return " ⚠️" } return "" }()),\n    TextPrompt: "Add instructions (optional)...",\n  }\n  // Make options carry the instruction context\n  q.Options = []ask.Option{\n    {Value: "yes", Label: "Yes", RequiresText: true, TextPrompt: "Add instructions for the AI..."},\n    {Value: "no", Label: "No", RequiresText: true, TextPrompt: "Reason (optional)..."},\n  }\n  \n  m.askPrompt = ui.NewQuestionPrompt(q, m.width)\n  m.askPrompt.PreviewDiff = previewText\n}\n\n// handleAuthorizeKey — now delegates to QuestionPrompt\nfunc (m *Model) handleAuthorizeKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {\n  complete, answer := m.askPrompt.HandleKey(msg)\n  if !complete {\n    return m, nil\n  }\n  \n  approved := answer.Value == "yes"\n  instructions := answer.Text\n  return m.resolveAuthorization(approved, instructions)\n}\n\n// Add PreviewDiff to QuestionPrompt struct\ntype QuestionPrompt struct {\n  // ... existing fields ...\n  PreviewDiff string // for authorization: shows diff preview\n}\n\n// In Render — after question, before hints\nif qp.PreviewDiff != "" {\n  // Show condensed diff preview\n  lines := strings.Count(qp.PreviewDiff, "\n")\n  sb.WriteString("\n  " + style.Dim.Render(fmt.Sprintf("%d lines changed", lines)) + "\n")\n}
Acceptance: AuthorizationPrompt struct is removed
Acceptance: askPrompt field reused for both ModeAsk and ModeAuthorize
Acceptance: setAuthMode creates a yesno Question with RequiresText on both options
Acceptance: handleAuthorizeKey delegates to QuestionPrompt.HandleKey
Acceptance: Answer maps to AuthResult: yes=approved, no=rejected, text=instructions
Acceptance: PreviewDiff field added to QuestionPrompt for diff display
Acceptance: Old authorization.go UI file is removed
Verification: cd /home/goglue/src/squid-os && go build ./...

## MILESTONE: 5 - System Prompt
Pattern: Instruction Injection
Objective: Update the system prompt so the LLM knows about the ask tool and understands when and how to use it.
Success: System prompt includes ask tool usage guidelines: when to collect info from user, how to structure questions, what types to use for what scenarios.
Diagram: sys-prompt.go -> DefaultAssistantPrompt() includes ask tool section -> injected into every API request

### TASK: 5.1 - Add ask tool documentation to system prompt
Type: feature
What: Add ask tool section to internal/config/sys-prompt.go explaining the 4 question types, when to use the tool (interviews, confirmations, structured data collection), and how to structure the questions array.
Why: The LLM needs to know about the ask tool so it can use it proactively when it needs to collect structured information from the user rather than just asking open-ended questions in chat.
Files: ~ internal/config/sys-prompt.go
Snippet: ## Ask Tool\n- Use the "ask" tool when you need to collect structured information from the user.\n- Ideal for: interviews, multi-step questionnaires, confirmations with options, preference gathering.\n- The tool accepts an array of questions and returns an array of answers.\n\n### Question Types\n- **yesno**: Yes/No binary choice. Use for confirmations and boolean questions.\n- **radio**: Single choice from a list of options. Use when the user must pick exactly one.\n- **checkbox**: Multiple choices from a list. Use when the user can select multiple options.\n- **text**: Free-form text input. Use for open-ended questions.\n\n### Structure\nEach question has: id, type, label, options (for radio/checkbox), textPrompt (optional), required (optional).\nEach option can have: value, label, requiresText, textPrompt.\n\n### Example\n{\n  "questions": [\n    {"id": "q1", "type": "radio", "label": "What is your role?",\n     "options": [{"value": "dev", "label": "Developer"}, {"value": "designer", "label": "Designer"}]},\n    {"id": "q2", "type": "text", "label": "Tell me about your experience", "required": true}\n  ]\n}\n\nThe result is an array of answers with value, text, and values (for checkbox) fields.
Acceptance: System prompt includes ask tool section with all 4 question types
Acceptance: Examples show proper JSON structure for questions
Acceptance: Describes when to use the tool vs regular chat
Acceptance: Mentions option-level requiresText and textPrompt
Verification: cd /home/goglue/src/squid-os && go build ./...
