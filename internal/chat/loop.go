package chat

import (
	"context"
	"fmt"
	"strings"
	"time"

	"squid-os/internal/config"
)

type LoopAction int

const (
	LoopContinue LoopAction = iota
	LoopToolCalls
	LoopDone
	LoopError
)

type LoopResult struct {
	Action        LoopAction
	Error         error
	MsgIdx        int
	IsAuthError   bool
	Cancelled     bool
	SilentFailure bool
}

// ProcessStreamEvent mutates pure session stream/message state for one provider event.
// UI concerns (textarea, notifications, rendering, autosave, Bubble Tea mode) are handled by app wrappers.
func ProcessStreamEvent(s *Session, event StreamEvent) LoopResult {
	if event.Error != nil {
		if s.Stream.StreamCancelled {
			return LoopResult{Action: LoopDone, MsgIdx: -1, Cancelled: true}
		}
		return LoopResult{Action: LoopError, Error: event.Error, IsAuthError: event.IsAuthError}
	}

	if event.Done {
		if event.Text != "" {
			s.Stream.AddTextChunk(event.Text)
		}
		if event.Thinking != "" {
			s.Stream.AddThinkChunk(event.Thinking)
		}
		s.Stream.InThinking = event.InThinking

		if s.Stream.StreamCancelled {
			return LoopResult{Action: LoopDone, MsgIdx: -1, Cancelled: true}
		}

		hasContent := s.Stream.Text != "" || s.Stream.Thinking != "" || len(s.Stream.PartialTools) > 0
		if !hasContent && event.StopReason == "" {
			err := fmt.Errorf("stream ended unexpectedly — server may be overloaded")
			return LoopResult{Action: LoopError, Error: err, SilentFailure: true}
		}

		if event.StopReason == "tool_calls" && len(s.Stream.PartialTools) > 0 {
			s.Stream.Active = false
			return LoopResult{Action: LoopToolCalls}
		}

		idx := SaveAssistantMsg(s, config.Message{
			ID:                 nextMessageID(s),
			Role:               config.RoleAssistant,
			CreatedAt:          s.Stream.Metrics.Start,
			ThinkingText:       strings.TrimLeft(s.Stream.Thinking, "\n"),
			ThinkingMetrics:    config.ContentMetrics{Tokens: s.Stream.Metrics.ThinkingTokens(), InferenceDuractionMs: s.Stream.Metrics.ThinkingDuration().Milliseconds(), TimeToFirstTokenMs: s.Stream.Metrics.TimeToFirstThinkingToken().Milliseconds()},
			Text:               strings.TrimLeft(s.Stream.Text, "\n"),
			TextMetrics:        config.ContentMetrics{Tokens: s.Stream.Metrics.TextTokens(), InferenceDuractionMs: s.Stream.Metrics.TextDuration().Milliseconds(), TimeToFirstTokenMs: s.Stream.Metrics.TimeToFirstTextToken().Milliseconds()},
			TokensPerSecond:    s.Stream.Metrics.AvgTokenPerSec(),
			OutputTokens:       s.Stream.Metrics.TotalOutputTokens(),
			DurationTimeMs:     s.Stream.Metrics.Duration().Milliseconds(),
			TimeToFirstTokenMs: s.Stream.Metrics.TimeToFirstToken().Milliseconds(),
			StopReason:         event.StopReason,
		})
		return LoopResult{Action: LoopDone, MsgIdx: idx}
	}

	if event.ToolCallDelta != "" {
		s.Stream.AddToolCallChunk(event.ToolCallDelta)
		for len(s.Stream.PartialTools) <= event.ToolCallIdx {
			s.Stream.PartialTools = append(s.Stream.PartialTools, PartialTool{})
		}
		p := &s.Stream.PartialTools[event.ToolCallIdx]
		if event.ToolCallID != "" {
			p.ID = event.ToolCallID
		}
		if event.ToolCallName != "" {
			p.Name = event.ToolCallName
		}
		p.Args += event.ToolCallDelta
		p.Chars += len(event.ToolCallDelta)
		if p.FirstAt.IsZero() {
			p.FirstAt = time.Now()
		}
		p.DoneAt = time.Now()
		if s.Stream.InThinking {
			s.Stream.InThinking = false
		}
		return LoopResult{Action: LoopContinue}
	}

	if len(event.ToolCalls) > 0 {
		for offset, tc := range event.ToolCalls {
			idx := event.ToolCallIdx + offset
			for len(s.Stream.PartialTools) <= idx {
				s.Stream.PartialTools = append(s.Stream.PartialTools, PartialTool{})
			}
			p := &s.Stream.PartialTools[idx]
			p.ID = tc.ID
			p.Type = tc.Type
			p.Name = tc.Name
			p.Args = tc.ArgsJSON
			p.Chars = len(tc.ArgsJSON)
			if p.FirstAt.IsZero() {
				p.FirstAt = time.Now()
			}
			p.DoneAt = time.Now()
		}
	}
	if event.Text != "" {
		s.Stream.AddTextChunk(event.Text)
	}
	if event.Thinking != "" {
		s.Stream.AddThinkChunk(event.Thinking)
	}
	s.Stream.InThinking = event.InThinking
	return LoopResult{Action: LoopContinue}
}

