package environment

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"squid-os/internal/git"
	"squid-os/internal/util"
)

// LoadProjectInfo builds ProjectInfo for a given working directory.
func LoadProjectInfo(workingDir, projectDir string) *ProjectInfo {
	info := &ProjectInfo{
		Path: workingDir,
	}

	if strings.HasPrefix(workingDir, projectDir) {
		info.IsUnderProjectDir = true
	}

	if git.HasGit(workingDir) || info.IsUnderProjectDir {
		info.FileTree = GenerateTree(workingDir, 3)
	}

	return info
}

// FormatProjectInfo renders ProjectInfo as a readable result string.
func FormatProjectInfo(info *ProjectInfo) string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("- working-dir: %s\n", util.FriendlyPath(git.Decorate(info.Path))))
	b.WriteString(fmt.Sprintf("- under-project-dir: %s\n", boolOrNot(info.IsUnderProjectDir)))
	if info.FileTree != "" {
		b.WriteString("- file-tree:\n")
		b.WriteString("```\n")
		b.WriteString(info.FileTree)
		b.WriteString("```\n")
	}
	return b.String()
}

// FindProjects scans the project directory for all subdirectories.
func FindProjects(projectDir string) []FolderEntry {
	var entries []FolderEntry
	if projectDir == "" {
		return entries
	}

	infos, err := os.ReadDir(projectDir)
	if err != nil {
		return entries
	}

	for _, info := range infos {
		if !info.IsDir() {
			continue
		}
		path := filepath.Join(projectDir, info.Name())
		entries = append(entries, FolderEntry{
			Name: info.Name(),
			Path: path,
		})
	}

	return entries
}


