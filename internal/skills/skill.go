package skills

import (
	"errors"
	"fmt"
	"regexp"
)

// Skill represents a loaded skill with its metadata and instructions.
type Skill struct {
	Name         string            // skill name (must match folder name)
	Description  string            // from frontmatter
	Version      string            // from frontmatter (optional)
	License      string            // from frontmatter (optional)
	AllowedTools string            // space-separated tool names (e.g. "bash read_file write_file")
	Metadata     map[string]string // from frontmatter (optional)

	// Content
	Body       string // full SKILL.md body (instructions) — frontmatter stripped
	Path       string // absolute path to SKILL.md
	ScriptsDir string // path to scripts/ (empty if none)
	AssetsDir  string // path to assets/ (empty if none)
	RefsDir    string // path to references/ (empty if none)
}

// Frontmatter is the parsed YAML header of SKILL.md
type Frontmatter struct {
	Name          string            `yaml:"name"`
	Description   string            `yaml:"description"`
	Version       string            `yaml:"version"`
	License       string            `yaml:"license"`
	Compatibility string            `yaml:"compatibility"`
	Metadata      map[string]string `yaml:"metadata"`
	AllowedTools  string            `yaml:"allowed-tools"`
}

// SkillEntry is the lightweight registry entry (frontmatter only)
type SkillEntry struct {
	Name        string
	Description string
	Path        string
}

// BuildParams holds all parameters for skill_build.
type BuildParams struct {
	Name         string
	Description  string
	Version      string
	License      string
	AllowedTools string // space-separated tool names (e.g. "bash read_file write_file")
	Metadata     map[string]string
	Overview     string
	Instructions string
	Rules        string
	OutputFormat string
	Examples     string
	References   map[string]string // filename -> content
	Assets       map[string]string // filename -> content
	Scripts      map[string]string // filename -> content
}

// nameRe enforces: 1-64 chars, lowercase letters/digits/hyphens, no leading/trailing/consecutive hyphens.
var nameRe = regexp.MustCompile(`^[a-z](?:[a-z0-9-]{0,62}[a-z0-9])?$`)

// ValidateName checks the skill name against the naming contract.
func ValidateName(name string) error {
	if name == "" {
		return errors.New("name is required")
	}
	if len(name) > 64 {
		return fmt.Errorf("name must be at most 64 characters, got %d", len(name))
	}
	if !nameRe.MatchString(name) {
		return errors.New("name must be lowercase letters, digits, and hyphens; no leading/trailing/consecutive hyphens")
	}
	return nil
}

// ValidateDescription checks the description is non-empty and within bounds.
func ValidateDescription(desc string) error {
	if desc == "" {
		return errors.New("description is required")
	}
	if len(desc) > 1024 {
		return fmt.Errorf("description must be at most 1024 characters, got %d", len(desc))
	}
	return nil
}
