package skills

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"squid-os/internal/config"
)

// Registry is the effective skill catalog. Names are unique; workspace entries
// replace global entries with the same name.
type Registry struct {
	entries []SkillEntry
	index   map[string]SkillEntry
}

// LoadRegistry builds one effective catalog from global then workspace roots.
func LoadRegistry(globalDir, workspaceDir string) (*Registry, error) {
	r := &Registry{index: make(map[string]SkillEntry)}
	if globalDir != "" {
		if err := os.MkdirAll(globalDir, 0755); err != nil {
			return nil, fmt.Errorf("create global skills directory: %w", err)
		}
		if err := scan(r, globalDir, config.CapabilityScopeGlobal); err != nil {
			return nil, fmt.Errorf("scan global skills: %w", err)
		}
	}
	if workspaceDir != "" {
		if err := scan(r, workspaceDir, config.CapabilityScopeWorkspace); err != nil {
			return nil, fmt.Errorf("scan workspace skills: %w", err)
		}
	}
	rebuildList(r)
	return r, nil
}

func scan(r *Registry, baseDir string, scope config.CapabilityScope) error {
	directories, err := os.ReadDir(baseDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	for _, directory := range directories {
		if !directory.IsDir() {
			continue
		}
		path := filepath.Join(baseDir, directory.Name(), "SKILL.md")
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		fm, _, err := extractFrontmatter(data)
		if err != nil {
			continue
		}
		name := directory.Name()
		if fm.Name != "" {
			name = fm.Name
		}
		r.index[name] = SkillEntry{
			Scope:       scope,
			Name:        name,
			Description: strings.TrimSpace(fm.Description),
			Path:        path,
		}
	}
	return nil
}

func rebuildList(r *Registry) {
	r.entries = make([]SkillEntry, 0, len(r.index))
	for _, entry := range r.index {
		r.entries = append(r.entries, entry)
	}
	sort.Slice(r.entries, func(i, j int) bool { return r.entries[i].Name < r.entries[j].Name })
}

func (r *Registry) Resolve(name string) (SkillEntry, bool) {
	if r == nil {
		return SkillEntry{}, false
	}
	entry, ok := r.index[name]
	return entry, ok
}

func (r *Registry) Load(name string) (*Skill, error) {
	entry, ok := r.Resolve(name)
	if !ok {
		return nil, fmt.Errorf("skill %q not found", name)
	}
	return r.load(entry)
}

// LoadScoped validates the session's resolved scope before loading.
func (r *Registry) LoadScoped(scope config.CapabilityScope, name string) (*Skill, error) {
	entry, ok := r.Resolve(name)
	if !ok || entry.Scope != scope {
		return nil, fmt.Errorf("skill %q [%s] not found", name, scope)
	}
	return r.load(entry)
}

func (r *Registry) load(entry SkillEntry) (*Skill, error) {
	data, err := os.ReadFile(entry.Path)
	if err != nil {
		return nil, fmt.Errorf("read skill %q: %w", entry.Name, err)
	}
	return ParseSkillFile(entry.Path, data)
}

func (r *Registry) List() []SkillEntry {
	if r == nil {
		return nil
	}
	return append([]SkillEntry(nil), r.entries...)
}

func FormatSkillRegistry(entries []SkillEntry) string {
	if len(entries) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("Available skills (use skill_load to activate):\n")
	for _, entry := range entries {
		b.WriteString(fmt.Sprintf("  - %s: %s [%s]\n", entry.Name, entry.Description, entry.Scope))
	}
	return b.String()
}
