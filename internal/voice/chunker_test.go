package voice

import (
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
)

// --- helpers ---------------------------------------------------------------

func testChunker() *SentenceChunker {
	return NewSentenceChunker(3, 1500*time.Millisecond, 40, nil)
}

// words builds a sentence of n distinct words ending with a period.

// holdTimer is a manual stand-in for the chunker's owned time.Timer. Its
// fire() drives the chunker's FireHeld seam directly, so hold expiry fires
// deterministically with no wall-clock waits or watcher goroutines.
type holdTimer struct {
	c  *SentenceChunker
	ch chan time.Time
}

func newHoldTimer(c *SentenceChunker) *holdTimer {
	return &holdTimer{c: c, ch: make(chan time.Time, 1)}
}

func (t *holdTimer) Reset(d time.Duration) bool { return true }
func (t *holdTimer) Stop() bool                 { return true }
func (t *holdTimer) C() <-chan time.Time        { return t.ch }
func (t *holdTimer) fire()                      { t.ch <- time.Time{} }

// armChunker injects the manual timer and arms the hold callback; it
// returns a channel receiving whatever the callback emits.
func armChunker(c *SentenceChunker) (<-chan string, *holdTimer) {
	out := make(chan string, 8)
	tm := newHoldTimer(c)
	c.mu.Lock()
	c.timer = tm
	c.mu.Unlock()
	c.ArmHoldTimer(func(frag string) { out <- frag })
	return out, tm
}

// drainOut reports whether any fragment was emitted on out so far.
func drainOut(t *testing.T, name string, out <-chan string) {
	t.Helper()
	select {
	case frag := <-out:
		t.Fatalf("%s: unexpected emission %q", name, frag)
	default:
	}
}

// expectOut waits briefly for one emission on out (the chunker's watcher
// goroutine delivers asynchronously; this is the only bounded wait in tests).
func expectOut(t *testing.T, name string, out <-chan string) string {
	t.Helper()
	select {
	case frag := <-out:
		return frag
	case <-time.After(500 * time.Millisecond):
		t.Fatalf("%s: expected emission did not arrive", name)
		return ""
	}
}

// --- Feed: sentence boundaries ---------------------------------------------

func TestFeedEmitsPerSentence(t *testing.T) {
	c := testChunker() // minWords=3
	if got := c.Feed("Hello world. This is fine!"); len(got) != 0 {
		t.Fatalf("short text emitted %v before reaching target size", got)
	}
	if rest := c.Flush(); rest != "Hello world. This is fine!" {
		t.Fatalf("Flush = %q", rest)
	}
}

func TestFeedEmitsUpToLastCompletedSentenceBeyondTarget(t *testing.T) {
	c := NewSentenceChunker(1, time.Second, 1000, nil)
	text := "First short. " + strings.Repeat("padding word ", 40) + "Final long sentence. Remainder."
	got := c.Feed(text)
	want := text[:strings.LastIndex(text, "Final long sentence.")+len("Final long sentence.")]
	if len(got) != 1 || got[0] != want {
		t.Fatalf("got %q, want %q", got, want)
	}
	if rest := c.Flush(); rest != "Remainder." {
		t.Fatalf("Flush = %q", rest)
	}
}

func TestFeedSplitsAcrossFeeds(t *testing.T) {
	c := NewSentenceChunker(1, time.Second, 1000, nil)
	text := strings.Repeat("buffered text without a boundary ", 12) + "First sentence. Second."
	var out []string
	for i := 0; i < len(text); i += 11 {
		out = append(out, c.Feed(text[i:min(i+11, len(text))])...)
	}
	if len(out) != 1 || out[0] != strings.TrimSuffix(text, " Second.") {
		t.Fatalf("got %q", out)
	}
	if rest := c.Flush(); rest != "Second." {
		t.Fatalf("Flush = %q", rest)
	}
}

func TestNewlinesAreNotNarrationBoundaries(t *testing.T) {
	c := NewNarrationChunker()
	defer c.Cancel()
	text := strings.Repeat("markdown bullet text without terminal punctuation ", 10) + "\nFinal sentence."
	var out []string
	for i := 0; i < len(text); i += 13 {
		out = append(out, c.Feed(text[i:min(i+13, len(text))])...)
	}
	if len(out) != 1 || !strings.HasSuffix(out[0], "Final sentence.") {
		t.Fatalf("chunks = %q", out)
	}
	if rest := c.Flush(); rest != "" {
		t.Fatalf("Flush = %q", rest)
	}
}

