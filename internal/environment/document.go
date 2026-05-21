package environment

import (
	"os"
	"path/filepath"
)

// FindDocuments scans the documents directory for subdirectories, checking for git.
func FindDocuments(documentsDir string) []DocEntry {
	var entries []DocEntry
	if documentsDir == "" {
		return entries
	}

	infos, err := os.ReadDir(documentsDir)
	if err != nil {
		return entries
	}

	for _, info := range infos {
		if !info.IsDir() {
			continue
		}
		path := filepath.Join(documentsDir, info.Name())
		entries = append(entries, DocEntry{
			Name:  info.Name(),
			Path:  path,
			IsGit: hasGit(path),
		})
	}

	return entries
}
