package chat

import (
	"time"

	"squid-os/internal/log"
)

const MinSpeedInferenceDuration = 150 * time.Millisecond

// StreamMetrics owns all timing and token-count metrics for an active inference stream.
// Text/thinking accumulation lives in StreamState; this type owns the derived metrics.
type StreamMetrics struct {
	Start                time.Time
	firstThinkingTokenAt time.Time
	thinkingDoneAt       time.Time
	firstTextTokenAt     time.Time
	textDoneAt           time.Time
	thinkingChars        int
	textChars            int
	firstToolCallTokenAt time.Time
	toolCallDoneAt       time.Time
	toolCallChars        int
}

// AddThinkChars adds character count to thinkingChars and records firstThinkingTokenAt on first call.
func (m *StreamMetrics) AddThinkChars(s string) {
	n := len(s)
	if m.thinkingChars == 0 && n > 0 {
		m.firstThinkingTokenAt = time.Now()
	}
	m.thinkingChars += n
	m.thinkingDoneAt = time.Now()
	log.LogStreamMetrics("addThinkChars", s, n, m.thinkingChars, m.firstThinkingTokenAt, m.thinkingDoneAt)
}

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

func (m StreamMetrics) ThinkingTokens() int { return CountTokensApproxInt(m.thinkingChars) }

func (m StreamMetrics) TimeToFirstThinkingToken() time.Duration {
	if m.firstThinkingTokenAt.IsZero() {
		return 0
	}
	return m.firstThinkingTokenAt.Sub(m.Start)
}

// AddTextChars adds character count to textChars and records firstTextTokenAt on first call.
func (m *StreamMetrics) AddTextChars(s string) {
	n := len(s)
	if m.textChars == 0 && n > 0 {
		m.firstTextTokenAt = time.Now()
	}
	m.textChars += n
	m.textDoneAt = time.Now()
	log.LogStreamMetrics("addTextChars", s, n, m.textChars, m.firstTextTokenAt, m.textDoneAt)
}

func (m StreamMetrics) TextTokens() int { return CountTokensApproxInt(m.textChars) }

func (m StreamMetrics) TimeToFirstTextToken() time.Duration {
	if m.firstTextTokenAt.IsZero() {
		return 0
	}
	return m.firstTextTokenAt.Sub(m.Start)
}

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

// AddToolCallChars adds character count to toolCallChars and records firstToolCallTokenAt on first call.
func (m *StreamMetrics) AddToolCallChars(s string) {
	n := len(s)
	if m.toolCallChars == 0 && n > 0 {
		m.firstToolCallTokenAt = time.Now()
	}
	m.toolCallChars += n
	m.toolCallDoneAt = time.Now()
	log.LogStreamMetrics("addToolCallChars", s, n, m.toolCallChars, m.firstToolCallTokenAt, m.toolCallDoneAt)
}

func (m StreamMetrics) ToolCallTokens() int { return CountTokensApproxInt(m.toolCallChars) }

func (m StreamMetrics) TimeToFirstToolCallToken() time.Duration {
	if m.firstToolCallTokenAt.IsZero() {
		return 0
	}
	return m.firstToolCallTokenAt.Sub(m.Start)
}

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

func (m StreamMetrics) HasFirstToken() bool {
	return !m.firstThinkingTokenAt.IsZero() || !m.firstTextTokenAt.IsZero() || !m.firstToolCallTokenAt.IsZero()
}

func (m StreamMetrics) TotalOutputTokens() int {
	return CountTokensApproxInt(m.thinkingChars + m.textChars + m.toolCallChars)
}

func (m StreamMetrics) InferenceDuration() time.Duration {
	return m.TextDuration() + m.ThinkingDuration() + m.ToolCallDuration()
}

func (m StreamMetrics) Duration() time.Duration {
	return m.TimeToFirstToken() + m.InferenceDuration()
}

func (m StreamMetrics) TimeToFirstToken() time.Duration {
	t := m.firstTokenAt()
	if t.IsZero() {
		return 0
	}
	return t.Sub(m.Start)
}

func (m StreamMetrics) LastActivity() time.Time {
	latest := m.textDoneAt
	if !m.thinkingDoneAt.IsZero() && m.thinkingDoneAt.After(latest) {
		latest = m.thinkingDoneAt
	}
	if !m.toolCallDoneAt.IsZero() && m.toolCallDoneAt.After(latest) {
		latest = m.toolCallDoneAt
	}
	return latest
}

func (m StreamMetrics) AvgTokenPerSec() float64 {
	d := m.InferenceDuration()
	if d < MinSpeedInferenceDuration {
		return 0
	}
	return float64(m.TotalOutputTokens()) / d.Seconds()
}

// CountTokensApproxInt estimates token count from a character count.
func CountTokensApproxInt(chars int) int {
	n := chars / 4
	if n == 0 && chars > 0 {
		n = 1
	}
	return n
}
