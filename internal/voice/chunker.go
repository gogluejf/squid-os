package voice

import (
	"strings"
	"sync"
	"time"
)

// Chunker splits a streaming assistant response into speakable chunks.
//
// The strategy is sentence-boundary based: text is split on the configured
// boundaries, fragments shorter than MinWords are held and merged with the
// next fragment (one-word bullets become one group), and the held fragment
// is force-emitted when input stalls for MaxHold (via the owned timer) or
// when the accumulated chunk reaches MaxWords.
//
// All Feed/Flush calls must happen on the stream goroutine; the hold-timer
// callback may run on the timer goroutine, so cb must be safe to call from
// any goroutine (typically it hands the chunk to the Coordinator).
// Callbacks must return promptly and must not re-enter the chunker.
type Chunker interface {
	// Feed appends streamed text and returns complete chunks, if any.
	Feed(text string) []string
	// Flush drains the remainder exactly once (stream end). A subsequent
	// Feed starts fresh; Flush after Flush returns "".
	Flush() string
	// ArmHoldTimer arms the owned hold timer: if no further Feed arrives
	// within MaxHold while text is buffered, cb fires with that text. Further Feed calls do not extend the deadline; Flush and Cancel disarm.
	ArmHoldTimer(cb func(text string))
	// Cancel disarms the timer and discards all buffered state.
	Cancel()
}

// SentenceChunker is the default Chunker implementation.
type SentenceChunker struct {
	batchWords int // after first emission; zero retains sentence strategy
	emitted    bool
	armed      bool
	minWords   int // hold fragments below this word count
	maxHold    time.Duration
	maxWords   int      // force-flush threshold for the accumulated chunk
	boundaries []string // e.g. [".", "!", "?", "\n"]

	mu          sync.Mutex
	buf         string        // raw text not yet cut at a boundary
	held        string        // short fragment waiting to merge with the next
	flushed     bool          // Flush already drained this cycle
	cb          func(string)  // current hold callback (nil means disarmed)
	timer       timerIface    // owned timer; tests may inject a manual one
	watcherDone chan struct{} // closed when the watcher goroutine exits
}

// timerIface is the slice of time.Timer the chunker needs; tests inject a
// manual implementation so hold expiry fires deterministically.
type timerIface interface {
	Reset(d time.Duration) bool
	Stop() bool
	C() <-chan time.Time
}

// realTimer adapts *time.Timer to timerIface.
type realTimer struct{ t *time.Timer }

func (r realTimer) Reset(d time.Duration) bool { return r.t.Reset(d) }
func (r realTimer) Stop() bool                 { return r.t.Stop() }
func (r realTimer) C() <-chan time.Time        { return r.t.C }

// NewSentenceChunker builds a Chunker with the given strategy parameters.
// Defaults: minWords=3, maxHold=1.5s, maxWords=40, boundaries ". ! ? \n".
func NewSentenceChunker(minWords int, maxHold time.Duration, maxWords int, boundaries []string) *SentenceChunker {
	if minWords <= 0 {
		minWords = 3
	}
	if maxHold <= 0 {
		maxHold = 1500 * time.Millisecond
	}
	if maxWords <= 0 {
		maxWords = 40
	}
	if len(boundaries) == 0 {
		boundaries = []string{".", "!", "?", "\n"}
	}
	return &SentenceChunker{
		minWords:   minWords,
		maxHold:    maxHold,
		maxWords:   maxWords,
		boundaries: boundaries,
	}
}

// NewNarrationChunker accumulates streamed text until about 300 characters,
// then emits up to the first completed sentence boundary. Narration treats
// only ., !, and ? followed by whitespace or EOF as boundaries; newlines are
// not cut points because Markdown bullets and wrapped lines are common.
func NewNarrationChunker() *SentenceChunker {
	c := NewSentenceChunker(1, 1500*time.Millisecond, 1000, []string{".", "!", "?"})
	c.batchWords = 0
	return c
}

// targetChunkSize is the character threshold before we look for a cut point.
const targetChunkSize = 3000

// Feed implements Chunker.
func (c *SentenceChunker) Feed(text string) []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.flushed {
		c.flushed = false
		c.buf, c.held = "", ""
		c.emitted = false
	}
	c.buf += text

	var out []string

	// Only try to emit if we have enough text accumulated.
	for len(c.buf) >= targetChunkSize {
		// Find the first sentence boundary in the buffer.
		cutPos := c.findSentenceBoundaryAtOrAfterTarget(c.buf)
		if cutPos <= 0 {
			break // no boundary found, keep accumulating
		}
		chunk := strings.TrimSpace(c.buf[:cutPos])
		c.buf = c.buf[cutPos:]
		if chunk != "" {
			out = append(out, chunk)
			c.emitted = true
		}
	}

	c.rearmLocked()
	return out
}

// findSentenceBoundaryAtOrAfterTarget finds the first completed sentence
// boundary whose cut position is at least targetChunkSize. This keeps short
// leading sentences merged into the first spoken chunk while still cutting
// at real sentence boundaries rather than mid-word. It returns the index
// just after the boundary punctuation, or 0 if no safe boundary exists.
func (c *SentenceChunker) findSentenceBoundaryAtOrAfterTarget(s string) int {
	best := 0
	for _, b := range c.boundaries {
		start := 0
		for {
			i := strings.Index(s[start:], b)
			if i < 0 {
				break
			}
			abs := start + i
			end := abs + len(b)
			if end >= targetChunkSize && (end == len(s) || isWhitespace(s[end])) {
				if best == 0 || end < best {
					best = end
				}
				break // first boundary for this char is found; try next char
			}
			start = abs + 1
		}
	}
	return best
}

