package voice

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

// --- helpers ---------------------------------------------------------------

func testChunker() *SentenceChunker {
	return NewSentenceChunker(3, 1500*time.Millisecond, 40, nil)
}

// words builds a sentence of n distinct words ending with a period.
func words(n int) string {
	parts := make([]string, n)
	for i := range parts {
		parts[i] = "w" + string(rune('a'+i%26)) + string(rune('0'+i/26))
	}
	return strings.Join(parts, " ") + "."
}

// holdTimer is a manual stand-in for the chunker's owned time.Timer. Its
// fire() drives the chunker's FireHeld seam directly, so hold expiry fires
// deterministically with no wall-clock waits or watcher goroutines.
type holdTimer struct {
	c    *SentenceChunker
	ch   chan time.Time
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
	got := c.Feed("Hello world. This is fine!")
	// Both sentences are 2 words < minWords, so they merge into one chunk.
	if len(got) != 1 || got[0] != "Hello world. This is fine!" {
		t.Fatalf("got %v", got)
	}
	if rest := c.Flush(); rest != "" {
		t.Errorf("Flush after complete sentences = %q, want empty", rest)
	}
}

func TestFeedEmitsLongSentencesSeparately(t *testing.T) {
	c := testChunker()
	got := c.Feed("First sentence here. Second sentence too.")
	want := []string{"First sentence here.", "Second sentence too."}
	if len(got) != len(want) {
		t.Fatalf("got %d chunks %q, want %d %q", len(got), got, len(want), want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("chunk %d = %q, want %q", i, got[i], want[i])
		}
	}
	if rest := c.Flush(); rest != "" {
		t.Errorf("Flush = %q, want empty", rest)
	}
}

func TestFeedSplitsAcrossFeeds(t *testing.T) {
	c := testChunker()
	if got := c.Feed("First sen"); len(got) != 0 {
		t.Fatalf("partial feed emitted %v", got)
	}
	got := c.Feed("tence. Second")
	// "First sentence." is 2 words < minWords → held, not yet emitted.
	if len(got) != 0 {
		t.Fatalf("got %v, want [] (held)", got)
	}
	got = c.Feed(".")
	// The held fragment merges with the next cut ("Second.") and stands
	// alone at 4 words, so it emits as one group.
	if len(got) != 1 || got[0] != "First sentence. Second." {
		t.Fatalf("got %v, want [\"First sentence. Second.\"]", got)
	}
	got = c.Feed(" Third one.")
	// "Third one." is 2 words < minWords, so it is held until Flush.
	if len(got) != 0 {
		t.Fatalf("got %v, want [] (held)", got)
	}
	if rest := c.Flush(); rest != "Third one." {
		t.Errorf("Flush = %q, want %q", rest, "Third one.")
	}
}

func TestNewlinesAreBoundaries(t *testing.T) {
	c := testChunker()
	got := c.Feed("Line one.\nLine two\nLong enough line to stand alone.")
	// "Line one." (2 words) is held; "\n" (0 words) merges into it, then
	// "Line two" (2 words) merges in too → one group. The long sentence
	// stands alone.
	if len(got) != 2 {
		t.Fatalf("got %d chunks %q", len(got), got)
	}
	for _, part := range []string{"Line one.", "Line two"} {
		if !strings.Contains(got[0], part) {
			t.Errorf("merged chunk %q missing %q", got[0], part)
		}
	}
	if got[1] != "Long enough line to stand alone." {
		t.Errorf("second chunk = %q", got[1])
	}
}

// --- short-fragment merging --------------------------------------------------

func TestShortFragmentsMergeWithNext(t *testing.T) {
	c := testChunker() // minWords=3
	got := c.Feed("- apple\n- banana\nDetails here follow.")
	// "- apple" (1 word) is held; merged with "- banana" it reaches 3 words
	// and emits as its own group; "Details here follow." stands alone.
	if len(got) != 2 {
		t.Fatalf("got %d chunks %q, want 2", len(got), got)
	}
	for _, part := range []string{"apple", "banana"} {
		if !strings.Contains(got[0], part) {
			t.Errorf("first chunk %q missing %q", got[0], part)
		}
	}
	if got[1] != "Details here follow." {
		t.Errorf("second chunk = %q", got[1])
	}
}

func TestLongFragmentNotMerged(t *testing.T) {
	c := testChunker()
	long := "One two three four five six seven eight nine ten."
	got := c.Feed(long + " Short.")
	// First sentence has 10 words >= minWords, so it emits on its own;
	// "Short." (1 word) is held until Flush.
	if len(got) != 1 || got[0] != long {
		t.Fatalf("got %v, want [%q]", got, long)
	}
	if rest := c.Flush(); rest != "Short." {
		t.Errorf("Flush = %q, want %q", rest, "Short.")
	}
}

func TestTwoWordFragmentsMergeChain(t *testing.T) {
	c := testChunker() // minWords=3
	got := c.Feed("Alpha beta. Gamma delta. Long enough sentence to stand alone.")
	// "Alpha beta." (2 words) merges with "Gamma delta." reaching 4 words →
	// emits as one group; the long sentence stands alone.
	if len(got) != 2 {
		t.Fatalf("got %d chunks %q, want 2", len(got), got)
	}
	for _, part := range []string{"Alpha beta.", "Gamma delta."} {
		if !strings.Contains(got[0], part) {
			t.Errorf("first chunk %q missing %q", got[0], part)
		}
	}
	if got[1] != "Long enough sentence to stand alone." {
		t.Errorf("second chunk = %q", got[1])
	}
}

// --- max-size force flush ----------------------------------------------------

func TestMaxWordsForceFlush(t *testing.T) {
	c := NewSentenceChunker(3, time.Second, 5, nil)
	got := c.Feed(words(8)) // 8 words > maxWords=5 → force-flush
	if len(got) != 1 {
		t.Fatalf("got %d chunks %q, want 1", len(got), got)
	}
	if wc := wordCount(got[0]); wc > 5 {
		t.Errorf("chunk has %d words, want <= 5: %q", wc, got[0])
	}
	// The overflow stays held (3 words == minWords, so it is NOT merged
	// forward) and is drained by Flush.
	if rest := c.Flush(); !strings.Contains(rest, "wf0") || !strings.Contains(rest, "wg0") || !strings.Contains(rest, "wh0") {
		t.Errorf("Flush = %q, want the 3-word overflow (wf0 wg0 wh0)", rest)
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
	got := c.Feed(b.String())
	if len(got) != 1 || wordCount(got[0]) != 5 {
		t.Fatalf("got %d chunks %q, want 1 chunk of 5 words", len(got), got)
	}
	// The 45-word overflow stays held and is drained by Flush.
	rest := c.Flush()
	if wc := wordCount(rest); wc != 45 {
		t.Errorf("Flush has %d words, want 45: %q", wc, rest)
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
	got := c.Feed("New start. End.")
	// "New start." (2 words < minWords) is held and merges with "End.",
	// which then stands alone as one chunk.
	if len(got) != 1 || got[0] != "New start. End." {
		t.Fatalf("got %v", got)
	}
	if rest := c.Flush(); rest != "" {
		t.Errorf("Flush = %q, want empty", rest)
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
	tm.fire() // input stalled past MaxHold
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
	c := NewSentenceChunker(1, time.Second, 40, []string{"§"})
	got := c.Feed("Part one§Part two§")
	if len(got) != 2 || got[0] != "Part one§" || got[1] != "Part two§" {
		t.Fatalf("got %v", got)
	}
}
