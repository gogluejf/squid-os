package skills

import (
	"bytes"
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

// writeFrontmatter writes the YAML frontmatter with allowed-tools as a space-separated string.
func writeFrontmatter(buf *bytes.Buffer, params BuildParams) error {
	buf.WriteString("---\n")

	// writeYAMLValue writes a simple YAML value, quoting it if it contains
	// colons or other characters that YAML might misinterpret.
	writeVal := func(key, val string) {
		if strings.ContainsRune(val, ':') || strings.ContainsRune(val, '#') || strings.ContainsRune(val, '{') || strings.ContainsRune(val, '}') {
			escaped := strings.ReplaceAll(val, `"`, `\"`)
			buf.WriteString(fmt.Sprintf("%s: \"%s\"\n", key, escaped))
		} else {
			buf.WriteString(fmt.Sprintf("%s: %s\n", key, val))
		}
	}

	// Write simple fields
	if params.Name != "" {
		writeVal("name", params.Name)
	}
	if params.Description != "" {
		writeVal("description", params.Description)
	}
	if params.Version != "" {
		writeVal("version", params.Version)
	}
	if params.License != "" {
		writeVal("license", params.License)
	}

	// allowed-tools: write as space-separated string
	if params.AllowedTools != "" {
		writeVal("allowed-tools", params.AllowedTools)
	}

	// metadata: write as YAML mapping if non-empty
	if len(params.Metadata) > 0 {
		buf.WriteString("metadata:\n")
		for k, v := range params.Metadata {
			if strings.ContainsRune(v, ':') {
				escaped := strings.ReplaceAll(v, `"`, `\"`)
				buf.WriteString(fmt.Sprintf("  %s: \"%s\"\n", k, escaped))
			} else {
				buf.WriteString(fmt.Sprintf("  %s: %s\n", k, v))
			}
		}
	}

	buf.WriteString("---\n")
	return nil
}

// buildResourcesSection generates the ## Resources section with relative markdown links.
// Returns the section text (may be empty if no resources exist).
func buildResourcesSection(params BuildParams) string {
	var b strings.Builder
	var hasResources bool

	// Scripts
	if len(params.Scripts) > 0 {
		hasResources = true
		b.WriteString("### Scripts\n")
		for fname := range params.Scripts {
			b.WriteString(fmt.Sprintf("- [%s](scripts/%s) — Executable script\n", fname, fname))
		}
		b.WriteString("\n")
	}

	// References
	if len(params.References) > 0 {
		hasResources = true
		b.WriteString("### References\n")
		for fname := range params.References {
			b.WriteString(fmt.Sprintf("- [%s](references/%s) — Additional documentation\n", fname, fname))
		}
		b.WriteString("\n")
	}

	// Assets
	if len(params.Assets) > 0 {
		hasResources = true
		b.WriteString("### Assets\n")
		for fname := range params.Assets {
			b.WriteString(fmt.Sprintf("- [%s](assets/%s) — Template or resource file\n", fname, fname))
		}
		b.WriteString("\n")
	}

	if !hasResources {
		return ""
	}
	return "## Resources\n" + b.String()
}

// BuildSkillFile writes a SKILL.md file with the given parameters.
func BuildSkillFile(path string, params BuildParams) error {
	var buf bytes.Buffer

	// Frontmatter (with allowed-tools as space-separated string)
	if err := writeFrontmatter(&buf, params); err != nil {
		return err
	}

	// Body sections
	var sections []string
	if params.Overview != "" {
		sections = append(sections, "## Overview\n"+params.Overview)
	}
	if params.Instructions != "" {
		sections = append(sections, "## Instructions\n"+params.Instructions)
	}
	if params.Rules != "" {
		sections = append(sections, "## Rules\n"+params.Rules)
	}
	if params.OutputFormat != "" {
		sections = append(sections, "## Output Format\n```"+params.OutputFormat+"```")
	}
	if params.Examples != "" {
		sections = append(sections, "## Examples\n"+params.Examples)
	}

	// Resources section (with relative markdown links)
	if resources := buildResourcesSection(params); resources != "" {
		sections = append(sections, resources)
	}

	if len(sections) > 0 {
		buf.WriteString("\n")
		buf.WriteString(strings.Join(sections, "\n\n"))
	}

	return os.WriteFile(path, buf.Bytes(), 0644)
}
