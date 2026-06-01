package skills

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Registry holds scanned skill entries indexed by name.
type Registry struct {
	baseDir string
	entries []SkillEntry
	index   map[string]*SkillEntry
}

// Global skill registry instance.
var reg *Registry

// NewRegistry creates a new registry rooted at baseDir.
func NewRegistry(baseDir string) *Registry {
	r := &Registry{
		baseDir: baseDir,
		index:   make(map[string]*SkillEntry),
	}
	return r
}

// GetRegistry returns the global skill registry.
func GetRegistry() *Registry {
	return reg
}

// InitRegistry creates the skills directory if needed, scans, and sets the global instance.
func InitRegistry(baseDir string) error {
	if err := os.MkdirAll(baseDir, 0755); err != nil {
		return fmt.Errorf("create skills directory: %w", err)
	}
	r := NewRegistry(baseDir)
	if err := r.Rescan(); err != nil {
		return fmt.Errorf("rescan skills: %w", err)
	}
	reg = r
	return nil
}

// Rescan the base directory for skill folders containing SKILL.md.
func (r *Registry) Rescan() error {
	r.entries = nil
	r.index = make(map[string]*SkillEntry)

	entries, err := os.ReadDir(r.baseDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // directory doesn't exist yet, that's fine
		}
		return fmt.Errorf("read skills directory: %w", err)
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		skillPath := filepath.Join(r.baseDir, entry.Name(), "SKILL.md")
		data, err := os.ReadFile(skillPath)
		if err != nil {
			continue // not a valid skill folder, skip
		}

		fm, _, err := extractFrontmatter(data)
		if err != nil {
			continue // invalid frontmatter, skip
		}

		// Cross-check: folder name should match frontmatter name
		name := entry.Name()
		if fm.Name != "" {
			name = fm.Name
		}

		entryItem := SkillEntry{
			Name:        name,
			Description: strings.TrimSpace(fm.Description),
			Path:        skillPath,
		}
		r.entries = append(r.entries, entryItem)
		r.index[entryItem.Name] = &r.entries[len(r.entries)-1]
	}

	return nil
}

// Load the full SKILL.md content for the named skill.
func (r *Registry) Load(name string) (*Skill, error) {
	if r == nil {
		return nil, fmt.Errorf("skill registry not initialized")
	}
	entry, ok := r.index[name]
	if !ok {
		return nil, fmt.Errorf("skill %q not found", name)
	}

	data, err := os.ReadFile(entry.Path)
	if err != nil {
		return nil, fmt.Errorf("read skill %q: %w", err)
	}

	return ParseSkillFile(entry.Path, data)
}

// List returns all registered skill entries.
func (r *Registry) List() []SkillEntry {
	if r == nil {
		return nil
	}
	cp := make([]SkillEntry, len(r.entries))
	copy(cp, r.entries)
	return cp
}

// FormatSkillRegistry returns a compact text representation of the skill registry
// for injection into the system prompt context.
func FormatSkillRegistry(entries []SkillEntry) string {
	if len(entries) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("Available skills (use skill_load to activate):\n")
	for _, e := range entries {
		b.WriteString(fmt.Sprintf("  - %s: %s\n", e.Name, e.Description))
	}
	return b.String()
}
