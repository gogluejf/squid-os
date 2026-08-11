package tools

import (
	"fmt"
	"os"
	"time"

	"squid-os/internal/config"
	"squid-os/internal/diff"
	"squid-os/internal/util"
)

// BuildFileEntry constructs a FileEntry from old and new file data.
func BuildFileEntry(path string, trace string, oldData, newData []byte) config.FileEntry {
	var checksum string
	var diffText string

	if newData != nil {
		checksum = util.ComputeChecksum(newData)
		if oldData != nil {
			diffText = diff.Diff(string(oldData), string(newData))
		} else {
			diffText = diff.Diff("", string(newData))
		}
	} else if oldData != nil {
		checksum = util.ComputeChecksum(oldData)
	}

	return config.FileEntry{
		Path:     path,
		Trace:    trace,
		Checksum: checksum,
		Time:     time.Now(),
		Diff:     diffText,
	}
}

// ValidateFileState checks a path against the session file state map.
func ValidateFileState(path string, sessionState map[string]config.FileStateEntry) error {
	stored, ok := sessionState[path]
	if !ok {
		return nil
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
func MergeEntries(entries []config.FileEntry, state map[string]config.FileStateEntry) {
	if state == nil {
		return
	}
	for _, e := range entries {
		state[e.Path] = config.FileStateEntry{
			Checksum:   e.Checksum,
			Trace:      e.Trace,
			ToolCallID: e.ToolCallID,
			UpdatedAt:  e.Time,
		}
	}
}