// --- target-size accumulation --------------------------------------------------

func TestFeedBelowTargetSizeHoldsText(t *testing.T) {
	c := testChunker()
	if got := c.Feed("Short sentence."); len(got) != 0 {
		t.Fatalf("emitted %q before target size", got)
	}
	if rest := c.Flush(); rest != "Short sentence." {
		t.Fatalf("Flush = %q", rest)
	}
}

func TestFeedAtTargetSizeEmitsFirstCompletedSentence(t *testing.T) {
	c := NewSentenceChunker(1, time.Second, 1000, nil)
	prefix := strings.Repeat("padding words to reach the buffer threshold ", 12)
	got := c.Feed(prefix + "First complete sentence. Remainder stays buffered.")
	want := prefix + "First complete sentence."
	if len(got) != 1 || got[0] != want {
		t.Fatalf("got %q, want %q", got, want)
	}
	if rest := c.Flush(); rest != "Remainder stays buffered." {
		t.Fatalf("Flush = %q", rest)
	}
}

func TestFeedCutsAtFirstPeriodBeyondTarget(t *testing.T) {
	c := NewSentenceChunker(1, time.Second, 1000, nil)
	// Each "sentence" ends with a period+space. First period is at ~40 chars,
	// well below 300. The first period BEYOND 300 should be the cut point.
	text := strings.Repeat("this is a test sentence. ", 20) + "Final word."
	got := c.Feed(text)
	// Find first ". " at or after position 300; cut is after the period (not the space)
	cut := 0
	for i := 300; i < len(text); i++ {
		if text[i] == '.' && i+1 < len(text) && text[i+1] == ' ' {
			cut = i + 1 // position just after the period
			break
		}
	}
	want := strings.TrimSpace(text[:cut])
	if len(got) != 1 || got[0] != want {
		t.Fatalf("got %q (len %d), want %q (len %d)", got[0], len(got[0]), want, len(want))
	}
	if rest := c.Flush(); rest != strings.TrimSpace(text[cut:]) {
		t.Fatalf("Flush = %q, want %q", rest, strings.TrimSpace(text[cut:]))
	}
}

// --- max-size force flush ----------------------------------------------------

func TestMaxWordsNoLongerForceFlushesMidSentence(t *testing.T) {
	c := NewSentenceChunker(3, time.Second, 5, nil)
	text := strings.TrimRight(strings.Repeat("unpunctuated streaming words ", 20), " ")
	var out []string
	for i := 0; i < len(text); i += 9 {
		out = append(out, c.Feed(text[i:min(i+9, len(text))])...)
	}
	if len(out) != 0 {
		t.Fatalf("mid-sentence chunks emitted: %q", out)
	}
	if rest := c.Flush(); rest != text {
		t.Fatalf("Flush = %q", rest)
	}
}

// --- Flush semantics ---------------------------------------------------------

func TestMaxWordsForceFlushNoPunctuation(t *testing.T) {
	c := NewSentenceChunker(3, time.Second, 5, nil)
	// 50 words with NO periods: a single uncut run-on that never reaches
	// mergeLocked, so only the buf-level force-flush can bound it.
	var b strings.Builder
	for i := 0; i < 50; i++ {
		if i > 0 {
			b.WriteByte(' ')
		}
		fmt.Fprintf(&b, "w%02d", i)
	}
	if got := c.Feed(b.String()); len(got) != 0 {
		t.Fatalf("Feed emitted %q", got)
	}
	if rest := c.Flush(); rest != b.String() {
		t.Fatalf("Flush = %q", rest)
	}
}

func TestFlushDrainsExactlyOnce(t *testing.T) {
	c := testChunker()
	c.Feed("Incomplete thought")
	first := c.Flush()
	if first != "Incomplete thought" {
		t.Fatalf("first Flush = %q", first)
	}
	if second := c.Flush(); second != "" {
		t.Errorf("second Flush = %q, want empty", second)
	}
}

