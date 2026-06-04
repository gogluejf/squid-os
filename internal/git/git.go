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

// Decorate returns the path decorated with git info — e.g. "/path (git)" if it's a repo,
// or the original path unchanged. Extend this later to show "+XX -XX" diff stats, branch name, etc.
func Decorate(dir string) string {
	if HasGit(dir) {
		return dir + " (git)"
	}
	return dir
}
