# EPIC: Agent Runtime and CLI Architecture
Why: Squid-OS needs a clean explicit CLI, first-class agent presets, session continuation, autosave-aware run execution, memory scaffolding, and bounded agent tool delegation without scattering behavior across main.go and TUI-only paths.
Outcomes: A mode-driven CLI, global agent registry, expanded session metadata, shared turn preparation, memory namespace foundation, run output modes, autosave behavior, and phase-1 agent tools.

## MILESTONE: 1 - CLI Command Model
Pattern: Command Tree, Explicit Mode Selection
Objective: Refactor the existing Cobra CLI into explicit root, tui, run, server, and gnu command paths with stable flag semantics.
Success: Root and tui open the TUI, run executes non-TUI, server and gnu are visible not-implemented commands, prompt no longer implies headless, and CLI parsing is centralized outside main.go.
Diagram: flowchart TD
    Root[squid-os root] --> TUI[tui command]
    Root --> Run[run command]
    Root --> Server[server command]
    Root --> GNU[gnu command]
    TUI --> Textarea[prefill textarea]
    Run --> NewRun[new run]
    Run --> Continue[continue session]
    Run --> AgentRun[named agent run]

### TASK: 1.1 - Extract CLI command construction
Type: refactor
What: Create an internal CLI package that builds the Cobra command tree and keeps main.go as a thin bootstrap.
Why: Centralized command construction prevents CLI behavior from spreading through main.go as the command surface grows.
Files: + internal/cli
Files: ~ main.go
Snippet: package cli\n\ntype Dependencies struct {\n    // paths, settings, endpoints, history loaders, and program launch hooks\n}\n\nfunc NewRootCommand(deps Dependencies) *cobra.Command {\n    // Build root, tui, run, server, and gnu commands.\n}\n\nfunc Execute(deps Dependencies) error {\n    // Entrypoint called by main.go.\n}
Acceptance: main.go delegates command creation/execution to internal/cli.
Acceptance: Root, tui, run, server, and gnu commands are constructed in one package.
Acceptance: go build passes with no behavior changes beyond command routing.
Verification: go build ./...

### TASK: 1.2 - Implement root and TUI option parsing
Type: feature
What: Add root and tui options for session selection, prompt prefill, model, thinking, working directory, agent system, tool, skill, and callable agent scopes.
Why: TUI boot needs the same explicit session setup surface as run without treating prompt as headless execution.
Files: ~ internal/cli
Files: ~ internal/app/app.go
Files: ~ internal/app/ui_session.go
Snippet: type TUIOptions struct {\n    Prompt string\n    SessionName string\n    Model string\n    Thinking string\n    WorkingDir string\n    AgentName string\n    AgentSystem string\n    ToolNames []string\n    SkillNames []string\n    ActiveSkill string\n    CallableAgentNames []string\n}\n\nfunc RunTUI(opts TUIOptions, deps Dependencies) error {\n    // Load or create session, prefill textarea, then launch Bubble Tea.\n}
Acceptance: squid-os opens TUI.
Acceptance: squid-os tui opens TUI.
Acceptance: --prompt on root or tui pre-fills textarea and does not execute a run.
Acceptance: --session on root or tui opens the named saved session.
Acceptance: TUI bootstrap options are passed through a typed options object.
Verification: go build ./...

### TASK: 1.3 - Implement complete run option parsing
Type: feature
What: Add run options for inline runs, named agents, session continuation, stdin composition, output mode, autosave, auth, memory, scopes, active skill, and limits.
Why: run is the explicit non-TUI execution surface and must carry the full runtime override contract.
Files: ~ internal/cli
Files: ~ internal/headless/headless.go
Snippet: type RunOptions struct {\n    AgentName string\n    PositionalAgent string\n    SessionName string\n    Prompt string\n    Stdin string\n    Mode string\n    Model string\n    Thinking string\n    WorkingDir string\n    AgentSystem string\n    ToolNames []string\n    SkillNames []string\n    ActiveSkill string\n    CallableAgentNames []string\n    Autosave bool\n    AutosaveName string\n    AuthMode string\n    MemoryNamespace string\n    MemoryInstructions string\n    Limits LimitOptions\n}\n\ntype LimitOptions struct {\n    MaxSteps int\n    MaxTools int\n    MaxTime string\n    MaxToolResultTokens int\n}
Acceptance: squid-os run executes only through the explicit run command.
Acceptance: squid-os run name and squid-os run --agent name resolve equivalently.
Acceptance: Differing positional agent and --agent values fail clearly.
Acceptance: --prompt and stdin compose predictably.
Acceptance: --session adds a new turn to an existing session rather than rebuilding bootstrap.
Acceptance: Bootstrap replacement flags are rejected with --session where required by the prep docs.
Verification: go build ./...

### TASK: 1.4 - Preserve version and stub commands
Type: feature
What: Wire --version to the canonical version source and expose server and gnu as help-visible not-implemented commands.
Why: The expanded CLI must preserve existing version behavior while reserving future product surfaces cleanly.
Files: ~ internal/cli
Files: ~ internal/version.go
Files: ~ main.go
Snippet: type CommandInfo struct {\n    Name string\n    Short string\n    NotImplemented bool\n}\n\nfunc NewVersionCommandSource() string {\n    // Return canonical app version used by CLI, TUI header, and user agents.\n}
Acceptance: --version prints the canonical Squid-OS version and exits.
Acceptance: server appears in help and returns a clear not implemented response.
Acceptance: gnu appears in help as shell GNU tool generation migration surface and returns not implemented.
Verification: go build ./...

### TASK: 1.5 - Add bash and zsh completion
Type: feature
What: Add shell completion for command names, static enum values, and dynamic sessions, skills, agents, and tool names where practical.
Why: Completion is part of the CLI product contract and makes the larger command surface usable.
Files: ~ internal/cli
Files: ~ internal/config/session.go
Files: ~ internal/skills
Files: ~ internal/agent
Files: ~ internal/tools
Snippet: type CompletionProvider interface {\n    Sessions(prefix string) []string\n    Skills(prefix string) []string\n    Agents(prefix string) []string\n    Tools(prefix string) []string\n}\n\nfunc AttachCompletions(cmd *cobra.Command, provider CompletionProvider) {\n    // Register dynamic and static completions.\n}
Acceptance: --session completion suggests saved session names.
Acceptance: --skill and --skills completion suggest installed skills.
Acceptance: --agent and --agents completion suggest installed agents.
Acceptance: --tools completion suggests registered tools or a generated registry-backed list.
Acceptance: Static flags complete values for thinking, save, mode, memory namespace, and auth mode.
Verification: go build ./...

## MILESTONE: 2 - Agent Registry and Session Metadata
Pattern: Registry, Configuration Object, Provenance Metadata
Objective: Introduce global agents as first-class runtime presets with registry discovery, YAML loading, environment visibility, and session metadata.
Success: Agents live under config agents, can be listed and loaded, appear in environment, and sessions persist root Agent plus available Agents separately from Skill and Skills.
Diagram: classDiagram
    class AgentDefinition
    class AgentRegistry
    class SessionAgent
    class SessionAgents
    AgentRegistry --> AgentDefinition
    SessionAgent --> AgentDefinition
    SessionAgents --> AgentRegistry

### TASK: 2.1 - Add agent config path and registry
Type: feature
What: Create internal/agent with definition structs, YAML loading, registry scanning, and config path support for the agents directory.
Why: Agents become a first-class global preset system like skills instead of a run-command special case.
Files: + internal/agent
Files: ~ internal/config/paths.go
Files: ~ go.mod
Snippet: package agent\n\ntype Definition struct {\n    Name string\n    Description string\n    Mode string\n    AuthMode string\n    Model string\n    Thinking bool\n    WorkingDirectory string\n    System string\n    Tools []string\n    Skills []string\n    Agents []string\n    Memory MemoryConfig\n    Limits LimitsConfig\n}\n\ntype Entry struct {\n    Name string\n    Description string\n    Path string\n}\n\ntype Registry struct {\n    // Scanned agent entries indexed by name.\n}\n\nfunc InitRegistry(baseDir string) (*Registry, error) {\n    // Create agents directory and scan definitions.\n}
Acceptance: Paths includes an Agents directory under the Squid-OS config root.
Acceptance: EnsureDirs creates the agents directory.
Acceptance: Registry lists agent names and descriptions.
Acceptance: Registry loads a full definition by name.
Verification: go build ./...

### TASK: 2.2 - Add Agent and Agents session metadata
Type: feature
What: Extend SessionMeta with singular Agent provenance and plural Agents availability scope.
Why: The root agent that started a session is provenance, while Agents is the callable subagent access list.
Files: ~ internal/config/session.go
Files: ~ internal/chat/session.go
Snippet: type SessionAgent struct {\n    Name string\n}\n\ntype SessionAgents struct {\n    Initial []string\n    Current []string\n}\n\ntype SessionMeta struct {\n    Agent SessionAgent\n    Agents SessionAgents\n    // Existing session metadata remains distinct.\n}
Acceptance: Session Agent stores the root owning agent name without initial/current.
Acceptance: Session Agents stores initial/current callable agent names.
Acceptance: Agent remains distinct from Skill.
Acceptance: Agents remains distinct from Skills and Tools.
Verification: go build ./...

### TASK: 2.3 - Expose agents in environment
Type: feature
What: Initialize the agent registry before session bootstrap and add an Agents section to the generated environment.
Why: The model should discover available agents the same way it discovers skills.
Files: ~ internal/environment/environment.go
Files: ~ internal/environment/loader.go
Files: ~ internal/app/app.go
Files: ~ internal/headless/headless.go
Snippet: type AgentInfo struct {\n    Name string\n    Description string\n}\n\ntype Environment struct {\n    Agents []AgentInfo\n    // Existing environment sections remain.\n}\n\nfunc FormatEnvironment(env Environment) string {\n    // Render Agents section like Skills.\n}
Acceptance: Environment includes an Agents section with name and description.
Acceptance: Agent registry initialization happens before NewSession environment injection.
Acceptance: Existing Skills environment behavior remains unchanged.
Verification: go build ./...

## MILESTONE: 3 - Runtime Options and Turn Preparation
Pattern: Application Service, Explicit State Transition
Objective: Resolve settings, agent definitions, CLI options, and session continuation into concrete session/run options, then prepare each turn consistently before streaming.
Success: TUI, run, session continuation, and agent tools use the same resolved options and transition injection happens after the user message and before stream start.
Diagram: sequenceDiagram
    participant Entry as CLI or TUI
    participant Resolve as ResolveOptions
    participant Session as ChatSession
    participant Turn as PrepareTurn
    participant Loop as RunLoop
    Entry->>Resolve: settings agent flags session
    Resolve->>Session: create or load session
    Entry->>Session: append user message
    Entry->>Turn: desired next turn options
    Turn->>Session: inject transitions
    Entry->>Loop: stream prepared session

### TASK: 3.1 - Add runtime option resolver
Type: feature
What: Add a small resolver that maps settings, optional agent definition, CLI options, and session continuation context into concrete runtime options.
Why: One resolver prevents precedence, model parsing, autosave, auth, and scope rules from diverging across TUI, run, and agent tools.
Files: + internal/runtime
Files: ~ internal/cli
Files: ~ internal/agent
Files: ~ internal/config/settings.go
Snippet: package runtime\n\ntype Inputs struct {\n    Settings config.Settings\n    Agent *agent.Definition\n    CLI CLIOptions\n    ExistingSession *config.SessionDoc\n}\n\ntype Options struct {\n    AgentName string\n    Provider string\n    Model string\n    Thinking config.ThinkingConfig\n    AgentSystem string\n    ToolNames []string\n    SkillNames []string\n    ActiveSkill string\n    CallableAgentNames []string\n    Autosave AutosaveOptions\n    AuthMode string\n    Memory memory.Config\n    Limits Limits\n    OutputMode string\n}\n\nfunc Resolve(inputs Inputs) (Options, error) {\n    // Apply CLI, then agent, then settings precedence.\n}
Acceptance: CLI values override agent definition values.
Acceptance: Agent definition values override settings defaults.
Acceptance: Combined provider/model strings are parsed consistently.
Acceptance: --session rejects bootstrap replacement options such as root agent and agent system replacement.
Acceptance: Resolved options include tools, skills, active skill, callable agents, autosave, auth, memory, and limits.
Verification: go build ./...

### TASK: 3.2 - Expand session bootstrap config
Type: feature
What: Extend SessionConfig and NewSession so resolved runtime options create complete session metadata and bootstrap messages.
Why: Session creation should receive explicit inputs for agent, scopes, memory, autosave, auth, and limits instead of reading scattered global state.
Files: ~ internal/chat/session.go
Files: ~ internal/config/session.go
Files: ~ internal/environment/loader.go
Snippet: type SessionConfig struct {\n    Provider string\n    Model string\n    Thinking config.ThinkingConfig\n    SystemPromptFile string\n    AgentName string\n    AgentSystem string\n    ToolNames []string\n    SkillNames []string\n    ActiveSkill string\n    CallableAgentNames []string\n    WorkingDir string\n    Memory memory.Config\n    Autosave AutosaveOptions\n    AuthMode string\n    Limits Limits\n}\n\nfunc NewSession(cfg SessionConfig, paths config.Paths) *Session {\n    // Append Base System, Environment System, optional Agent System, config, and tools metadata.\n}
Acceptance: New sessions persist root Agent and callable Agents metadata.
Acceptance: New sessions can set active Skill separately from available Skills.
Acceptance: NewSession injects agent0 when Agent System text exists.
Acceptance: Base System, Environment System, and Agent System remain separate concepts.
Verification: go build ./...

### TASK: 3.3 - Add shared PrepareTurn
Type: feature
What: Move next-turn transition injection into a shared PrepareTurn function called after user append and before StartStream.
Why: Model, thinking, active skill, and availability changes must behave the same in TUI, run, session continuation, and future automation.
Files: + internal/chat/turn.go
Files: ~ internal/chat/session.go
Files: ~ internal/app/stream.go
Files: ~ internal/run
Snippet: type PrepareTurnOptions struct {\n    Inference config.InferenceConfig\n    ActiveSkill string\n    ToolNames []string\n    SkillNames []string\n    CallableAgentNames []string\n}\n\nfunc PrepareTurn(s *Session, opts PrepareTurnOptions) error {\n    // Insert transition messages after the latest user message.\n    // Update current inference and availability scopes.\n}\n
Acceptance: PrepareTurn inserts transitions immediately after the user message they affect.
Acceptance: TUI sendMessage no longer owns model/thinking/skill transition logic.
Acceptance: run --session uses PrepareTurn before streaming.
Acceptance: StartStream remains focused on starting provider streaming and does not hide history mutation.
Verification: go build ./...

### TASK: 3.4 - Model autosave as session runtime config
Type: feature
What: Represent autosave settings in resolved options and session metadata so TUI and run can persist automatically through one concept.
Why: Autosave is the existing persistence behavior and should replace incognito-style special cases and run-only save wording.
Files: ~ internal/config/session.go
Files: ~ internal/config/settings.go
Files: ~ internal/runtime
Files: ~ internal/app/session_persistence.go
Snippet: type AutosaveOptions struct {\n    Enabled bool\n    Name string\n}\n\ntype SessionMeta struct {\n    Autosave AutosaveOptions\n    // Existing metadata omitted.\n}\n
Acceptance: Resolved runtime options include autosave enabled and optional autosave name.
Acceptance: Session metadata can persist autosave configuration.
Acceptance: Manual save remains separate from autosave.
Acceptance: Existing TUI autosave behavior can be mapped to the new config.
Verification: go build ./...

## MILESTONE: 4 - Memory Foundation
Pattern: Configuration Scaffold, Namespace Resolution
Objective: Add memory namespace config, path resolution, session metadata, and environment exposure without implementing a full memory engine.
Success: workspace, global, and agent memory resolve to clear paths, memory instructions appear in environment, and memory metadata persists on sessions.
Diagram: flowchart TD
    Agent[agent memory config] --> Resolve[resolve memory]
    CLI[CLI memory override] --> Resolve
    Settings[settings memory dir] --> Resolve
    WorkDir[working directory] --> Resolve
    Resolve --> Session[session memory metadata]
    Resolve --> Env[environment memory section]

### TASK: 4.1 - Add memory config and namespace paths
Type: feature
What: Create internal/memory with config types and helpers that resolve workspace, global, and agent namespaces to concrete paths.
Why: Memory needs a stable structural foundation before any retrieval or writeback engine exists.
Files: + internal/memory
Files: ~ internal/config/paths.go
Files: ~ internal/runtime
Snippet: package memory\n\ntype Namespace string\n\nconst (\n    NamespaceWorkspace Namespace = "workspace"\n    NamespaceGlobal Namespace = "global"\n    NamespaceAgent Namespace = "agent"\n)\n\ntype Config struct {\n    Namespace Namespace\n    Path string\n    Instructions string\n}\n\nfunc ResolvePath(namespace Namespace, workingDir string, paths config.Paths, agentName string) (string, error) {\n    // workspace -> workingDir/memory\n    // global -> configured memory directory\n    // agent -> config agents agent-name memory\n}
Acceptance: workspace namespace resolves to working-directory/memory.
Acceptance: global namespace resolves to configured memory dir.
Acceptance: agent namespace resolves under config agents agent-name memory.
Acceptance: No memory retrieval or writeback engine is added in this task.
Verification: go build ./...

### TASK: 4.2 - Persist and expose memory config
Type: feature
What: Add memory metadata to sessions and update environment generation to show namespace, resolved path, instructions, and current memory index content.
Why: The model needs explicit memory intent and location even while the memory engine remains deferred.
Files: ~ internal/config/session.go
Files: ~ internal/environment/environment.go
Files: ~ internal/environment/loader.go
Files: ~ internal/chat/session.go
Snippet: type SessionMemory struct {\n    Namespace string\n    Path string\n    Instructions string\n}\n\ntype Environment struct {\n    MemoryNamespace string\n    MemoryPath string\n    MemoryInstructions string\n    Memory string\n}\n
Acceptance: Session metadata stores resolved memory namespace, path, and instructions.
Acceptance: Environment includes memory namespace and resolved path.
Acceptance: Environment includes memory instructions when configured.
Acceptance: Existing memory index display still works.
Verification: go build ./...

## MILESTONE: 5 - Run Execution and Output Modes
Pattern: Headless Runner, Output Adapter, Autosave
Objective: Make run execute prepared sessions through the shared loop with autosave, session continuation, auth mapping, limits, and output modes.
Success: run supports final_message, silent, stream JSONL, structured not implemented, autosaves saved runs, and keeps stdout/stderr contracts clean.
Diagram: flowchart TD
    Run[run command] --> Resolve[resolve options]
    Resolve --> Session[create or load session]
    Session --> Turn[prepare turn]
    Turn --> Loop[run loop]
    Loop --> Output[output adapter]
    Loop --> Autosave[autosave if enabled]

### TASK: 5.1 - Build run execution service
Type: feature
What: Create a run package that creates or loads a session, appends prompt input, prepares the turn, runs RunLoop, and applies autosave.
Why: run should become the canonical non-TUI execution path instead of a special headless shortcut.
Files: + internal/run
Files: ~ internal/headless/headless.go
Files: ~ internal/chat/loop.go
Files: ~ internal/cli
Snippet: package run\n\ntype Request struct {\n    Options runtime.Options\n    SessionName string\n    Prompt string\n}\n\ntype Result struct {\n    FinalText string\n    SavedSessionName string\n}\n\nfunc Execute(ctx context.Context, req Request) (Result, error) {\n    // Load or create session, append prompt, prepare turn, run loop, autosave.\n}
Acceptance: run executes a fresh inline prompt without launching TUI.
Acceptance: run with a named agent creates a session from that agent.
Acceptance: run --session appends a new turn to an existing saved session.
Acceptance: autosave writes a session when enabled and reports only the saved session name.
Verification: go build ./...

### TASK: 5.2 - Implement run output modes
Type: feature
What: Add output adapters for final_message, silent, stream, and structured mode handling.
Why: Automation needs predictable stdout and stderr behavior for each run mode.
Files: ~ internal/run
Files: ~ internal/cli
Snippet: type OutputMode string\n\nconst (\n    OutputFinalMessage OutputMode = "final_message"\n    OutputStream OutputMode = "stream"\n    OutputSilent OutputMode = "silent"\n    OutputStructured OutputMode = "structured"\n)\n\ntype OutputWriter interface {\n    WriteResult(result Result) error\n}\n
Acceptance: final_message prints the final assistant answer to stdout.
Acceptance: silent prints no final assistant answer to stdout.
Acceptance: saved session name goes to stderr for final_message and silent when autosaved.
Acceptance: structured returns a clear not implemented error for phase 1.
Verification: go build ./...

### TASK: 5.3 - Add CLI stream JSONL protocol
Type: feature
What: Map loop events and selected session lifecycle events into JSONL stream output with timestamps, roles, message IDs, and tool call IDs.
Why: stream mode needs a stable machine-readable CLI boundary without redesigning provider streaming.
Files: ~ internal/run
Files: ~ internal/chat/loop.go
Snippet: type StreamEnvelope struct {\n    Event string\n    Timestamp string\n    Role string\n    MessageID string\n    ToolCallID string\n    Tool string\n    Text string\n    Saved bool\n    SessionName string\n    StopReason string\n}\n\nfunc WriteStreamEvent(env StreamEnvelope) error {\n    // Write one JSON object per line.\n}
Acceptance: stream emits session_start as the first JSONL event.
Acceptance: session_start includes saved session name only when saved.
Acceptance: stream events include timestamps.
Acceptance: message, thinking, tool call, tool execution, permission request, error, session_event, and finished events are valid JSONL.
Acceptance: stream mode does not duplicate saved session metadata to stderr on success.
Verification: go build ./...

### TASK: 5.4 - Apply run auth and limits
Type: feature
What: Map agent/run auth modes onto existing authorization behavior and enforce practical phase-1 run limits.
Why: Non-TUI runs and agent tools must be deterministic and bounded without inventing separate auth or limit systems.
Files: ~ internal/run
Files: ~ internal/chat/loop.go
Files: ~ internal/chat/tool_exec.go
Files: ~ internal/runtime
Snippet: type Limits struct {\n    MaxSteps int\n    MaxTools int\n    MaxTime time.Duration\n    MaxToolResultTokens int\n}\n\ntype AuthMode string\n\nfunc ApplyRunPolicies(opts runtime.Options) chat.RunLoopOptions {\n    // Map auth and limits into loop/tool execution options.\n}
Acceptance: max tool result tokens continues to be enforced.
Acceptance: max steps, max tools, and max time are modeled and enforced where practical.
Acceptance: auth-required behavior maps to configured non-TUI policy.
Acceptance: No separate agent-tool auth subsystem is introduced.
Verification: go build ./...

## MILESTONE: 6 - Agent Tools
Pattern: Tool Adapter, Registry Guard, Blocking Delegation
Objective: Add list_agents, call_agent, and inline_agent tools using the agent registry and run execution model.
Success: The model can list agents, call an allowed named agent as a blocking final-answer subtask, and run inline agent definitions from tool arguments.
Diagram: flowchart TD
    Parent[parent session] --> Tool[agent tool]
    Tool --> List[list_agents]
    Tool --> Call[call_agent]
    Tool --> Inline[inline_agent]
    Call --> RunNamed[run named agent]
    Inline --> RunInline[run inline profile]
    RunNamed --> Result[final answer result]
    RunInline --> Result

### TASK: 6.1 - Register list_agents tool
Type: feature
What: Add a list_agents tool that returns agent names and descriptions using the agent registry and documented callable-agent behavior.
Why: The model needs explicit agent discovery just like it has skill_list for skills.
Files: + internal/tools/agents.go
Files: ~ internal/tools/tools.go
Files: ~ internal/agent
Snippet: var ListAgentsTool = Tool{\n    Name: "list_agents"\n    Description: "List available agents with names and descriptions."\n    // Schema has no required arguments.\n}\n\nfunc executeListAgents(args map[string]interface{}) ToolResult {\n    // Read agent registry and return compact name plus description list.\n}
Acceptance: list_agents is registered in the tool registry.
Acceptance: list_agents returns agent names and descriptions.
Acceptance: The implementation clearly chooses or labels installed versus callable agent listing behavior.
Verification: go build ./...

### TASK: 6.2 - Register call_agent tool
Type: feature
What: Add call_agent as a blocking named-agent tool with arguments for agent name, prompt, and limits.
Why: Provides simple phase-1 sub-agent delegation while keeping full child-session orchestration out of scope.
Files: ~ internal/tools/agents.go
Files: ~ internal/run
Files: ~ internal/agent
Files: ~ internal/chat/tool_exec.go
Snippet: type CallAgentArgs struct {\n    Agent string\n    Prompt string\n    Limits Limits\n}\n\nfunc executeCallAgent(args CallAgentArgs, ctx ToolContext) ToolResult {\n    // Verify Agent is allowed by current session Agents scope.\n    // Execute named agent through run service with final-answer output.\n}\n
Acceptance: call_agent requires agent and prompt.
Acceptance: call_agent accepts limits for the delegated subtask.
Acceptance: call_agent rejects agents not allowed by the current session Agents scope.
Acceptance: called agent uses its own configured tools, skills, agents, memory, system, and model.
Acceptance: call_agent returns only the final answer as the tool result.
Verification: go build ./...

### TASK: 6.3 - Register inline_agent tool
Type: feature
What: Add inline_agent as a blocking tool whose agent-like profile is defined directly in tool arguments instead of an installed preset.
Why: Supports ad hoc delegation without requiring users to create a registry agent file first.
Files: ~ internal/tools/agents.go
Files: ~ internal/run
Files: ~ internal/runtime
Snippet: type InlineAgentArgs struct {\n    Prompt string\n    System string\n    Model string\n    ToolNames []string\n    SkillNames []string\n    CallableAgentNames []string\n    AuthMode string\n    Limits Limits\n}\n\nfunc executeInlineAgent(args InlineAgentArgs, ctx ToolContext) ToolResult {\n    // Build runtime options from inline arguments and run blocking.\n}\n
Acceptance: inline_agent does not require registry lookup.
Acceptance: inline_agent profile source is the tool arguments.
Acceptance: inline_agent runs blocking and returns the final answer.
Acceptance: inline_agent uses the normal run execution path.
Verification: go build ./...

## MILESTONE: 7 - Cleanup and Validation
Pattern: Strangler Cleanup, Regression Tests
Objective: Remove obsolete incognito and legacy CLI behavior, then validate the new architecture with focused regression tests.
Success: Incognito and implicit headless prompt behavior are gone, version remains preserved, and tests cover CLI parsing, agents, runtime resolution, PrepareTurn, run output, and agent tools.
Diagram: flowchart TD
    Legacy[legacy CLI behavior] --> Remove[remove hidden mode switches]
    Incognito[incognito mode] --> Autosave[explicit autosave config]
    Tests[focused tests] --> Build[go build and go test]
    Build --> Done[ready for implementation review]

### TASK: 7.1 - Drop incognito CLI flag
Type: refactor
What: Remove the incognito CLI flag from main.go. Incognito toggle (alt+i) remains in the app — it keeps the session alive and just stops saving/history.
Why: The incognito feature is useful for privacy (no history, no save). Keep it as an in-app toggle, just remove the CLI startup flag.
Files: ~ main.go
Files: ~ internal/cli
Files: ~ internal/keymap.go
Snippet: func main() {\n    // incognito flag removed from CLI parsing\n    // incognito remains as in-app toggle (alt+i)\n}
Acceptance: No incognito flag in CLI argument parsing.
Acceptance: App still toggles incognito via alt+i.
Acceptance: Incognito still blocks history and session saves.
Acceptance: Toggle off reloads session from disk (discarding incognito changes).
Verification: go build ./...

### TASK: 7.2 - Remove legacy root CLI behavior
Type: refactor
What: Remove implicit headless-on-prompt behavior and legacy root image handling that no longer fit the explicit command model.
Why: Root command should consistently mean TUI while non-TUI execution belongs only under run.
Files: ~ internal/cli
Files: ~ internal/headless/headless.go
Files: ~ main.go
Snippet: func RunRoot(opts TUIOptions) error {\n    // Root always launches TUI.\n}\n\nfunc RunExplicit(opts RunOptions) error {\n    // Non-TUI execution lives under squid-os run.\n}\n
Acceptance: Root --prompt does not start non-TUI execution.
Acceptance: Non-TUI execution requires the run command.
Acceptance: Unsupported legacy flags are removed or rejected clearly.
Verification: go build ./...

### TASK: 7.3 - Add focused regression tests
Type: test
What: Add tests for CLI parsing, agent registry loading, runtime precedence, PrepareTurn injection, run outputs, memory paths, and agent tools.
Why: The new architecture crosses many entrypoints and needs tests to prevent behavior drift.
Files: + internal/cli/*_test.go
Files: + internal/agent/*_test.go
Files: + internal/runtime/*_test.go
Files: + internal/chat/*_test.go
Files: + internal/run/*_test.go
Files: + internal/memory/*_test.go
Snippet: func TestRunAgentFlagConflict(t *testing.T) {\n    // Positional agent and --agent mismatch should fail.\n}\n\nfunc TestPrepareTurnTransitionPlacement(t *testing.T) {\n    // Transitions should appear after the user message.\n}\n\nfunc TestWorkspaceMemoryPath(t *testing.T) {\n    // workspace memory resolves to working-directory/memory.\n}\n
Acceptance: Tests cover positional agent and --agent equivalence and conflict.
Acceptance: Tests cover --prompt plus stdin composition.
Acceptance: Tests cover --session continuation rejecting bootstrap replacement flags.
Acceptance: Tests cover PrepareTurn transition placement.
Acceptance: Tests cover stream session_start output and saved session name behavior.
Acceptance: Tests cover agent registry scan/load and agent tool contracts.
Verification: go test ./...
