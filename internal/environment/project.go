package environment

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// LoadProjectInfo builds ProjectInfo for a given working directory.
func LoadProjectInfo(workingDir, projectDir string) *ProjectInfo {
	info := &ProjectInfo{
		Path:      workingDir,
		IsGitRepo: hasGit(workingDir),
	}

	if strings.HasPrefix(workingDir, projectDir) {
		info.IsUnderProjectDir = true
	}

	if info.IsGitRepo || info.IsUnderProjectDir {
		info.FileTree = GenerateTree(workingDir, 3)
	}

	return info
}

// FormatProjectInfo renders ProjectInfo as a readable result string.
func FormatProjectInfo(info *ProjectInfo) string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("Path: %s\n", info.Path))
	b.WriteString(fmt.Sprintf("Git Init: %s\n", boolOrNot(info.IsGitRepo)))
	b.WriteString(fmt.Sprintf("Under Project Dir: %s\n", boolOrNot(info.IsUnderProjectDir)))
	if info.FileTree != "" {
		b.WriteString("File Tree:\n")
		b.WriteString("```\n")
		b.WriteString(info.FileTree)
		b.WriteString("```\n")
	}
	return b.String()
}

// FindProjects scans the project directory for git-initialized repos.
func FindProjects(projectDir string) []ProjectEntry {
	var entries []ProjectEntry
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
		if hasGit(path) {
			entries = append(entries, ProjectEntry{
				Name:  info.Name(),
				Path:  path,
				IsGit: true,
			})
		}
	}

	return entries
}

func hasGit(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, ".git"))
	return err == nil
}
