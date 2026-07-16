package chat

import "time"

// PartialTool holds streaming-in-progress state for a single tool call.
type PartialTool struct {
	ID      string
	Type    string
	Name    string
	Args    string
	Chars   int
	FirstAt time.Time
	DoneAt  time.Time
}

// StreamState bundles pure transient fields for an active inference stream.
type StreamState struct {
	Text            string
	Thinking        string
	InThinking      bool
	Active          bool
	Metrics         StreamMetrics
	StreamCancelled bool
	CancelMessage   string
	CancelFn        func(msg string)
	PartialTools    []PartialTool
}

// AddTextChunk appends text and updates metrics.
func (ss *StreamState) AddTextChunk(text string) {
	ss.Text += text
	ss.Metrics.AddTextChars(text)
}

// AddThinkChunk appends thinking text and updates metrics.
func (ss *StreamState) AddThinkChunk(think string) {
	ss.Thinking += think
	ss.Metrics.AddThinkChars(think)
}

// AddToolCallChunk tracks tool call argument streaming for timing/token metrics.
func (ss *StreamState) AddToolCallChunk(delta string) {
	ss.Metrics.AddToolCallChars(delta)
}

// MarkCancelled records that the current stream was intentionally cancelled.
func (ss *StreamState) MarkCancelled(msg string) {
	ss.StreamCancelled = true
	ss.CancelMessage = msg
}

// Cancel cancels the current stream using the stream-owned cancellation wrapper.
func (ss *StreamState) Cancel(msg string) {
	if ss.CancelFn != nil {
		ss.CancelFn(msg)
		return
	}
	ss.MarkCancelled(msg)
}

// Reset clears pure stream state before a new request.
func (ss *StreamState) Reset() {
	*ss = StreamState{}
}
