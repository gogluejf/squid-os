package chat

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"squid-os/internal/config"
	"squid-os/internal/tools"
)

type ToolExecAction int

const (
	ToolExecContinue ToolExecAction = iota
	ToolExecNeedAuth
	ToolExecDone
)

type AuthRequest struct {
	ToolName      string
	Args          map[string]interface{}
	ArgsJSON      string
	DisplayValue  string
	IsDestructive bool
	ToolIndex     int
	MsgIdx        int
}

type AuthDecision struct {
	Approved     bool
	Instructions string
}

type ToolExecOptions struct {
	AuthorizationMode   string
	MaxToolResultTokens int
	Decision            *AuthDecision
	MsgIdx              int
}

type ToolExecResult struct {
	Action           ToolExecAction
	MsgIdx           int
	ToolIndex        int
	NextIndex        int
	AuthRequest      *AuthRequest
	CapturedUserText string
	WorkingDir       string
	LoadedSkill      string
}

func BuildInstructionEntry(p PartialTool) config.ToolCallEntry {
	dur := p.DoneAt.Sub(p.FirstAt).Milliseconds()
	return config.ToolCallEntry{
		ID:   p.ID,
		Type: p.Type,
		Instruction: struct {
			Name       string `json:"name"`
			Arguments  string `json:"arguments"`
			Tokens     int    `json:"tokens,omitempty"`
			DurationMs int64  `json:"duration_ms,omitempty"`
		}{Name: p.Name, Arguments: p.Args, Tokens: CountTokensApproxString(p.Args), DurationMs: dur},
	}
}

