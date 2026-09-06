package chat

import (
	"context"
	"fmt"
)

// ToolEventType identifies a single tool-execution event emitted by StartToolExec.
type ToolEventType int

const (
	ToolEventRunning ToolEventType = iota
	ToolEventFinished
	ToolEventNeedAuth
	ToolEventDone
	ToolEventError
	ToolEventCancelled
)

// ToolEvent is one step of the tool-execution loop. The worker goroutine owns
// all session mutation; consumers only read these immutable snapshots and
// never call ExecuteTools concurrently with the worker.
type ToolEvent struct {
	Type             ToolEventType
	Error            error
	CancelMessage    string
	MsgIdx           int
	ToolIndex        int
	NextIndex        int
	AuthRequest      *AuthRequest
	CapturedUserText string
	LoadedSkill      string
}

// StartToolExec runs the ExecuteTools loop in a background goroutine so slow
// tools (bash, agents, network) never block the caller — in the TUI this keeps
// the Bubble Tea event loop alive while a command runs.
//
// The loop mirrors RunLoop's inline tool handling: it executes pending tools,
// pauses on ToolEventNeedAuth (the consumer must restart via ResumeToolExec
// with an AuthDecision), appends captured user text, and finishes with
// ToolEventDone or ToolEventError.
func StartToolExec(ctx context.Context, s *Session, opts ToolExecOptions) <-chan ToolEvent {
	out := make(chan ToolEvent, 64)
	go func() {
		defer close(out)
		msgIdx := opts.MsgIdx
		decision := opts.Decision
		for {
			// Cooperative cancellation: checked between tools. A cancel that
			// lands mid-tool still kills the child via its own exec context
			// (bash timeout / process group); this stops the loop from starting
			// the next tool after that.
			if ctx.Err() != nil {
				s.Stream.MarkCancelled("tool execution aborted by user")
				out <- ToolEvent{Type: ToolEventCancelled, CancelMessage: "tool execution aborted", MsgIdx: msgIdx}
				return
			}
			res := ExecuteTools(s, ToolExecOptions{
				Decision:   decision,
				MsgIdx:     msgIdx,
				Checkpoint: opts.Checkpoint,
			})
			decision = nil
			if res.Error != nil {
				out <- ToolEvent{Type: ToolEventError, Error: res.Error, MsgIdx: res.MsgIdx, ToolIndex: res.ToolIndex}
				return
			}
			switch res.Action {
			case ToolExecNeedAuth:
				out <- ToolEvent{Type: ToolEventNeedAuth, MsgIdx: res.MsgIdx, ToolIndex: res.ToolIndex, AuthRequest: res.AuthRequest}
				return
			case ToolExecContinue:
				out <- ToolEvent{Type: ToolEventRunning, MsgIdx: res.MsgIdx, ToolIndex: res.ToolIndex, NextIndex: res.NextIndex, LoadedSkill: res.LoadedSkill}
				msgIdx = res.MsgIdx
			case ToolExecDone:
				if res.CapturedUserText != "" {
					s.Append(NewUserMessage(nextMessageID(s), res.CapturedUserText))
				}
				s.Stream.Reset()
				out <- ToolEvent{Type: ToolEventDone, MsgIdx: res.MsgIdx, ToolIndex: res.ToolIndex, NextIndex: res.NextIndex, CapturedUserText: res.CapturedUserText, LoadedSkill: res.LoadedSkill}
				return
			default:
				out <- ToolEvent{Type: ToolEventError, Error: fmt.Errorf("unexpected tool action %d", res.Action)}
				return
			}
		}
	}()
	return out
}

// ResumeToolExec continues a paused tool-execution loop after the consumer has
// collected an authorization decision for the pending ToolEventNeedAuth.
func ResumeToolExec(ctx context.Context, s *Session, opts ToolExecOptions, decision *AuthDecision) <-chan ToolEvent {
	opts.Decision = decision
	return StartToolExec(ctx, s, opts)
}
