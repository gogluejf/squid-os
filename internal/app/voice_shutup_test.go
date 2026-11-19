package app

import (
	"context"
	"testing"
	"time"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
)

// keyAltX builds the alt+x key message as Bubble Tea delivers it.
func keyAltX() tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}, Alt: true} }

// TestShutUpBindingIsAltX verifies the binding resolves to alt+x and is not
// claimed by any slash command shortcut (matchCommandKey must stay empty).
func TestShutUpBindingIsAltX(t *testing.T) {
	msg := keyAltX()
	if !key.Matches(msg, keys.ShutUp) {
		t.Fatal("alt+x does not match keys.ShutUp")
	}
	if name := matchCommandKey(msg); name != "" {
		t.Fatalf("alt+x claimed by command %q", name)
	}
}

// TestShutUpMutesRestOfCurrentMessage: alt+x during narration kills the
// running chunk, drops buffered chunks, and mutes every phrase not yet
// emitted — one press silences the whole group. The next message speaks
// normally without re-enabling anything.
func TestShutUpMutesRestOfCurrentMessage(t *testing.T) {
	m := newVoiceTestModel(t, true)
	m.narrator.beginMessage(true)
	m.narrator.feedDelta("First sentence of the reply. ")
	gid := m.narrator.coord.Current()
	if gid == 0 {
		t.Fatal("no active group after first feed")
	}

	updated, _ := m.handleKey(keyAltX())
	m2 := updated.(Model)

	if got := m2.narrator.coord.Current(); got != 0 {
		t.Fatalf("group %d still current after alt+x", got)
	}
	// Phrases not yet emitted at the moment of the press must be dropped.
	m2.narrator.emit(gid, "this phrase was never emitted.")
	if got := m2.narrator.coord.Current(); got != 0 {
		t.Fatalf("unemitted phrase resurrected group %d", got)
	}
	// EndGroup on the muted group is a no-op; must not panic or speak.
	m2.narrator.coord.EndGroup(gid)

	// Not sticky: the NEXT assistant message speaks normally.
	m2.narrator.beginMessage(true)
	m2.narrator.feedDelta("A fresh message speaks fine. ")
	next := m2.narrator.coord.Current()
	if next == 0 || next == gid {
		t.Fatalf("next message did not start a fresh speaking group (got %d, old %d)", next, gid)
	}
	m2.narrator.endMessage(context.Background())
	deadline := time.Now().Add(2 * time.Second)
	for m2.narrator.coord.Current() != 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if got := m2.narrator.coord.Current(); got != 0 {
		t.Fatalf("next group did not drain: %d", got)
	}
}

// TestShutUpSilencesWholeMultiChunkGroup: with several chunks queued
// back-to-back (an error lead-in plus its detail), one alt+x press kills the
// playing chunk AND the buffered ones — the whole group dies in one frame.
func TestShutUpSilencesWholeMultiChunkGroup(t *testing.T) {
	m := newVoiceTestModel(t, true)
	m.narrator.beginMessage(true)
	// Lead-in chunk starts playing; the detail chunk queues behind it.
	m.narrator.feedDelta("Something went wrong. ")
	m.narrator.feedDelta("Here is the full detail of what failed. ")
	gid := m.narrator.coord.Current()
	if gid == 0 {
		t.Fatal("no active group before alt+x")
	}

	updated, _ := m.handleKey(keyAltX())
	m2 := updated.(Model)
	if got := m2.narrator.coord.Current(); got != 0 {
		t.Fatalf("group %d survived one alt+x press (buffered chunk leaked?)", got)
	}
	// Any further late chunk for the same group stays dead.
	m2.narrator.emit(gid, "yet another late chunk.")
	if got := m2.narrator.coord.Current(); got != 0 {
		t.Fatalf("late chunk resurrected group %d", got)
	}
}

// TestShutUpWorksUnderOverlays: alt+x cancels the group while the help
// overlay or an authorization component overlay is open — the handler runs
// before mode dispatch, so overlays cannot swallow the key.
func TestShutUpWorksUnderOverlays(t *testing.T) {
	// Help overlay.
	m := newVoiceTestModel(t, true)
	m.mode = ModeHelp
	m.narrator.beginMessage(true)
	m.narrator.feedDelta("Speaking under the help overlay. ")
	if m.narrator.coord.Current() == 0 {
		t.Fatal("no active group under help overlay")
	}
	updated, _ := m.handleKey(keyAltX())
	m2 := updated.(Model)
	if got := m2.narrator.coord.Current(); got != 0 {
		t.Fatalf("help overlay swallowed alt+x: group %d alive", got)
	}
	// The overlay itself must remain open (shut-up is not a dismiss key).
	if m2.mode != ModeHelp {
		t.Fatalf("alt+x changed mode to %v, want ModeHelp", m2.mode)
	}

	// Authorization/component overlay.
	m3 := newVoiceTestModel(t, true)
	m3.mode = ModeComponent
	m3.narrator.beginMessage(true)
	m3.narrator.feedDelta("Speaking under the auth overlay. ")
	if m3.narrator.coord.Current() == 0 {
		t.Fatal("no active group under component overlay")
	}
	updated, _ = m3.handleKey(keyAltX())
	m4 := updated.(Model)
	if got := m4.narrator.coord.Current(); got != 0 {
		t.Fatalf("component overlay swallowed alt+x: group %d alive", got)
	}
	if m4.mode != ModeComponent {
		t.Fatalf("alt+x changed mode to %v, want ModeComponent", m4.mode)
	}
}

// TestShutUpNoSpeechIsNoOp: pressing alt+x with nothing playing must not
// panic and must leave the model usable — subsequent messages speak.
func TestShutUpNoSpeechIsNoOp(t *testing.T) {
	m := newVoiceTestModel(t, true)
	updated, _ := m.handleKey(keyAltX())
	m2 := updated.(Model)
	if got := m2.narrator.coord.Current(); got != 0 {
		t.Fatalf("no speech but group %d active", got)
	}

	m2.narrator.beginMessage(true)
	m2.narrator.feedDelta("Still speaks after a no-op shut-up. ")
	if got := m2.narrator.coord.Current(); got == 0 {
		t.Fatal("speech broken after no-op alt+x")
	}
}
