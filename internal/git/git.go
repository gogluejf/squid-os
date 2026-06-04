package git

import (
	"os"
	"path/filepath"
)

// HasGit returns true if the given directory contains a .git folder.
func HasGit(dir string) bool {
	if dir == "" {
		return false
	}
	_, err := os.Stat(filepath.Join(dir, ".git"))
	return err == nil
}

// Label returns a display suffix for a directory — "(git)" if it's a repo, empty otherwise.
// Extend this later to show "+XX -XX" diff stats, branch name, etc.
func Label(dir string) string {
	if HasGit(dir) {
		return " (git)"
	}
	return ""
}
