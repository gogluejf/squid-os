package app

import (
	"context"

	"squid-os/internal/log"
)

// speakAlert speaks one preformatted alert as its OWN speech group — distinct
// from the narration group, so an alert landing while a response tail is still
// playing bumps it (default onPreempt), and alt+x / barge-in / voice stop kill
// it exactly like narration (same CancelGroup path).
//
// voiceOn is passed by the caller (speakAlertEvent reads Model.settings.Voice.Enabled on the
// UI goroutine) — no indirection, no stale captures: production and tests
// behave identically by construction.
//
// Recursion guard: while ANY alert group is in flight, new alerts are dropped
// silently — a TTS failure must never spawn a spoken error about itself.
// The flag clears when the last alert group stops being current (coordinator
// drain hook). Hooks are asynchronous, so they can safely acquire n.mu.
func (n *Narrator) speakAlert(voiceOn bool, text, lang string) {
	if n == nil || n.coord == nil || text == "" || !voiceOn {
		return
	}
	n.mu.Lock()
	if n.alertActive {
		n.mu.Unlock()
		log.LogVoice("alert.skipped", "reason=active_alert language=%q", lang)
		return // recursion guard: an alert is already playing
	}
	n.alertActive = true
	gid := n.coord.StartGroup()
	n.alertGids[gid] = struct{}{}
	n.mu.Unlock()

	log.LogVoice("alert.group", "group=%d language=%q text=%q", gid, lang, text)
	n.coord.EmitAlert(context.Background(), gid, text, lang)
	n.coord.EndGroup(gid)
}

// onAlertDrained is the coordinator's per-group drain callback for alert
// groups: when an alert group stops being current (played out or killed),
// forget it; when the LAST one goes, clear the recursion-guard flag.
func (n *Narrator) onAlertDrained(gid int) {
	if n == nil {
		return
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	if _, ok := n.alertGids[gid]; !ok {
		return
	}
	delete(n.alertGids, gid)
	if len(n.alertGids) == 0 {
		n.alertActive = false
	}
}
