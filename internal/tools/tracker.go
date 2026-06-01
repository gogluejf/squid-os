package tools

import (
	"fmt"
	"os"

	"squid-os/internal/config"
	"squid-os/internal/util"
)

// Validate checks a path against the session file state map.
// Returns nil if the path is not tracked, or if the on-disk checksum matches.
// Returns an error if the file on disk differs from the last recorded value,
// or if the file was previously marked as deleted.
func Validate(path string, sessionState map[string]config.FileStateEntry) error {
	stored, ok := sessionState[path]
	if !ok {
		return nil // never tracked
	}
	if stored.Checksum == "" {
		return fmt.Errorf("file was deleted during this session: %s", path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("cannot read %s for validation: %w", path, err)
	}
	current := util.ComputeChecksum(data)
	if current != stored.Checksum {
		return fmt.Errorf("file on disk changed: %s", path)
	}
	return nil
}

// MergeEntries merges FileEntry results into a file state map.
// A non-empty checksum sets the entry; an empty checksum marks it as deleted.
func MergeEntries(entries []config.FileEntry, state map[string]config.FileStateEntry) {
	if state == nil {
		return
	}
	for _, e := range entries {
		state[e.Path] = config.FileStateEntry{
			Checksum:  e.Checksum,
			Trace:     e.Trace,
			UpdatedAt: e.Time,
		}
	}
}
