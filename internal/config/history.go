package config

import (
	"encoding/json"
	"os"
	"time"
)

type History struct {
	Entries []string `json:"entries"`
}

// LoadHistory loads history.json or returns empty.
// If the file exists but is malformed, it backs it up with a .corrupted.TIMESTAMP suffix.
func LoadHistory(p Paths) History {
	h := History{}
	data, err := os.ReadFile(p.HistoryFile())
	if err != nil {
		return h
	}
	if err := json.Unmarshal(data, &h); err != nil {
		// Corrupted file — back it up before we overwrite it on next save
		ts := time.Now().Format("20060102-150405")
		backup := p.HistoryFile() + ".corrupted." + ts
		_ = os.Rename(p.HistoryFile(), backup)
		return h
	}
	return h
}

// SaveHistory writes history.json
func SaveHistory(p Paths, h History) error {
	data, err := json.MarshalIndent(h, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(p.HistoryFile(), data, 0644)
}

// AddHistoryEntry adds an entry to the LRU history.
func AddHistoryEntry(h *History, entry string, max int) {
	// Skips if identical to the most recent entry; does not deduplicate the full list.
	if len(h.Entries) > 0 && h.Entries[len(h.Entries)-1] == entry {
		return
	}
	h.Entries = append(h.Entries, entry)
	if len(h.Entries) > max {
		h.Entries = h.Entries[len(h.Entries)-max:]
	}
}

// RemoveHistoryEntry removes the last entry from history.
func RemoveHistoryEntry(h *History) {
	if len(h.Entries) > 0 {
		h.Entries = h.Entries[:len(h.Entries)-1]
	}
}
