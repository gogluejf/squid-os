package chat

import (
	"squid-os/internal/config"
)

// RefreshTokenTally stores the current full token tally on the session doc.
func (s *Session) RefreshTokenTally() {
	s.Doc.TokenTally = s.CalculateTokenTally()
}

// CalculateTokenTally computes the structured token tally without mutating the session.
func (s *Session) CalculateTokenTally() *config.TokenTally {
	lifetime := s.calculateLifetimeTally()
	contextTally := s.calculateContextTally()

	return &config.TokenTally{
		Lifetime: lifetime,
		Context:  contextTally,
	}
}

func (s *Session) calculateLifetimeTally() config.LifetimeTokenTally {
	var input config.InputTokenTally
	var output config.OutputTokenTally

	for _, msg := range s.Doc.Messages {
		switch msg.Role {
		case config.RoleUser:
			input.User += msg.InputTokens
			for _, ref := range msg.Attachments {
				input.Attachment += ref.Tokens
			}
		case config.RoleSystem:
			input.SystemPrompt += msg.InputTokens
		case config.RoleInternal:
			// Internal messages declare wire cost via InputTokens only when
			// their content is sent to the model outside the conversation
			// (today: tools0 tool schemas). All other internal metadata is 0.
			input.ToolDefinitions += msg.InputTokens
		case config.RoleSynthetic:
			input.Synthetic += msg.InputTokens
		case config.RoleAssistant:
			// Input tokens on assistant messages come from tool execution results
			input.ToolExecution += msg.InputTokens

			// Media produced by tools (e.g. inspect_media) is tracked separately
			// so lifetime accounting includes the synthetic multimodal cost.
			for _, tc := range msg.ToolCalls {
				for _, ref := range tc.Execution.Attachments {
					input.ToolAttachment += ref.Tokens
				}
			}

			// Output tokens
			output.Thinking += msg.ThinkingMetrics.Tokens
			output.ToolCalls += msg.ToolCallMetrics.Tokens
			output.Assistant += msg.TextMetrics.Tokens
		}
	}

	input.Total = input.User + input.Attachment + input.ToolAttachment + input.ToolExecution + input.SystemPrompt + input.ToolDefinitions + input.Synthetic
	output.Total = output.Assistant + output.Thinking + output.ToolCalls

	total := input.Total + output.Total

	return config.LifetimeTokenTally{
		Input:  input,
		Output: output,
		Total:  total,
	}
}

func (s *Session) calculateContextTally() config.ContextTokenTally {
	caps := s.ModelCaps()
	return BuildContext(s.Doc.Messages, s.Doc.Config.ContextCompaction, s.mediaBaseDir(), s.Doc.Attachments, &caps).TokenTally()
}
