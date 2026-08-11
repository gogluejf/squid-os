package chat

import (
	"encoding/json"
	"squid-os/internal/config"
	"squid-os/internal/tools"
)

// CompactionDecision describes whether a tool call's arguments and result
// should be compacted (replaced with placeholder text) because a later
// full checkpoint supersedes them.
type CompactionDecision struct {
	ToolCallID       string
	Path             string
	Trace            string
	CompactArguments bool
	CompactResult    bool
	SupersededBy     string // ToolCallID of the checkpoint that supersedes this event
}

// CompactionTokens holds raw, retained, and saved token counts for
// both instruction (arguments) and execution (result) content.
type CompactionTokens struct {
	RawInstruction      int
	RawExecution        int
	Raw                 int
	RetainedInstruction int
	RetainedExecution   int
	Retained            int
	SavedInstruction    int
	SavedExecution      int
	Saved               int
}

// CompactionPlan is the immutable output of BuildCompactionPlan.
// Decisions maps tool-call ID to its compaction decision.
// Tokens holds the aggregated token accounting.
type CompactionPlan struct {
	Decisions map[string]CompactionDecision
	Tokens    CompactionTokens
}

// fileEvent represents a single file-related tool call extracted from history.
type fileEvent struct {
	toolCallID        string
	toolName          string
	path              string
	trace             string
	isCheckpoint      bool // true for full reads, writes, creates
	isRangedRead      bool // true for partial observations
	isSuccessful      bool
	instructionTokens int
	executionTokens   int
	argsJSON          string // original instruction arguments JSON
}

// BuildCompactionPlan scans all messages once, extracts file-tool events,
// groups them by canonical path, identifies checkpoints, and produces
// per-tool-call compaction decisions with token summaries.
//
// The function does not mutate the input messages.
func BuildCompactionPlan(messages []config.Message) CompactionPlan {
	// Single pass: extract all file events
	var events []fileEvent
	for _, msg := range messages {
		for _, tc := range msg.ToolCalls {
			ev := extractFileEvent(tc)
			if ev != nil {
				events = append(events, *ev)
			}
		}
	}

	if len(events) == 0 {
		return CompactionPlan{
			Decisions: make(map[string]CompactionDecision),
			Tokens:    CompactionTokens{},
		}
	}

	// Group events by persisted canonical FileEntry path.
	pathGroups := make(map[string][]fileEvent)
	for _, ev := range events {
		pathGroups[ev.path] = append(pathGroups[ev.path], ev)
	}

	// For each path, walk events to find the latest checkpoint and compact earlier ones
	decisions := make(map[string]CompactionDecision)
	var tokens CompactionTokens

	for _, group := range pathGroups {
		// Find the latest checkpoint index (last full checkpoint in chronological order)
		latestCheckpointIdx := -1
		latestCheckpointID := ""
		for i := len(group) - 1; i >= 0; i-- {
			if group[i].isCheckpoint {
				latestCheckpointIdx = i
				latestCheckpointID = group[i].toolCallID
				break
			}
		}

		// If there's a checkpoint, compact all successful events before it
		if latestCheckpointIdx >= 0 {
			for i := 0; i < len(group); i++ {
				ev := group[i]
				addRaw(&tokens, ev)

				if i < latestCheckpointIdx {
					// Events before the latest checkpoint are superseded
					if ev.isSuccessful {
						// Compact successful events superseded by a later checkpoint
						decision := CompactionDecision{
							ToolCallID:       ev.toolCallID,
							Path:             ev.path,
							Trace:            ev.trace,
							CompactArguments: true,
							CompactResult:    true,
							SupersededBy:     latestCheckpointID,
						}
						decisions[ev.toolCallID] = decision
						tokens.SavedInstruction += ev.instructionTokens
						tokens.SavedExecution += ev.executionTokens
						tokens.Saved += ev.instructionTokens + ev.executionTokens
					} else {
						// Failed/incomplete events before checkpoint: retained as-is
						addRetained(&tokens, ev)
						decisions[ev.toolCallID] = CompactionDecision{
							ToolCallID:       ev.toolCallID,
							Path:             ev.path,
							Trace:            ev.trace,
							CompactArguments: false,
							CompactResult:    false,
						}
					}
				} else if i == latestCheckpointIdx {
					// The latest checkpoint itself is retained
					addRetained(&tokens, ev)
					decisions[ev.toolCallID] = CompactionDecision{
						ToolCallID:       ev.toolCallID,
						Path:             ev.path,
						Trace:            ev.trace,
						CompactArguments: false,
						CompactResult:    false,
					}
				} else {
					// Events after the latest checkpoint: retained
					addRetained(&tokens, ev)
					decisions[ev.toolCallID] = CompactionDecision{
						ToolCallID:       ev.toolCallID,
						Path:             ev.path,
						Trace:            ev.trace,
						CompactArguments: false,
						CompactResult:    false,
					}
				}
			}
		} else {
			// No checkpoint for this path: all events retained
			for _, ev := range group {
				addRaw(&tokens, ev)
				addRetained(&tokens, ev)
				decisions[ev.toolCallID] = CompactionDecision{
					ToolCallID:       ev.toolCallID,
					Path:             ev.path,
					Trace:            ev.trace,
					CompactArguments: false,
					CompactResult:    false,
				}
			}
		}
	}

	return CompactionPlan{
		Decisions: decisions,
		Tokens:    tokens,
	}
}

