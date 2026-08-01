package config

// SequenceStat aggregates token, timing, and execution metrics for a message
// sequence (assistant reply + its tool calls).
type SequenceStat struct {
	AvgTokensPerSec      float64 `json:"avg_tok_per_sec,omitempty"`
	OutputTokens         int     `json:"output_tokens,omitempty"`
	DurationMs           int64   `json:"duration_ms,omitempty"`
	InferenceDuractionMs int64   `json:"inference_duration_ms,omitempty"`
	InputTokens          int     `json:"input_tokens,omitempty"`
	ExecDurMs            int64   `json:"exec_dur_ms,omitempty"`
}

// Add sums another SequenceStat into this one and recomputes AvgTokensPerSec.
func (ss *SequenceStat) Add(other *SequenceStat) {
	ss.OutputTokens += other.OutputTokens
	ss.DurationMs += other.DurationMs
	ss.InferenceDuractionMs += other.InferenceDuractionMs
	ss.ExecDurMs += other.ExecDurMs
	ss.InputTokens += other.InputTokens
	ss.recomputeAvg()
}

// Accumulate adds a single message's metrics into this stat.
func (ss *SequenceStat) Accumulate(msg Message) {
	ss.OutputTokens += msg.OutputTokens
	ss.DurationMs += msg.DurationTimeMs
	ss.InferenceDuractionMs += msg.TextMetrics.InferenceDuractionMs
	ss.InferenceDuractionMs += msg.ThinkingMetrics.InferenceDuractionMs
	ss.InferenceDuractionMs += msg.ToolCallMetrics.InferenceDuractionMs
	ss.InputTokens += msg.InputTokens
	for _, tc := range msg.ToolCalls {
		ss.ExecDurMs += tc.Execution.DurationMs
	}
	ss.recomputeAvg()
}

func (ss *SequenceStat) recomputeAvg() {
	if ss.InferenceDuractionMs > 0 {
		ss.AvgTokensPerSec = float64(ss.OutputTokens) / float64(ss.InferenceDuractionMs) * 1000.0
	}
}

// FindSequenceHeadIdx returns the index of the first assistant message after
// the last user message, skipping any "synthetic" messages in between,
// or -1 if none exists yet.
func FindSequenceHeadIdx(msgs []Message) int {
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == RoleUser {
			for j := i + 1; j < len(msgs); j++ {
				if msgs[j].Role == RoleAssistant {
					return j
				}
			}
			return -1
		}
	}
	return -1
}

// RecomputeSequenceStats walks the sequence head onward and accumulates
// token, timing, and execution metrics into the head's SequenceStat.
func RecomputeSequenceStats(messages []Message) {
	seqIdx := FindSequenceHeadIdx(messages)
	if seqIdx == -1 {
		return
	}
	head := messages[seqIdx]
	stat := &SequenceStat{}
	for i := seqIdx; i < len(messages); i++ {
		stat.Accumulate(messages[i])
	}
	head.SequenceStat = stat
	messages[seqIdx] = head
}
