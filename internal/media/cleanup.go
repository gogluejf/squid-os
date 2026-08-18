package media

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
)

const (
	// IncognitoPrefix is the directory name prefix for incognito session workspaces.
	IncognitoPrefix = "squid-incognito-"
	// SessionPrefix is the directory name prefix for normal unsaved session workspaces.
	SessionPrefix = "squid-session-"
)

// CleanupPolicy configures bounded cleanup of stale temporary workspaces.
type CleanupPolicy struct {
	// Root is the parent directory containing temporary workspaces (e.g., ~/tmp).
	Root string
	// OlderThan specifies the minimum age for a workspace to be considered stale.
	// Only directories modified more than OlderThan ago will be removed.
	OlderThan time.Duration
	// MaxEntries is the upper bound on the number of directories removed in a
	// single cleanup run. This prevents runaway deletion in case of misconfiguration.
	MaxEntries int
}

// CleanupStale removes stale Squid-owned temporary workspaces from the given
// root directory. It only removes directories whose names match the known
// Squid prefixes (IncognitoPrefix or SessionPrefix) and whose modification
// time is older than the configured threshold.
//
// The operation is bounded: it processes at most MaxEntries directories and
// stops immediately if any unexpected error occurs reading the root.
func CleanupStale(policy CleanupPolicy) error {
	if policy.Root == "" {
		return fmt.Errorf("cleanup root is empty")
	}
	if policy.OlderThan <= 0 {
		return fmt.Errorf("cleanup OlderThan must be positive")
	}
	if policy.MaxEntries <= 0 {
		return fmt.Errorf("cleanup MaxEntries must be positive")
	}

	entries, err := os.ReadDir(policy.Root)
	if err != nil {
		return fmt.Errorf("read cleanup root %s: %w", policy.Root, err)
	}

	cutoff := time.Now().Add(-policy.OlderThan)
	removed := 0

	for _, entry := range entries {
		if removed >= policy.MaxEntries {
			break
		}

		if !entry.IsDir() {
			continue
		}

		name := entry.Name()
		if !strings.HasPrefix(name, IncognitoPrefix) && !strings.HasPrefix(name, SessionPrefix) {
			continue
		}

		info, err := entry.Info()
		if err != nil {
			// Skip entries we can't stat — don't abort the whole cleanup.
			continue
		}

		if !info.ModTime().Before(cutoff) {
			continue
		}

		dirPath := filepath.Join(policy.Root, name)
		if err := os.RemoveAll(dirPath); err != nil {
			// Log but continue — we want to remove as many as possible.
			continue
		}
		removed++
	}

	return nil
}

// IncognitoWorkspaceDir creates a new isolated temporary directory for an
// incognito session workspace under the given root. The directory name uses
// the IncognitoPrefix so it can be identified and cleaned up later.
//
// Returns the absolute path to the created directory.
func IncognitoWorkspaceDir(root string) (string, error) {
	id := uuid.New().String()[:8]
	pattern := IncognitoPrefix + id + "-" + uuid.New().String()[:4]
	dir, err := os.MkdirTemp(root, pattern)
	if err != nil {
		return "", fmt.Errorf("create incognito workspace: %w", err)
	}
	return dir, nil
}

// RemoveWorkspace removes the entire workspace directory.
// It is a no-op if the directory no longer exists.
func RemoveWorkspace(dir string) error {
	if dir == "" {
		return nil
	}
	return os.RemoveAll(dir)
}

// IsSquidTempDir returns true if the directory name matches a known Squid
// temporary workspace prefix.
func IsSquidTempDir(name string) bool {
	return strings.HasPrefix(name, IncognitoPrefix) || strings.HasPrefix(name, SessionPrefix)
}

// IsIncognitoDir returns true if the directory name matches the incognito prefix.
func IsIncognitoDir(name string) bool {
	return strings.HasPrefix(name, IncognitoPrefix)
}
