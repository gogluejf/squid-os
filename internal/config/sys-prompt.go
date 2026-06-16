package config

import (
	"os"
	"path/filepath"
	"strings"
)

// LoadSystemPrompt reads a system prompt file from the prompts directory.
// If name is empty or file not found, returns the default prompt file.
func LoadSystemPrompt(p Paths, name string) string {
	if name == "" {
		name = "default.md"
	}

	// Try as-is, then with .md, then with .txt
	candidates := []string{name}
	if !strings.HasSuffix(name, ".md") && !strings.HasSuffix(name, ".txt") {
		candidates = append(candidates, name+".md", name+".txt")
	}

	for _, c := range candidates {
		path := filepath.Join(p.SysPrompts, c)
		data, err := os.ReadFile(path)
		if err == nil {
			return strings.TrimSpace(string(data))
		}
	}

	return ""
}

// ListSystemPrompts returns available system prompt files
func ListSystemPrompts(p Paths) []string {
	entries, err := os.ReadDir(p.SysPrompts)
	if err != nil {
		return nil
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() {
			ext := strings.ToLower(filepath.Ext(e.Name()))
			if ext == ".md" || ext == ".txt" {
				names = append(names, e.Name())
			}
		}
	}
	return names
}
