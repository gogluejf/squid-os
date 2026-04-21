package app

import (
	"time"
)

// StreamMetrics owns all timing and token-count metrics for an active inference stream.
// Text/thinking accumulation lives in streamState; this type owns the derived metrics.
type StreamMetrics struct {
	Start                time.Time
	firstThinkingTokenAt time.Time
	thinkingDoneAt       time.Time
	firstTextTokenAt     time.Time
	thinkingChars        int // private — updated by streamState
	textChars            int
}

// MarkThinkingDone records the time when thinking mode ends.
func (m *StreamMetrics) MarkThinkingDone() {
	m.thinkingDoneAt = time.Now()
}

// HasFirstToken returns true if at least one token (text or thinking) has arrived.
func (m StreamMetrics) HasFirstToken() bool {
	return !m.firstThinkingTokenAt.IsZero() || !m.firstTextTokenAt.IsZero()
}

// TextTokens returns the approximate token count for text characters.
func (m StreamMetrics) TextTokens() int {
	return countTokensApproxInt(m.textChars)
}

// ThinkingTokens returns the approximate token count for thinking characters.
func (m StreamMetrics) ThinkingTokens() int {
	return countTokensApproxInt(m.thinkingChars)
}

// TotalTokens returns the combined approximate token count.
func (m StreamMetrics) TotalTokens() int {
	return countTokensApproxInt(m.thinkingChars + m.textChars)
}

// Duration returns the total elapsed time since the stream started.
func (m StreamMetrics) Duration() time.Duration {
	return time.Since(m.Start)
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

// TextDuration returns the duration from the first text token to now.
func (m StreamMetrics) TextDuration() time.Duration {
	if m.firstTextTokenAt.IsZero() {
		return 0
	}
	return time.Since(m.firstTextTokenAt)
}

// firstTokenAt returns the earliest timestamp at which any token arrived.
func (m StreamMetrics) firstTokenAt() time.Time {
	switch {
	case m.firstThinkingTokenAt.IsZero():
		return m.firstTextTokenAt
	case m.firstTextTokenAt.IsZero():
		return m.firstThinkingTokenAt
	case m.firstThinkingTokenAt.Before(m.firstTextTokenAt):
		return m.firstThinkingTokenAt
	default:
		return m.firstTextTokenAt
	}
}

// TimeToFirstToken returns the earliest time from stream start to any first token.
func (m StreamMetrics) TimeToFirstToken() time.Duration {
	t := m.firstTokenAt()
	if t.IsZero() {
		return 0
	}
	return t.Sub(m.Start)
}

// TimeToFirstTextToken returns the time from stream start to the first text token.
func (m StreamMetrics) TimeToFirstTextToken() time.Duration {
	if m.firstTextTokenAt.IsZero() {
		return 0
	}
	return m.firstTextTokenAt.Sub(m.Start)
}

// TimeToFirstThinkingToken returns the time from stream start to the first thinking token.
func (m StreamMetrics) TimeToFirstThinkingToken() time.Duration {
	if m.firstThinkingTokenAt.IsZero() {
		return 0
	}
	return m.firstThinkingTokenAt.Sub(m.Start)
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
	return float64(m.TotalTokens()) / elapsed
}

// addTextChars adds character count to textChars and records firstTextTokenAt on first call.
func (m *StreamMetrics) addTextChars(n int) {
	if m.textChars == 0 && n > 0 {
		m.firstTextTokenAt = time.Now()
	}
	m.textChars += n
}

// addThinkChars adds character count to thinkingChars and records firstThinkingTokenAt on first call.
func (m *StreamMetrics) addThinkChars(n int) {
	if m.thinkingChars == 0 && n > 0 {
		m.firstThinkingTokenAt = time.Now()
	}
	m.thinkingChars += n
}

// countTokensApproxInt estimates token count from a character count.
func countTokensApproxInt(chars int) int {
	n := chars / 4
	if n == 0 && chars > 0 {
		n = 1
	}
	return n
}