func AppendStreamErrorMessage(s *Session, err error) int {
	msg := "Stream error"
	if err != nil {
		msg = "Stream error: " + err.Error()
	}
	return appendSyntheticMessage(s, msg, "stream error")
}

func AppendAuthErrorMessage(s *Session) int {
	return appendSyntheticMessage(s, "Authentication failed — use /model to re-authenticate", "auth error")
}

func AppendSilentFailureMessage(s *Session) int {
	return appendSyntheticMessage(s, "Stream ended unexpectedly — server may be overloaded (VRAM / model loading)", "stream error")
}

func AppendStreamCancelledMessage(s *Session) int {
	msg := s.Stream.CancelMessage
	if msg == "" {
		msg = "Stream cancelled"
	}
	return appendSyntheticMessage(s, msg, "aborted")
}

func appendSyntheticMessage(s *Session, text, label string) int {
	s.Append(config.Message{
		ID:          nextMessageID(s),
		Role:        config.RoleSynthetic,
		CreatedAt:   time.Now(),
		Text:        text,
		Label:       label,
		InputTokens: CountTokensApproxString(text),
	})
	return len(s.Doc.Messages) - 1
}

func nextMessageID(s *Session) string {
	return fmt.Sprintf("msg_%d", len(s.Doc.Messages)+1)
}

func SaveAssistantMsg(s *Session, msg config.Message) int {
	s.Append(msg)
	config.RecomputeSequenceStats(s.Doc.Messages)
	return len(s.Doc.Messages) - 1
}

func FlushToolMessage(s *Session, msgIdx int) {
	msg := &s.Doc.Messages[msgIdx]
	execDurMs := int64(0)
	for _, tc := range msg.ToolCalls {
		execDurMs += tc.Execution.DurationMs
	}
	msg.DurationTimeMs = s.Stream.Metrics.Duration().Milliseconds() + execDurMs
	msg.InputTokens = config.TotalExecutionTokens(msg.ToolCalls)
	config.RecomputeSequenceStats(s.Doc.Messages)
	s.RefreshTokenTally()
}

// StartStream builds API messages from session state and starts provider streaming.
func StartStream(s *Session, endpoints config.EndpointsConfig) <-chan StreamEvent {
	return StartStreamWithContext(context.Background(), s, endpoints)
}

// StartStreamWithContext builds API messages from session state and starts provider streaming using ctx.
func StartStreamWithContext(ctx context.Context, s *Session, endpoints config.EndpointsConfig) <-chan StreamEvent {
	s.Stream.Begin()
	inf := s.CurrentInference()
	providerSettings := config.ResolveProviderSettings(endpoints, inf.Provider)
	engine := NewEngine(providerSettings, inf.Model, inf.Thinking)
	streamCtx, cancel := context.WithCancel(ctx)
	s.Stream.CancelFn = func(msg string) {
		s.Stream.MarkCancelled(msg)
		cancel()
	}
	reqCtx := s.BuildContext()
	return engine.Stream(streamCtx, reqCtx.Messages, s.GetTools())
}

type LoopEventType int

