package app

import (
	"time"

	"squid-os/internal/log"
)

// StreamMetrics owns all timing and token-count metrics for an active inference stream.
// Text/thinking accumulation lives in streamState; this type owns the derived metrics.
type StreamMetrics struct {
	Start                time.Time
	firstThinkingTokenAt time.Time
	thinkingDoneAt       time.Time
	firstTextTokenAt     time.Time
	textDoneAt           time.Time
	thinkingChars        int // private — updated by streamState
	textChars            int
	firstToolCallTokenAt time.Time
	toolCallDoneAt       time.Time
	toolCallChars        int
}

/* ================== THINKING TOKENS ================ */

// addThinkChars adds character count to thinkingChars and records firstThinkingTokenAt on first call.
func (m *StreamMetrics) addThinkChars(s string) {
	n := len(s)
	if m.thinkingChars == 0 && n > 0 {
		m.firstThinkingTokenAt = time.Now()
	}
	m.thinkingChars += n
	m.thinkingDoneAt = time.Now()
	log.LogStreamMetrics("addThinkChars", s, n, m.thinkingChars, m.firstThinkingTokenAt, m.thinkingDoneAt)
}

// ThinkingDuration returns the duration from the first thinking token to when thinking ended
// (or now if thinking is still active).
func (m StreamMetrics) ThinkingDuration() time.Duration {
	if m.firstThinkingTokenAt.IsZero() {
		return 0
	}
	end := m.thinkingDoneAt
	if end.IsZero() {
		end = time.Now()
	}
	return end.Sub(m.firstThinkingTokenAt)
}

// ThinkingTokens returns the approximate token count for thinking characters.
func (m StreamMetrics) ThinkingTokens() int {
	return countTokensApproxInt(m.thinkingChars)
}

// TimeToFirstThinkingToken returns the time from stream start to the first thinking token.
func (m StreamMetrics) TimeToFirstThinkingToken() time.Duration {
	if m.firstThinkingTokenAt.IsZero() {
		return 0
	}
	return m.firstThinkingTokenAt.Sub(m.Start)
}

/* ================== TEXT TOKENS ================ */

// addTextChars adds character count to textChars and records firstTextTokenAt on first call.
func (m *StreamMetrics) addTextChars(s string) {
	n := len(s)
	if m.textChars == 0 && n > 0 {
		m.firstTextTokenAt = time.Now()
	}
	m.textChars += n
	m.textDoneAt = time.Now()
	log.LogStreamMetrics("addTextChars", s, n, m.textChars, m.firstTextTokenAt, m.textDoneAt)
}

// TextTokens returns the approximate token count for text characters.
func (m StreamMetrics) TextTokens() int {
	return countTokensApproxInt(m.textChars)
}

// TimeToFirstTextToken returns the time from stream start to the first text token.
func (m StreamMetrics) TimeToFirstTextToken() time.Duration {
	if m.firstTextTokenAt.IsZero() {
		return 0
	}
	return m.firstTextTokenAt.Sub(m.Start)
}

// TextDuration returns the duration from the first text token to when text ended
// (or now if text is still active).
func (m StreamMetrics) TextDuration() time.Duration {
	if m.firstTextTokenAt.IsZero() {
		return 0
	}
	end := m.textDoneAt
	if end.IsZero() {
		end = time.Now()
	}
	return end.Sub(m.firstTextTokenAt)
}

/* ================== TOOL TOKENS ================ */

// addToolCallChars adds character count to toolCallChars and records firstToolCallTokenAt on first call.
func (m *StreamMetrics) addToolCallChars(s string) {
	n := len(s)
	if m.toolCallChars == 0 && n > 0 {
		m.firstToolCallTokenAt = time.Now()
	}
	m.toolCallChars += n
	m.toolCallDoneAt = time.Now()
	log.LogStreamMetrics("addToolCallChars", s, n, m.toolCallChars, m.firstToolCallTokenAt, m.toolCallDoneAt)
}

// ToolCallTokens returns the approximate token count for tool call argument characters.
func (m StreamMetrics) ToolCallTokens() int {
	return countTokensApproxInt(m.toolCallChars)
}

// TimeToFirstToolCallToken returns the time from stream start to the first tool call delta.
func (m StreamMetrics) TimeToFirstToolCallToken() time.Duration {
	if m.firstToolCallTokenAt.IsZero() {
		return 0
	}
	return m.firstToolCallTokenAt.Sub(m.Start)
}

// ToolCallDuration returns the duration from the first tool call delta to when it was done.
func (m StreamMetrics) ToolCallDuration() time.Duration {
	if m.firstToolCallTokenAt.IsZero() {
		return 0
	}
	end := m.toolCallDoneAt
	if end.IsZero() {
		end = time.Now()
	}
	return end.Sub(m.firstToolCallTokenAt)
}

// firstTokenAt returns the earliest timestamp at which any token arrived.
func (m StreamMetrics) firstTokenAt() time.Time {
	earliest := m.firstThinkingTokenAt
	if !m.firstTextTokenAt.IsZero() && (earliest.IsZero() || m.firstTextTokenAt.Before(earliest)) {
		earliest = m.firstTextTokenAt
	}
	if !m.firstToolCallTokenAt.IsZero() && (earliest.IsZero() || m.firstToolCallTokenAt.Before(earliest)) {
		earliest = m.firstToolCallTokenAt
	}
	return earliest
}

/* ================== TOTAL TOKENS ================== */

// HasFirstToken returns true if at least one token (text or thinking) has arrived.
func (m StreamMetrics) HasFirstToken() bool {
	return !m.firstThinkingTokenAt.IsZero() || !m.firstTextTokenAt.IsZero() || !m.firstToolCallTokenAt.IsZero()
}

// TotalOutputTokens returns thinking + text tokens (model output during streaming).
func (m StreamMetrics) TotalOutputTokens() int {
	return countTokensApproxInt(m.thinkingChars + m.textChars + m.toolCallChars)
}

// Duration returns the total elapsed time since the stream started.
func (m StreamMetrics) Duration() time.Duration {
	return time.Since(m.Start)
}

// TimeToFirstToken returns the earliest time from stream start to any first token.
func (m StreamMetrics) TimeToFirstToken() time.Duration {
	t := m.firstTokenAt()
	if t.IsZero() {
		return 0
	}
	return t.Sub(m.Start)
}

// AvgTokenPerSec returns the average tokens per second since the first token arrived.
func (m StreamMetrics) AvgTokenPerSec() float64 {
	t := m.firstTokenAt()
	if t.IsZero() {
		return 0
	}
	elapsed := time.Since(t).Seconds()
	if elapsed <= 0 {
		return 0
	}
	return float64(m.TotalOutputTokens()) / elapsed
}

// countTokensApproxInt estimates token count from a character count.
func countTokensApproxInt(chars int) int {
	n := chars / 4
	if n == 0 && chars > 0 {
		n = 1
	}
	return n
}
