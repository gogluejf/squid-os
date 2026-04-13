package config

import (
	"encoding/json"
	"os"
)

// LoadHistory loads history.json or returns empty
func LoadHistory(p Paths) History {
	h := History{}
	data, err := os.ReadFile(p.HistoryFile())
	if err != nil {
		return h
	}
	_ = json.Unmarshal(data, &h)
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
// Skips if identical to the most recent entry; does not deduplicate the full list.
func AddHistoryEntry(h *History, entry string, max int) {
	if len(h.Entries) > 0 && h.Entries[len(h.Entries)-1] == entry {
		return
	}
	h.Entries = append(h.Entries, entry)
	if len(h.Entries) > max {
		h.Entries = h.Entries[len(h.Entries)-max:]
	}
}