const (
	LoopEventText LoopEventType = iota
	LoopEventThinking
	LoopEventToolDelta
	LoopEventAssistantSaved
	LoopEventToolFlushed
	LoopEventNeedAuth
	LoopEventDone
	LoopEventError
)

type LoopEvent struct {
	Type        LoopEventType
	Text        string
	Thinking    string
	Error       error
	MsgIdx      int
	ToolIndex   int
	AuthRequest *AuthRequest
	IsAuthError bool
	Cancelled   bool
}

func RunLoop(ctx context.Context, s *Session, paths config.Paths, endpoints config.EndpointsConfig) <-chan LoopEvent {
	out := make(chan LoopEvent, 64)
	go func() {
		defer close(out)
		steps, toolCount := 0, 0
		for {
			steps++
			if s.Doc.Config.Limits.MaxSteps > 0 && steps > s.Doc.Config.Limits.MaxSteps {
				out <- LoopEvent{Type: LoopEventError, Error: fmt.Errorf("maximum steps exceeded")}
				return
			}
			streamCh := StartStreamWithContext(ctx, s, endpoints)
			restart := false
			for event := range streamCh {
				if event.Text != "" {
					out <- LoopEvent{Type: LoopEventText, Text: event.Text}
				}
				if event.Thinking != "" {
					out <- LoopEvent{Type: LoopEventThinking, Thinking: event.Thinking}
				}
				if event.ToolCallDelta != "" {
					out <- LoopEvent{Type: LoopEventToolDelta, Text: event.ToolCallDelta, ToolIndex: event.ToolCallIdx}
				}

				res := ProcessStreamEvent(s, event)
				switch res.Action {
				case LoopContinue:
					continue
				case LoopError:
					if res.IsAuthError {
						_ = config.PersistProviderAuthState(paths, endpoints, s.CurrentInference().Provider)
						res.MsgIdx = AppendAuthErrorMessage(s)
					} else if res.SilentFailure {
						_ = config.PersistRefreshedProvider(paths, endpoints, s.CurrentInference().Provider)
						res.MsgIdx = AppendSilentFailureMessage(s)
					} else {
						_ = config.PersistRefreshedProvider(paths, endpoints, s.CurrentInference().Provider)
						res.MsgIdx = AppendStreamErrorMessage(s, res.Error)
					}
					out <- LoopEvent{Type: LoopEventError, Error: res.Error, MsgIdx: res.MsgIdx, IsAuthError: res.IsAuthError}
					return
				case LoopDone:
					_ = config.PersistRefreshedProvider(paths, endpoints, s.CurrentInference().Provider)
					if res.Cancelled {
						res.MsgIdx = AppendStreamCancelledMessage(s)
					}
					out <- LoopEvent{Type: LoopEventAssistantSaved, MsgIdx: res.MsgIdx}
					out <- LoopEvent{Type: LoopEventDone, MsgIdx: res.MsgIdx, Cancelled: res.Cancelled}
					return
				case LoopToolCalls:
					msgIdx := -1
					for {
						if s.Doc.Config.Limits.MaxTools > 0 && toolCount >= s.Doc.Config.Limits.MaxTools {
							out <- LoopEvent{Type: LoopEventError, Error: fmt.Errorf("maximum tools exceeded")}
							return
						}
						toolRes := ExecuteTools(s, ToolExecOptions{MsgIdx: msgIdx})
						toolCount++
						out <- LoopEvent{Type: LoopEventToolFlushed, MsgIdx: toolRes.MsgIdx, ToolIndex: toolRes.ToolIndex}
						switch toolRes.Action {
						case ToolExecNeedAuth:
							out <- LoopEvent{Type: LoopEventNeedAuth, MsgIdx: toolRes.MsgIdx, ToolIndex: toolRes.ToolIndex, AuthRequest: toolRes.AuthRequest}
							return
						case ToolExecContinue:
							msgIdx = toolRes.MsgIdx
							continue
						case ToolExecDone:
							if toolRes.CapturedUserText != "" {
								s.Append(NewUserMessage(nextMessageID(s), toolRes.CapturedUserText, ""))
							}
							s.Stream.Reset()
							restart = true
						}
						break
					}
				}
				if restart {
					break
				}
			}
			if !restart {
				return
			}
		}
	}()
	return out
}