func ExecuteTools(s *Session, toolReg *tools.Registry, opts ToolExecOptions) ToolExecResult {
	if s.Doc.FileState == nil {
		s.Doc.FileState = make(map[string]config.FileStateEntry)
	}
	sessionState := s.Doc.FileState

	msgIdx := opts.MsgIdx
	if msgIdx < 0 {
		entries := make([]config.ToolCallEntry, len(s.Stream.PartialTools))
		for i, p := range s.Stream.PartialTools {
			entries[i] = BuildInstructionEntry(p)
			entries[i].Execution.Status = tools.ResultStatusPending
		}
		msgIdx = SaveAssistantMsg(s, config.Message{
			ID:                 fmt.Sprintf("msg_%d", len(s.Doc.Messages)+1),
			Role:               config.RoleAssistant,
			CreatedAt:          s.Stream.Metrics.Start,
			ThinkingText:       strings.TrimLeft(s.Stream.Thinking, "\n"),
			ThinkingMetrics:    config.ContentMetrics{Tokens: s.Stream.Metrics.ThinkingTokens(), InferenceDuractionMs: s.Stream.Metrics.ThinkingDuration().Milliseconds(), TimeToFirstTokenMs: s.Stream.Metrics.TimeToFirstThinkingToken().Milliseconds()},
			Text:               strings.TrimLeft(s.Stream.Text, "\n"),
			TextMetrics:        config.ContentMetrics{Tokens: s.Stream.Metrics.TextTokens(), InferenceDuractionMs: s.Stream.Metrics.TextDuration().Milliseconds(), TimeToFirstTokenMs: s.Stream.Metrics.TimeToFirstTextToken().Milliseconds()},
			ToolCalls:          entries,
			ToolCallMetrics:    config.ContentMetrics{Tokens: s.Stream.Metrics.ToolCallTokens(), InferenceDuractionMs: s.Stream.Metrics.ToolCallDuration().Milliseconds(), TimeToFirstTokenMs: s.Stream.Metrics.TimeToFirstToolCallToken().Milliseconds()},
			TokensPerSecond:    s.Stream.Metrics.AvgTokenPerSec(),
			OutputTokens:       s.Stream.Metrics.TotalOutputTokens(),
			DurationTimeMs:     s.Stream.Metrics.Duration().Milliseconds(),
			TimeToFirstTokenMs: s.Stream.Metrics.TimeToFirstToken().Milliseconds(),
			StopReason:         "tool_calls",
		})
	}

	entries := s.Doc.Messages[msgIdx].ToolCalls
	startIndex := nextExecutableToolIndex(entries)
	if startIndex >= len(entries) {
		FlushToolMessage(s, msgIdx)
		return ToolExecResult{Action: ToolExecDone, MsgIdx: msgIdx, ToolIndex: len(entries) - 1, NextIndex: len(entries)}
	}

	i := startIndex
	entry := &entries[i]
	toolName := entry.Instruction.Name
	argsJSON := entry.Instruction.Arguments

	tool := toolReg.Get(toolName)
	if tool == nil {
		entry.Execution.Status = tools.ResultStatusError
		entry.Execution.Error = fmt.Sprintf("unknown tool: %s", toolName)
		FlushToolMessage(s, msgIdx)
		return ToolExecResult{Action: nextToolAction(i, len(entries)), MsgIdx: msgIdx, ToolIndex: i, NextIndex: i + 1}
	}

	args, argsJSON, malformedErr := parseToolArgs(argsJSON)
	if malformedErr != nil {
		entries[i].Execution.Status = tools.ResultStatusError
		entries[i].Execution.Error = fmt.Sprintf("malformed tool arguments from model: %v", malformedErr)
		for j := i + 1; j < len(entries); j++ {
			entries[j].Execution.Status = tools.ResultStatusError
			entries[j].Execution.Error = "cancelled: prior tool had malformed arguments"
		}
		FlushToolMessage(s, msgIdx)
		return ToolExecResult{Action: ToolExecDone, MsgIdx: msgIdx, ToolIndex: i, NextIndex: len(entries)}
	}

	var capturedInstructions string
	if opts.Decision != nil {
		capturedInstructions = opts.Decision.Instructions
		if !opts.Decision.Approved {
			entries[i].Execution.Status = tools.ResultStatusError
			entries[i].Execution.Error = "rejected by user — tool was not executed, don't retry."
			for j := i + 1; j < len(entries); j++ {
				entries[j].Execution.Status = tools.ResultStatusError
				entries[j].Execution.Error = "cancelled: previous tool was not approved"
			}
			FlushToolMessage(s, msgIdx)
			return ToolExecResult{Action: ToolExecDone, MsgIdx: msgIdx, ToolIndex: i, NextIndex: len(entries)}
		}
		goto doExecute
	}

	if toolName != "read_file" && toolName != "open" {
		if pathVal, ok := args["path"].(string); ok {
			resolvedPath := tools.ResolvePath(pathVal)
			if err := tools.Validate(resolvedPath, sessionState); err != nil {
				entries[i].Execution.Status = tools.ResultStatusError
				entries[i].Execution.Error = fmt.Sprintf("blocked: file changed externally: %s — tool was not executed. Read the file again uisng the tool \"read_file\" and retry your command.", resolvedPath)
				for j := i + 1; j < len(entries); j++ {
					entries[j].Execution.Status = tools.ResultStatusError
					entries[j].Execution.Error = "cancelled: prior tool failed due to file change, remaining tools skipped"
				}
				FlushToolMessage(s, msgIdx)
				return ToolExecResult{Action: ToolExecDone, MsgIdx: msgIdx, ToolIndex: i, NextIndex: len(entries)}
			}
		}
	}

	if shouldAuthorize(opts.AuthorizationMode, tool, args) {
		entries[i].Execution.Error = ""
		if tool.Preview != nil {
			preview := tool.Preview(args)
			if preview.Status == tools.ResultStatusError {
				entries[i].Execution.Status = tools.ResultStatusError
				entries[i].Execution.Error = preview.Error
				FlushToolMessage(s, msgIdx)
				return ToolExecResult{Action: nextToolAction(i, len(entries)), MsgIdx: msgIdx, ToolIndex: i, NextIndex: i + 1}
			}
			for j := range preview.Files {
				preview.Files[j].ToolCallID = entry.ID
			}
			entries[i].Execution.Status = tools.ResultStatusPending
			entries[i].Execution.Result = preview.Result
			entries[i].Execution.Files = preview.Files
		} else {
			entries[i].Execution.Status = tools.ResultStatusPending
		}
		for j := i + 1; j < len(entries); j++ {
			entries[j].Execution.Status = tools.ResultStatusPending
			entries[j].Execution.Error = "waiting: prior tool requires authorization"
		}
		isDestructive := false
		if tool.IsDestructive != nil {
			isDestructive = tool.IsDestructive(args)
		}
		FlushToolMessage(s, msgIdx)
		return ToolExecResult{
			Action:    ToolExecNeedAuth,
			MsgIdx:    msgIdx,
			ToolIndex: i,
			NextIndex: i,
			AuthRequest: &AuthRequest{
				ToolName:      toolName,
				Args:          args,
				ArgsJSON:      argsJSON,
				DisplayValue:  tool.DisplayValue(argsJSON),
				IsDestructive: isDestructive,
				ToolIndex:     i,
				MsgIdx:        msgIdx,
			},
		}
	}

doExecute:
	entries[i].Execution.Status = ""
	entries[i].Execution.Result = ""
	entries[i].Execution.Error = ""
	entries[i].Execution.Files = nil

	maxToolResultTokens := opts.MaxToolResultTokens
	if maxToolResultTokens <= 0 {
		maxToolResultTokens = s.Doc.Session.MaxToolResultTokens
	}

	resultStart := time.Now()
	result := tool.Execute(args)
	result = enforceToolResultTokenLimit(result, maxToolResultTokens)
	entries[i].Execution.Status = result.Status
	entries[i].Execution.Result = result.Result
	entries[i].Execution.Error = result.Error

	content := result.Result
	if result.Status == tools.ResultStatusError {
		content = result.Error
	}
	entries[i].Execution.Tokens = CountTokensApproxString(content)
	entries[i].Execution.DurationMs = time.Since(resultStart).Milliseconds()
	for j := range result.Files {
		result.Files[j].ToolCallID = entry.ID
	}
	entries[i].Execution.Files = result.Files
	tools.MergeEntries(result.Files, sessionState)

	res := ToolExecResult{Action: nextToolAction(i, len(entries)), MsgIdx: msgIdx, ToolIndex: i, NextIndex: i + 1}
	if toolName == "set_working_dir" && result.Status == tools.ResultStatusSuccess {
		if pathVal, ok := args["path"].(string); ok {
			res.WorkingDir = pathVal
			s.Doc.Session.WorkingDir = pathVal
		}
	}
	if toolName == "skill_load" && result.Status == tools.ResultStatusSuccess {
		if name, ok := args["name"].(string); ok {
			res.LoadedSkill = name
			s.SetCurrentSkill(name)
		}
	}

	if capturedInstructions != "" {
		for j := i + 1; j < len(entries); j++ {
			entries[j].Execution.Status = tools.ResultStatusError
			entries[j].Execution.Error = "cancelled: user provided instructions before this tool could execute"
		}
		res.Action = ToolExecDone
		res.NextIndex = len(entries)
		res.CapturedUserText = capturedInstructions
	}

	FlushToolMessage(s, msgIdx)
	return res
}