func TestFeedAfterFlushStartsFresh(t *testing.T) {
	c := testChunker()
	c.Feed("Old text")
	c.Flush()
	if got := c.Feed("New start. End."); len(got) != 0 {
		t.Fatalf("short text emitted %v before target size", got)
	}
	if rest := c.Flush(); rest != "New start. End." {
		t.Errorf("Flush = %q", rest)
	}
}

func TestFlushEmptyWhenNothingBuffered(t *testing.T) {
	c := testChunker()
	if got := c.Flush(); got != "" {
		t.Errorf("Flush on fresh chunker = %q, want empty", got)
	}
}

// --- hold timer (manual trigger — no wall-clock polling) ----------------------

func TestHoldTimerEmitsHeldFragmentOnStall(t *testing.T) {
	c := NewSentenceChunker(3, 50*time.Millisecond, 40, nil)
	out, tm := armChunker(c)

	c.Feed("Hi there.") // 2 words < 3 → held
	tm.fire()           // input stalled past MaxHold
	if frag := expectOut(t, "stall", out); frag != "Hi there." {
		t.Fatalf("emitted %q, want \"Hi there.\"", frag)
	}
}

func TestHoldTimerCancelledByFlush(t *testing.T) {
	c := NewSentenceChunker(3, 30*time.Millisecond, 40, nil)
	out, tm := armChunker(c)
	c.Feed("Hi there.")

	if got := c.Flush(); got != "Hi there." {
		t.Fatalf("Flush = %q, want %q", got, "Hi there.")
	}
	tm.fire() // must be a no-op after Flush
	drainOut(t, "flush-cancel", out)

	select {
	case frag := <-out:
		t.Fatalf("timer fired after Flush with %q", frag)
	default:
	}
}

func TestHoldTimerDisarmedWhenNoHeldFragment(t *testing.T) {
	c := NewSentenceChunker(3, 30*time.Millisecond, 40, nil)
	out, tm := armChunker(c)
	c.Feed("A long enough sentence goes out now.") // 7 words ≥ 3 → emitted, nothing held

	tm.fire() // must be a no-op: nothing held
	drainOut(t, "disarmed", out)

	select {
	case frag := <-out:
		t.Fatalf("timer fired with %q although nothing was held", frag)
	default:
	}
}

func TestHoldTimerRearmedByFeed(t *testing.T) {
	c := NewSentenceChunker(3, 200*time.Millisecond, 40, nil)
	out, tm := armChunker(c)
	c.Feed("Hi there.") // held; timer armed at t=0
	tm.fire()           // first arming expires → emits the held fragment
	frag := expectOut(t, "first-arm", out)
	if frag != "Hi there." {
		t.Fatalf("emitted %q, want \"Hi there.\"", frag)
	}
	// The callback must stay armed after a fire: a second held fragment in
	// the SAME chunker instance must also time out (fireHeld clears only
	// the emitted fragment, not cb).
	c2frag := c.Feed("Go now.") // 2 words < 3 → held; timer re-armed by Feed
	if len(c2frag) != 0 {
		t.Fatalf("second feed emitted %v, want [] (held)", c2frag)
	}
	tm.fire() // the LIVE arming (post-rearm) expires → second fragment fires
	frag2 := expectOut(t, "rearm-live", out)
	if frag2 != "Go now." {
		t.Fatalf("emitted %q, want \"Go now.\"", frag2)
	}
	drainOut(t, "rearm-extra", out)
}

func TestCancelDiscardsBufferAndTimer(t *testing.T) {
	c := NewSentenceChunker(3, 30*time.Millisecond, 40, nil)
	out, tm := armChunker(c)
	c.Feed("Pending fragment")
	c.Cancel()

	tm.fire() // must be a no-op after Cancel
	drainOut(t, "cancel", out)

	select {
	case frag := <-out:
		t.Fatalf("timer fired after Cancel with %q", frag)
	default:
	}
	if got := c.Flush(); got != "" {
		t.Errorf("Flush after Cancel = %q, want empty", got)
	}
}

// --- constructor defaults ------------------------------------------------------

func TestConstructorDefaults(t *testing.T) {
	c := NewSentenceChunker(0, 0, 0, nil)
	if c.minWords != 3 || c.maxHold != 1500*time.Millisecond || c.maxWords != 40 {
		t.Errorf("defaults = (%d, %v, %d)", c.minWords, c.maxHold, c.maxWords)
	}
	if len(c.boundaries) != 4 {
		t.Errorf("boundaries = %v", c.boundaries)
	}
}

