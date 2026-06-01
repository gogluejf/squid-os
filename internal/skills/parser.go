package skills

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// ParseSkillFile reads SKILL.md and extracts frontmatter + body, resolving
// sibling directory paths (scripts/, assets/, references/).
func ParseSkillFile(path string, data []byte) (*Skill, error) {
	fm, body, err := extractFrontmatter(data)
	if err != nil {
		return nil, fmt.Errorf("parse frontmatter in %s: %w", path, err)
	}

	// Derive dirs from the SKILL.md location
	skillDir := filepath.Dir(path)

	sk := &Skill{
		Name:         fm.Name,
		Description:  fm.Description,
		Version:      fm.Version,
		License:      fm.License,
		AllowedTools: fm.AllowedTools,
		Metadata:     fm.Metadata,
		Body:         body,
		Path:         path,
	}

	// Check for optional subdirs
	if d := filepath.Join(skillDir, "scripts"); dirExists(d) {
		sk.ScriptsDir = d
	}
	if d := filepath.Join(skillDir, "assets"); dirExists(d) {
		sk.AssetsDir = d
	}
	if d := filepath.Join(skillDir, "references"); dirExists(d) {
		sk.RefsDir = d
	}

	return sk, nil
}

// extractFrontmatter splits YAML frontmatter (--- ... ---) from the body.
func extractFrontmatter(data []byte) (*Frontmatter, string, error) {
	s := string(data)

	// Must start with ---
	if !strings.HasPrefix(s, "---") {
		return nil, "", fmt.Errorf("SKILL.md must start with YAML frontmatter (---)")
	}

	// Find the closing ---
	idx := strings.Index(s[3:], "---")
	if idx == -1 {
		return nil, "", fmt.Errorf("SKILL.md frontmatter missing closing ---")
	}

	yamlEnd := 3 + idx
	fmBytes := []byte(s[3:yamlEnd])
	body := strings.TrimSpace(s[yamlEnd+3:])

	var fm Frontmatter
	if err := yaml.Unmarshal(fmBytes, &fm); err != nil {
		return nil, "", fmt.Errorf("invalid YAML frontmatter: %w", err)
	}

	return &fm, body, nil
}

func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}
