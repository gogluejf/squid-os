package ui

// BlockRange describes a clickable block relative to its rendered message.
type BlockRange struct {
	Key   string
	Start int
	End   int
}

// ExpandTracker decides whether a block is expanded. Per-block overrides (set
// by clicking) win over the global default (the e key). Block keys are stable
// per message: "thinking", "text", "tool:<id>", or the message ID for
// single-block messages.
type ExpandTracker struct {
	Global bool
	Open   map[string]bool
}

func NewExpandTracker(global bool) *ExpandTracker {
	return &ExpandTracker{Global: global}
}

func (t *ExpandTracker) IsOpen(msgID, block string) bool {
	if t.Open != nil {
		if v, ok := t.Open[msgID+"\x00"+block]; ok {
			return v
		}
	}
	return t.Global
}

func (t *ExpandTracker) Toggle(msgID, block string) {
	if t.Open == nil {
		t.Open = make(map[string]bool)
	}
	key := msgID + "\x00" + block
	t.Open[key] = !t.IsOpen(msgID, block)
}

// Clear drops all per-block overrides; blocks fall back to the global default.
func (t *ExpandTracker) Clear() {
	t.Open = nil
}
