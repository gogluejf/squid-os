package app

import (
	"testing"

	"squid-os/internal/inputqueue"
	"squid-os/internal/localinput"
)

// TestLocalVoiceStopMsgCancelsActiveGroup verifies the socket voice_stop op's
// app-side handler runs the identical CancelGroup path as alt+x: an active
// group is killed and the reply reports "cancelled"; a second stop finds
// nothing and reports "nothing_playing".
func TestLocalVoiceStopMsgCancelsActiveGroup(t *testing.T) {
	m := newVoiceTestModel(t, true)
	m.narrator.beginMessage(true)
	m.narrator.feedDelta("Something is being spoken out loud. ")
	gid := m.narrator.coord.Current()
	if gid == 0 {
		t.Fatal("no active group after feed")
	}

	reply := make(chan string, 1)
	updated, _ := m.Update(localVoiceStopMsg{reply: reply})
	m2 := updated.(Model)

	select {
	case status := <-reply:
		if status != "cancelled" {
			t.Fatalf("reply = %q, want cancelled", status)
		}
	default:
		t.Fatal("handler did not reply")
	}
	if got := m2.narrator.coord.Current(); got != 0 {
		t.Fatalf("group %d still current after voice_stop", got)
	}

	// Second stop: explicit no-op, reply nothing_playing.
	reply2 := make(chan string, 1)
	updated2, _ := m2.Update(localVoiceStopMsg{reply: reply2})
	m3 := updated2.(Model)
	select {
	case status := <-reply2:
		if status != "nothing_playing" {
			t.Fatalf("second stop reply = %q, want nothing_playing", status)
		}
	default:
		t.Fatal("handler did not reply")
	}
	_ = m3
}

// TestLocalAdmissionDuplicateReceiptDoesNotCancelSpeech (finding 3): a client
// retrying an already-admitted request gets a duplicate receipt; that is NOT
// a new user message and must NOT mute the in-progress response (task 2.2).
func TestLocalAdmissionDuplicateReceiptDoesNotCancelSpeech(t *testing.T) {
	m := newVoiceTestModel(t, true)
	live := m.liveSessionID()

	req := inputqueue.AdmissionRequest{
		RequestID: "aaaaaaaa-1111-2222-3333-bbbbbbbbbbbb",
		SessionID: live,
		Source:    "local",
		Text:      "a brand new instruction",
		Boundary:  inputqueue.BoundaryStep,
	}

	// First admission: a NEW message — cancels any active speech group.
	reply1 := make(chan localinput.ReceiptInfo, 1)
	updated1, _ := m.Update(localAdmissionMsg{req: req, reply: reply1})
	res1 := <-reply1
	if !res1.OK || res1.Duplicate {
		t.Fatalf("first admission = %+v, want OK non-duplicate", res1)
	}

	// A fresh group starts playing the (future) response.
	m1 := updated1.(Model)
	m1.narrator.beginMessage(true)
	m1.narrator.feedDelta("Response now being spoken aloud. ")
	gid := m1.narrator.coord.Current()
	if gid == 0 {
		t.Fatal("no active group after begin+feed")
	}

	// Client retries the SAME RequestID while the group plays: the receipt is
	// a duplicate, so the handler must leave the group untouched.
	reply2 := make(chan localinput.ReceiptInfo, 1)
	updated2, _ := m1.Update(localAdmissionMsg{req: req, reply: reply2})
	res2 := <-reply2
	if !res2.OK || !res2.Duplicate {
		t.Fatalf("second admission = %+v, want OK duplicate", res2)
	}
	m2 := updated2.(Model)
	if got := m2.narrator.coord.Current(); got != gid {
		t.Fatalf("duplicate admission cancelled the playing group: Current() = %d, want %d", got, gid)
	}
}
