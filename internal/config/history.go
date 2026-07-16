package config

import (
	"encoding/json"
	"os"
	"time"
)

type HistoryEntry struct {
	Text      string    `json:"text"`
	CreatedAt time.Time `json:"created_at"`
}

type History struct {
	Entries []HistoryEntry `json:"entries"`
}

// LoadHistory loads history.json or returns empty.
// It supports both the current timestamped format and the legacy []string format.
// If the file exists but is malformed, it backs it up with a .corrupted.TIMESTAMP suffix.
func LoadHistory(p Paths) History {
	data, err := os.ReadFile(p.HistoryFile())
	if err != nil {
		return History{}
	}

	var h History
	if err := json.Unmarshal(data, &h); err == nil {
		return h
	}

	var legacy struct {
		Entries []string `json:"entries"`
	}
	if err := json.Unmarshal(data, &legacy); err == nil {
		createdAt := time.Now().UTC()
		if info, statErr := os.Stat(p.HistoryFile()); statErr == nil {
			createdAt = info.ModTime().UTC()
		}
		h.Entries = make([]HistoryEntry, 0, len(legacy.Entries))
		for _, entry := range legacy.Entries {
			h.Entries = append(h.Entries, HistoryEntry{Text: entry, CreatedAt: createdAt})
		}
		return h
	}

	// Corrupted file — back it up before we overwrite it on next save.
	ts := time.Now().Format("20060102-150405")
	backup := p.HistoryFile() + ".corrupted." + ts
	_ = os.Rename(p.HistoryFile(), backup)
	return History{}
}

// SaveHistory writes history.json.
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
	if len(h.Entries) > 0 && h.Entries[len(h.Entries)-1].Text == entry {
		return
	}
	h.Entries = append(h.Entries, HistoryEntry{Text: entry, CreatedAt: time.Now().UTC()})
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
