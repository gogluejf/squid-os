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
			Description: fm.Description,
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
		return nil, fmt.Errorf("read skill %q: %w", name, err)
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

// Build creates a new skill with the given parameters.
func (r *Registry) Build(params BuildParams) (*Skill, error) {
	if err := ValidateName(params.Name); err != nil {
		return nil, err
	}
	if err := ValidateDescription(params.Description); err != nil {
		return nil, err
	}

	// Check for duplicates
	if _, exists := r.index[params.Name]; exists {
		return nil, fmt.Errorf("skill %q already exists", params.Name)
	}

	// Create folder
	skillDir := filepath.Join(r.baseDir, params.Name)
	if err := os.MkdirAll(skillDir, 0755); err != nil {
		return nil, fmt.Errorf("create skill directory: %w", err)
	}

	// Write SKILL.md
	skillPath := filepath.Join(skillDir, "SKILL.md")
	if err := BuildSkillFile(skillPath, params); err != nil {
		return nil, fmt.Errorf("write SKILL.md: %w", err)
	}

	// Write scripts if any
	if len(params.Scripts) > 0 {
		scriptsDir := filepath.Join(skillDir, "scripts")
		if err := os.MkdirAll(scriptsDir, 0755); err != nil {
			return nil, fmt.Errorf("create scripts directory: %w", err)
		}
		for fname, content := range params.Scripts {
			if err := os.WriteFile(filepath.Join(scriptsDir, fname), []byte(content), 0755); err != nil {
				return nil, fmt.Errorf("write script %s: %w", fname, err)
			}
		}
	}

	// Write references if any
	if len(params.References) > 0 {
		refsDir := filepath.Join(skillDir, "references")
		if err := os.MkdirAll(refsDir, 0755); err != nil {
			return nil, fmt.Errorf("create references directory: %w", err)
		}
		for fname, content := range params.References {
			if err := os.WriteFile(filepath.Join(refsDir, fname), []byte(content), 0644); err != nil {
				return nil, fmt.Errorf("write reference %s: %w", fname, err)
			}
		}
	}

	// Write assets if any
	if len(params.Assets) > 0 {
		assetsDir := filepath.Join(skillDir, "assets")
		if err := os.MkdirAll(assetsDir, 0755); err != nil {
			return nil, fmt.Errorf("create assets directory: %w", err)
		}
		for fname, content := range params.Assets {
			if err := os.WriteFile(filepath.Join(assetsDir, fname), []byte(content), 0644); err != nil {
				return nil, fmt.Errorf("write asset %s: %w", fname, err)
			}
		}
	}

	// Rebuild index
	if err := r.Rescan(); err != nil {
		return nil, err
	}

	return r.Load(params.Name)
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
