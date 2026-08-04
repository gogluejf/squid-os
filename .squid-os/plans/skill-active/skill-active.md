# EPIC: Active Skill State
Why: Track loaded skill on the session like working_dir, allow Tab cycling, and inject a synthetic message on change when the user sends their next message. State lives entirely in SessionSkill struct — no Model-level tracking.
Outcomes: User can cycle skills with Tab, skill_load sets Next, change is reflected via synthetic message on next turn, persists on save/load, cleared on new session

## MILESTONE: 1 - Config
Pattern: Data struct (self-contained in Session)
Objective: Add SessionSkill struct with Current and Next to the Session config
Success: Session file persists both Current and Next, restored on load, footer can read it
Diagram: classDiagram
    class SessionSkill {
        +Current string
        +Next string
    }
    class Session {
        +Skill SessionSkill
    }
    class SessionFile {
        +Session Session
    }
    SessionFile --> Session
    Session --> SessionSkill

### TASK: 1.1 - Add SessionSkill to config
Type: feature
What: Add SessionSkill struct and embed it in Session in config/session.go
Why: Self-contained skill state on the session — Current is committed, Next is pending change
Files: ~ internal/config/session.go
Snippet: type SessionSkill struct {\n    Current string `json:"current"`\n    Next    string `json:"next"`\n}\n\ntype Session struct {\n    // ... existing fields ...\n    Skill SessionSkill `json:"skill"`\n}
Acceptance: Both fields always persist (no omitempty)
Acceptance: Default zero value is {"": ""} — no skill
Verification: go build ./...

## MILESTONE: 2 - Tab Cycling
Pattern: Key binding on session state
Objective: Tab in ModeChat cycles through [none] + registered skills, sets Next, autosaves Current
Success: Tab cycles skills on session.Skill, shows notification, autosaves immediately
Diagram: stateDiagram-v2
    [*] --> none
    none --> skillA
    skillA --> skillB
    skillB --> none

### TASK: 2.1 - Implement cycleSkill on Tab
Type: feature
What: Intercept Tab in ModeChat to cycle skills, set Next, autosave
Why: Let the user manually switch the loaded skill via Tab
Files: ~ internal/app/input.go
Files: ~ internal/app/app.go
Snippet: case msg.Type == tea.KeyTab && !msg.Alt && m.mode == ModeChat:\n    return m.cycleSkill()
Snippet: func (m Model) cycleSkill() (Model, tea.Cmd) {\n    options := []string{""}\n    if reg := skills.GetRegistry(); reg != nil {\n        for _, e := range reg.List() {\n            options = append(options, e.Name)\n        }\n    }\n    current := m.session.file.Session.Skill.Next\n    if current == "" {\n        current = m.session.file.Session.Skill.Current\n    }\n    idx := 0\n    for i, s := range options {\n        if s == current {\n            idx = i\n            break\n        }\n    }\n    next := options[(idx+1)%len(options)]\n    m.session.file.Session.Skill.Next = next\n    label := next\n    if next == "" {\n        label = "[no skill]"\n    }\n    m.setNotification(ui.NotificationInfo, "skill: " + label)\n    m.updateViewportContent()\n    return m.autoSave()\n}
Acceptance: Tab cycles: empty -> skill1 -> skill2 -> ... -> empty (loop)
Acceptance: If Next is already set, cycling continues from Next (not Current)
Acceptance: Sets Next on session.Skill
Acceptance: Autosaves after each cycle
Acceptance: Shows notification with skill name or [no skill]
Verification: go build ./...

## MILESTONE: 3 - Tool Side-Effect
Pattern: Post-execution hook (mirrors set_working_dir in stream.go)
Objective: skill_load tool sets Skill.Next as a side effect, autosaves
Success: After skill_load succeeds, session.Skill.Next is set and autosaved
Diagram: sequenceDiagram
    participant M as Model
    participant T as Tool Execute Loop
    participant S as skill_load
    M->>T: Execute tool call
    T->>S: skill_load(args)
    S-->>T: result success
    T->>M: if name == skill_load
    T->>M: session.Skill.Next = args.name
    M->>M: autoSave

### TASK: 3.1 - skill_load sets Next post-execution
Type: feature
What: Add post-execution hook for skill_load in stream.go next to set_working_dir
Why: When the model calls skill_load, set Skill.Next and autosave so the footer reflects it
Files: ~ internal/app/stream.go
Snippet: // After set_working_dir block in stream.go tool loop:\nif p.name == "skill_load" && result.Status == tools.ResultStatusSuccess {\n    if name, ok := args["name"].(string); ok {\n        m.session.file.Session.Skill.Next = name\n        m, _ = m.autoSave()\n    }\n}
Acceptance: After successful skill_load, session.Skill.Next is set
Acceptance: Autosaves after setting Next
Acceptance: Failed skill_load does not change Next
Verification: go build ./...

## MILESTONE: 4 - Synthetic Message on Send
Pattern: Pre-send check in sendMessage
Objective: On user send, if Next != Current inject synthetic skill-load message, then liquidate Next into Current
Success: Transition produces synthetic with label skill-load and param name; Next is cleared, Current updated; no extra save needed (autosave already ran)
Diagram: flowchart LR
    A[sendMessage] --> B{Skill.Next != Skill.Current}
    B -->|yes| C[Generate synthetic skill-load msg]
    B -->|no| D[append user msg]
    C --> E[append synthetic before user msg]
    E --> D
    D --> F[Current = Next, Next = ""]

### TASK: 4.1 - Inject synthetic skill-load message on send
Type: feature
What: In sendMessage(), before appending user message, check if Skill.Next != Skill.Current and inject synthetic skill-load message, then liquidate
Why: When the skill has changed, the model needs to know via a synthetic message before the user message
Files: ~ internal/app/stream.go
Snippet: func (m Model) sendMessage() (Model, tea.Cmd) {\n    // ... existing start ...\n\n    // Check for skill change\n    if m.session.file.Session.Skill.Next != m.session.file.Session.Skill.Current {\n        old := m.session.file.Session.Skill.Current\n        nxt := m.session.file.Session.Skill.Next\n        m.injectSkillChangeSynthetic(old, nxt)\n        m.session.file.Session.Skill.Current = nxt\n        m.session.file.Session.Skill.Next = ""\n    }\n\n    // ... rest of sendMessage (append user msg, start stream) ...
Snippet: func (m *Model) injectSkillChangeSynthetic(old string, nxt string) {\n    var text string\n    if nxt == "" {\n        text = fmt.Sprintf("Skill %q has been unloaded by the user. Don't use the previously loaded skill anymore.", old)\n    } else if old == "" {\n        text = m.getSkillText(nxt)\n    } else {\n        text = fmt.Sprintf("Skill changed from %q to %q. Stop using the previous skill and use the new one instead.\n\n", old, nxt)\n        text += m.getSkillText(nxt)\n    }\n    m.session.appendMsg(config.Message{\n        ID:          fmt.Sprintf("msg_%d", len(m.session.file.Messages)+1),\n        Role:        config.RoleSynthetic,\n        CreatedAt:   time.Now(),\n        Text:        text,\n        Label:       "skill-load",\n        Params:      map[string]string{"name": nxt},\n        InputTokens: countTokensApprox(text),\n    })\n}
Snippet: func (m *Model) getSkillText(name string) string {\n    reg := skills.GetRegistry()\n    if reg == nil {\n        return fmt.Sprintf("Loaded skill: %s", name)\n    }\n    sk, err := reg.Load(name)\n    if err != nil {\n        return fmt.Sprintf("Loaded skill: %s", name)\n    }\n    text := fmt.Sprintf("Loaded skill: %s\n\n", name)\n    if sk.Body != "" {\n        text += sk.Body\n    }\n    return text\n}
Acceptance: empty -> skill: synthetic 'Loaded skill: X' + body, param name=X
Acceptance: skill-A -> skill-B: synthetic 'Skill changed from A to B' + new body, param name=B
Acceptance: skill-A -> empty: synthetic 'Skill A unloaded', param name=empty
Acceptance: Label is skill-load
Acceptance: After synthetic injection, Current = Next, Next cleared
Acceptance: If Next == Current, no synthetic injected
Verification: go build ./...

## MILESTONE: 5 - Clear State
Pattern: Reset on session lifecycle
Objective: Zero out Skill on new session / incognito
Success: No leftover skill state when starting fresh
Diagram: flowchart LR
    A[clearSession] --> B[Skill = SessionSkill{}]
    C[toggleIncognito] --> B

### TASK: 5.1 - Clear Skill on new session / incognito
Type: feature
What: Reset session.file.Session.Skill to zero value in clearSession() and toggleIncognito()
Why: A fresh session should not carry over a previously loaded skill
Files: ~ internal/app/session.go
Snippet: func (m Model) clearSession() (Model, tea.Cmd) {\n    m.session.file.Session.Skill = config.SessionSkill{}\n    // ...
Snippet: func (m Model) toggleIncognito() (Model, tea.Cmd) {\n    m.session.file.Session.Skill = config.SessionSkill{}\n    // ...
Acceptance: clearSession zeros both Current and Next
Acceptance: toggleIncognito zeros both Current and Next
Verification: go build ./...

## MILESTONE: 6 - Footer Display
Pattern: UI render using session state
Objective: Footer shows skill state: Next if set, otherwise Current
Success: Footer displays current/pending skill info from SessionSkill
Diagram: flowchart LR
    A[Footer render] --> B{Skill.Next != ""}
    B -->|yes| C[display Next]
    B -->|no| D{Skill.Current != ""}
    D -->|yes| E[display Current]
    D -->|no| F[display none]

### TASK: 6.1 - Display skill in footer
Type: feature
What: Add SessionSkill to FooterData, display it in line2 footer (after auth mode, before working dir)
Why: User needs to see what skill is currently loaded or pending
Files: ~ internal/ui/footer.go
Files: ~ internal/app/render.go
Snippet: type FooterData struct {\n    // ... existing fields ...\n    Skill config.SessionSkill\n}
Snippet: // In buildFooterData:\nreturn ui.FooterData{\n    // ... existing ...\n    Skill: m.session.file.Session.Skill,\n}
Snippet: // In RenderFooter line2, after authLabel:\nvar skillLabel string\nif data.Skill.Next != "" {\n    skillLabel = style.FooterValueStyle.Render("[skill: " + data.Skill.Next + "]")\n} else if data.Skill.Current != "" {\n    skillLabel = style.FooterValueStyle.Render("[skill: " + data.Skill.Current + "]")\n}\nleft2 := thinkLabel + authLabel + skillLabel + style.FooterValueStyle.Render(" ") + curDirLabel
Acceptance: When Next is set, footer shows [skill: Next]
Acceptance: When Next is empty but Current is set, footer shows [skill: Current]
Acceptance: When both empty, no skill label shown
Acceptance: Skill label appears between auth mode and working dir
Verification: go build ./...