// extractFileEvent returns a file event if the tool call is a file operation.
// Returns nil for non-file tools.
func extractFileEvent(tc config.ToolCallEntry) *fileEvent {
	toolName := tc.Instruction.Name
	isFileTool := toolName == "read_file" || toolName == "write_file" || toolName == "edit_file"
	if !isFileTool {
		return nil
	}

	// Parse arguments to determine path and whether read is ranged
	var args map[string]interface{}
	if tc.Instruction.Arguments != "" {
		if err := json.Unmarshal([]byte(tc.Instruction.Arguments), &args); err != nil {
			// Malformed args — can't classify properly, retain as-is
			return &fileEvent{
				toolCallID:        tc.ID,
				toolName:          toolName,
				path:              "",
				trace:             "",
				isCheckpoint:      false,
				isSuccessful:      false,
				instructionTokens: tc.Instruction.Tokens,
				executionTokens:   tc.Execution.Tokens,
				argsJSON:          tc.Instruction.Arguments,
			}
		}
	}

	instructionPath, _ := args["path"].(string)
	path := instructionPath
	if len(tc.Execution.Files) > 0 && tc.Execution.Files[0].Path != "" {
		path = tc.Execution.Files[0].Path
	}

	_, _, isRangedRead, rangeErr := tools.ParseReadRange(args)
	if toolName != "read_file" || rangeErr != nil {
		isRangedRead = false
	}

	// Determine success status
	isSuccessful := tc.Execution.Status == tools.ResultStatusSuccess

	// Determine trace from FileEntry (persisted) or infer from tool name
	trace := ""
	if len(tc.Execution.Files) > 0 {
		trace = tc.Execution.Files[0].Trace
	} else if isSuccessful {
		switch toolName {
		case "read_file":
			trace = config.TraceRead
		case "write_file":
			trace = config.TraceWrite
		case "edit_file":
			trace = config.TraceEdit
		}
	}

	// Classify checkpoint:
	// - read_file without range => full checkpoint
	// - write_file trace write/create => full checkpoint
	// - edit_file => delta (never a checkpoint, including no-op read traces)
	// - ranged read => partial observation (never a checkpoint)
	isCheckpoint := false
	switch toolName {
	case "read_file":
		if !isRangedRead && isSuccessful {
			isCheckpoint = true
		}
	case "write_file":
		if isSuccessful && (trace == config.TraceWrite || trace == config.TraceCreate) {
			isCheckpoint = true
		}
	case "edit_file":
		// edit_file is always a delta, never a checkpoint
		// No-op edits emit trace=read — still not a checkpoint
	}

	return &fileEvent{
		toolCallID:        tc.ID,
		toolName:          toolName,
		path:              path,
		trace:             trace,
		isCheckpoint:      isCheckpoint,
		isRangedRead:      isRangedRead,
		isSuccessful:      isSuccessful,
		instructionTokens: tc.Instruction.Tokens,
		executionTokens:   tc.Execution.Tokens,
		argsJSON:          tc.Instruction.Arguments,
	}
}

func addRaw(t *CompactionTokens, ev fileEvent) {
	t.RawInstruction += ev.instructionTokens
	t.RawExecution += ev.executionTokens
	t.Raw += ev.instructionTokens + ev.executionTokens
}

func addRetained(t *CompactionTokens, ev fileEvent) {
	t.RetainedInstruction += ev.instructionTokens
	t.RetainedExecution += ev.executionTokens
	t.Retained += ev.instructionTokens + ev.executionTokens
}