func isWhitespace(b byte) bool {
	return b == ' ' || b == '\t' || b == '\n' || b == '\r'
}

// Flush implements Chunker. Drains buf+held exactly once per cycle.
func (c *SentenceChunker) Flush() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.flushed {
		return ""
	}
	c.flushed = true
	c.disarmLocked()

	// held was cut from the stream BEFORE buf, so it must come first in the
	// output; buf is the most recent incomplete fragment.
	parts := make([]string, 0, 2)
	if h := strings.TrimSpace(c.held); h != "" {
		parts = append(parts, h)
	}
	if s := strings.TrimSpace(c.buf); s != "" {
		parts = append(parts, s)
	}
	c.buf, c.held = "", ""
	return strings.Join(parts, " ")
}

// ArmHoldTimer implements Chunker.
func (c *SentenceChunker) ArmHoldTimer(cb func(string)) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.cb = cb
	if c.timer != nil && c.watcherDone == nil {
		// Timer was injected externally (test seam): start the watcher now.
		c.watcherDone = make(chan struct{})
		go c.watchTimer(c.watcherDone)
	}
	c.rearmLocked()
}

// Cancel implements Chunker.
func (c *SentenceChunker) Cancel() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.stopWatcherLocked()
	c.timer = nil
	c.disarmLocked()
	c.buf, c.held = "", ""
}

// rearmLocked resets the hold timer if a fragment is currently held, or
// stops the timer otherwise (keeping cb so a later Feed can re-arm).
// Caller holds mu.
func (c *SentenceChunker) rearmLocked() {
	if strings.TrimSpace(c.held+c.buf) == "" || c.cb == nil {
		c.stopTimerOnlyLocked()
		return
	}
	if c.armed {
		return
	}
	c.armed = true
	if c.timer == nil {
		rt := time.NewTimer(c.maxHold)
		c.timer = realTimer{t: rt}
	}
	if c.watcherDone == nil {
		c.watcherDone = make(chan struct{})
		go c.watchTimer(c.watcherDone)
	}
	// Both real and manual timers satisfy timerIface; Stop/Reset are part
	// of the interface, so no type assertion is needed here.
	if !c.timer.Stop() {
		select {
		case <-c.timer.C():
		default:
		}
	}
	c.timer.Reset(c.maxHold)
}

// watchTimer drains the owned timer's firing channel until done is closed
// and fires the held fragment on each expiry.
func (c *SentenceChunker) watchTimer(done chan struct{}) {
	var ch <-chan time.Time
	c.mu.Lock()
	if c.timer != nil {
		ch = c.timer.C()
	}
	c.mu.Unlock()
	for {
		select {
		case <-done:
			return
		case <-ch:
			c.fireHeld()
		}
	}
}

// fireHeld emits the held fragment via the armed callback when the timer
// expires and the fragment is still held. It clears only the emitted
// fragment; the callback stays armed so a subsequent held fragment in the
// same message can also time out (only Cancel/Flush disarm it). The callback
// runs under mu to order admission before Flush/EndGroup; it must be prompt
// and must not re-enter the chunker.
func (c *SentenceChunker) fireHeld() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if strings.TrimSpace(c.held+c.buf) == "" || c.cb == nil || c.flushed {
		return
	}
	frag := strings.TrimSpace(strings.TrimSpace(c.held) + " " + strings.TrimSpace(c.buf))
	// Only emit if we're at a sentence boundary. Never cut mid-sentence.
	if !c.endsWithBoundary(frag) {
		return // not at a sentence end, keep waiting
	}
	c.held, c.buf = "", ""
	c.emitted = true
	cb := c.cb
	c.armed = false
	c.rearmLocked()
	cb(frag)
}

// endsWithBoundary reports whether s ends with one of the chunker's sentence
// boundaries after ignoring trailing whitespace.
func (c *SentenceChunker) endsWithBoundary(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" {
		return false
	}
	for _, b := range c.boundaries {
		if strings.HasSuffix(s, b) {
			return true
		}
	}
	return false
}

// FireHeld is the test seam for the hold-timer expiry: it behaves exactly
// like a timer firing and emits the held fragment through the armed
// callback (if any). Production code uses the owned timer instead.
func (c *SentenceChunker) FireHeld() {
	c.mu.Lock()
	if c.timer != nil {
		c.timer.Stop()
	}
	c.mu.Unlock()
	c.fireHeld()
}

// stopTimerOnlyLocked stops the timer without clearing the callback. Used
// when no fragment is held yet but a cb may arrive later. Caller holds mu.
func (c *SentenceChunker) stopTimerOnlyLocked() {
	c.armed = false
	if c.timer != nil {
		if !c.timer.Stop() {
			select {
			case <-c.timer.C():
			default:
			}
		}
	}
	c.stopWatcherLocked()
}

// disarmLocked stops the timer and clears the callback. Caller holds mu.
func (c *SentenceChunker) disarmLocked() {
	c.armed = false
	if c.timer != nil {
		if !c.timer.Stop() {
			select {
			case <-c.timer.C():
			default:
			}
		}
	}
	c.stopWatcherLocked()
	c.cb = nil
}

// stopWatcherLocked signals the watcher goroutine to exit if one is running.
// It must be called before replacing c.timer. Caller holds mu.
func (c *SentenceChunker) stopWatcherLocked() {
	if c.watcherDone != nil {
		close(c.watcherDone)
		c.watcherDone = nil
	}
}

// wordCount counts whitespace-separated words.
func wordCount(s string) int { return len(strings.Fields(s)) }
