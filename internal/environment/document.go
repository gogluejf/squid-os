package environment

import (
	"os"
	"path/filepath"
)

// FindDocuments scans the documents directory for subdirectories, checking for git.
func FindDocuments(documentsDir string) []FolderEntry {
	var entries []FolderEntry
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
		entries = append(entries, FolderEntry{
			Name: info.Name(),
			Path: path,
		})
	}

	return entries
}
