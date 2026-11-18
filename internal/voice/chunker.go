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
type Chunker interface {
	// Feed appends streamed text and returns complete chunks, if any.
	Feed(text string) []string
	// Flush drains the remainder exactly once (stream end). A subsequent
	// Feed starts fresh; Flush after Flush returns "".
	Flush() string
	// ArmHoldTimer arms the owned hold timer: if no further Feed arrives
	// within MaxHold while a fragment is held, cb fires with the held
	// fragment as text. Every Feed re-arms; Flush and Cancel disarm.
	ArmHoldTimer(cb func(text string))
	// Cancel disarms the timer and discards all buffered state.
	Cancel()
}

// SentenceChunker is the default Chunker implementation.
type SentenceChunker struct {
	minWords   int      // hold fragments below this word count
	maxHold    time.Duration
	maxWords   int      // force-flush threshold for the accumulated chunk
	boundaries []string // e.g. [".", "!", "?", "\n"]

	mu       sync.Mutex
	buf      string  // raw text not yet cut at a boundary
	held     string  // short fragment waiting to merge with the next
	flushed  bool    // Flush already drained this cycle
	cb          func(string)  // current hold callback (nil means disarmed)
	timer       timerIface   // owned timer; tests may inject a manual one
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

// Feed implements Chunker.
func (c *SentenceChunker) Feed(text string) []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.flushed {
		c.flushed = false // a new stream cycle starts
		c.buf, c.held = "", ""
	}
	c.buf += text

	var out []string
	for {
		sent, rest := cutAtBoundary(c.buf, c.boundaries)
		if sent == "" {
			break
		}
		c.buf = rest
		if chunk := c.mergeLocked(sent); chunk != "" {
			out = append(out, chunk)
		}
	}
	// A run-on with no boundary never reaches mergeLocked; if the
	// accumulated buffer alone exceeds maxWords, force-flush it here so a
	// long punctuation-free stream still emits bounded chunks.
	if s := strings.TrimSpace(c.buf); wordCount(s) > c.maxWords {
		words := strings.Fields(s)
		take := words[:c.maxWords]
		c.held = strings.Join(words[c.maxWords:], " ")
		c.buf = ""
		out = append(out, strings.Join(take, " "))
	}
	c.rearmLocked()
	return out
}

// mergeLocked folds a freshly cut sentence into the held fragment (if the
// held fragment is too short to stand alone) and force-flushes when the
// result exceeds maxWords. Caller holds mu.
func (c *SentenceChunker) mergeLocked(sentence string) string {
	sentence = strings.TrimSpace(sentence)
	if c.held != "" && wordCount(c.held) < c.minWords {
		sentence = strings.TrimSpace(c.held) + " " + sentence
		c.held = ""
	}
	if wordCount(sentence) > c.maxWords {
		// Force-flush: emit the first maxWords words now; the overflow
		// stays held so the next sentence merges with it instead of
		// being lost.
		words := strings.Fields(sentence)
		take := words[:c.maxWords]
		c.held = strings.Join(words[c.maxWords:], " ")
		return strings.Join(take, " ")
	}
	// Short sentences are held for merging with the next one; they are
	// emitted by the hold timer (stall), the next Feed (merge), or Flush.
	if wordCount(sentence) < c.minWords {
		c.held = sentence
		return ""
	}
	return sentence
}

// cutAtBoundary splits s at the first occurrence of any boundary string,
// returning the sentence (boundary included) and the remainder. Returns
// ("", s) when no boundary occurs.
func cutAtBoundary(s string, boundaries []string) (sentence, rest string) {
	best := -1
	var bestB string
	for _, b := range boundaries {
		i := strings.Index(s, b)
		if i >= 0 && (best == -1 || i < best) {
			best, bestB = i, b
		}
	}
	if best == -1 {
		return "", s
	}
	// Include the boundary itself in the sentence so punctuation survives
	// into TTS ("Hello." not "Hello").
	end := best + len(bestB)
	return s[:end], s[end:]
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
	if c.held == "" || c.cb == nil {
		c.stopTimerOnlyLocked()
		return
	}
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
// same message can also time out (only Cancel/Flush disarm it). It is safe
// to call directly on the firing channel's goroutine; it never blocks while
// holding mu.
func (c *SentenceChunker) fireHeld() {
	c.mu.Lock()
	if c.held == "" || c.cb == nil || c.flushed {
		c.mu.Unlock()
		return
	}
	frag, cb := c.held, c.cb
	c.held = ""
	c.mu.Unlock()
	cb(frag)
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