func parseToolArgs(argsJSON string) (map[string]interface{}, string, error) {
	args := make(map[string]interface{})
	if argsJSON == "" {
		return args, argsJSON, nil
	}
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		repairedJSON, repaired := RepairArgs(argsJSON)
		if repaired {
			if err2 := json.Unmarshal([]byte(repairedJSON), &args); err2 != nil {
				return nil, repairedJSON, err2
			}
			return args, repairedJSON, nil
		}
		return nil, repairedJSON, err
	}
	return args, argsJSON, nil
}

func nextToolAction(i, total int) ToolExecAction {
	if i+1 >= total {
		return ToolExecDone
	}
	return ToolExecContinue
}

func nextExecutableToolIndex(entries []config.ToolCallEntry) int {
	for i, entry := range entries {
		switch entry.Execution.Status {
		case tools.ResultStatusSuccess, tools.ResultStatusError:
			continue
		default:
			return i
		}
	}
	return len(entries)
}

func enforceToolResultTokenLimit(result tools.ToolResult, maxTokens int) tools.ToolResult {
	if maxTokens <= 0 {
		maxTokens = 15000
	}

	content := result.Result
	if result.Status == tools.ResultStatusError {
		content = result.Error
	}

	tokens := CountTokensApproxString(content)
	if tokens <= maxTokens {
		return result
	}

	msg := fmt.Sprintf("Tool result too large: estimated %d tokens exceeds max_tool_result_tokens=%d. Refine the command or redirect output to a file.", tokens, maxTokens)
	return tools.ToolResult{
		Status: tools.ResultStatusError,
		Error:  msg,
		Files:  result.Files,
	}
}

func shouldAuthorize(mode string, tool *tools.Tool, args map[string]interface{}) bool {
	switch mode {
	case config.AuthorizationAskForAll:
		return true
	case config.AuthorizationAskOnWrite:
		return tool != nil && tool.IsDestructive != nil && tool.IsDestructive(args)
	default:
		return false
	}
}