func TestCustomBoundaries(t *testing.T) {
	c := NewSentenceChunker(1, time.Second, 1000, []string{"§"})
	// Prefix is ~480 chars, so the first § is well beyond 300.
	prefix := strings.Repeat("padding to reach the configured target size ", 10)
	text := prefix + "Part one§ Part two§"
	got := c.Feed(text)
	want := prefix + "Part one§"
	if len(got) != 1 || got[0] != want {
		t.Fatalf("got %q, want %q", got, want)
	}
	if rest := c.Flush(); rest != "Part two§" {
		t.Fatalf("Flush = %q", rest)
	}
}

func TestNarrationAccumulatesTo300ThenFirstCompletedSentence(t *testing.T) {
	c := NewNarrationChunker()
	defer c.Cancel()

	text := strings.Repeat("working through a streamed answer ", 12) + "done."
	var out []string
	for i := 0; i < len(text); i += 7 {
		end := min(i+7, len(text))
		out = append(out, c.Feed(text[i:end])...)
	}

	if len(out) != 1 || !strings.HasSuffix(out[0], "done.") {
		t.Fatalf("chunks = %q", out)
	}
	if rest := c.Flush(); rest != "" {
		t.Fatalf("Flush = %q, want empty", rest)
	}
}

func TestNarrationDoesNotSplitMarkdownBulletsOrMidWord(t *testing.T) {
	c := NewNarrationChunker()
	defer c.Cancel()

	text := "**Summary:**\n- First point arrives in small pieces.\n- Second point keeps the same word intact across feeds.\nFinal sentence ends cleanly."
	var out []string
	for i := 0; i < len(text); i += 9 {
		end := min(i+9, len(text))
		out = append(out, c.Feed(text[i:end])...)
	}
	out = append(out, c.Flush())

	joined := strings.Join(out, "\x00")
	if joined != text {
		t.Fatalf("recombined chunks lost or reordered text:\n%q", joined)
	}
	for _, chunk := range out {
		trimmed := strings.TrimSpace(chunk)
		switch {
		case strings.HasSuffix(trimmed, ".") || strings.HasSuffix(trimmed, "!") || strings.HasSuffix(trimmed, "?"):
		default:
			t.Fatalf("chunk does not end at completed punctuation: %q", chunk)
		}
	}
}

func TestNarrationHoldEmitsOnlyAtCompletedPunctuation(t *testing.T) {
	c := NewNarrationChunker()
	out, tm := armChunker(c)
	defer c.Cancel()

	c.Feed(strings.Repeat("incomplete buffer without terminal punctuation ", 12))
	tm.fire()
	drainOut(t, "mid-sentence hold", out)

	got := c.Feed(" finished.")
	if len(got) != 1 || !strings.HasSuffix(got[0], "finished.") {
		t.Fatalf("Feed emitted %q, want one completed-sentence chunk", got)
	}
	drainOut(t, "after direct emission", out)
}

func TestHoldDeadlineWithContinuousInput(t *testing.T) {
	c := NewSentenceChunker(1, 40*time.Millisecond, 1000, nil)
	defer c.Cancel()
	out := make(chan string, 1)
	c.ArmHoldTimer(func(s string) { out <- s })
	// Feed enough text to exceed 300 chars, ending with a period.
	c.Feed(strings.Repeat("streamed buffer text here. ", 15))
	// No more Feed calls — the hold timer should fire after maxHold.
	select {
	case got := <-out:
		if !strings.HasSuffix(got, ".") {
			t.Fatalf("hold emitted %q, want ending with period", got)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("hold did not emit buffered text on stall")
	}
}

func TestChunkerConcurrentFeedFlushCancel(t *testing.T) {
	c := NewNarrationChunker()
	c.ArmHoldTimer(func(string) {})
	var wg sync.WaitGroup
	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				switch i {
				case 0:
					c.Feed("Some short sentence. ")
				case 1:
					c.FireHeld()
				case 2:
					c.Flush()
					c.Cancel()
				}
			}
		}(i)
	}
	wg.Wait()
	c.Cancel()
}
