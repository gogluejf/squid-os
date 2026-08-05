package agent

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"squid-os/internal/config"
)

// Registry is the effective agent catalog. Names are unique; workspace entries
// replace global entries with the same name.
type Registry struct {
	entries []Entry
	index   map[string]Entry
}

// LoadRegistry builds one effective catalog from global then workspace roots.
func LoadRegistry(globalDir, workspaceDir string) (*Registry, error) {
	r := &Registry{index: make(map[string]Entry)}
	if globalDir != "" {
		if err := os.MkdirAll(globalDir, 0755); err != nil {
			return nil, fmt.Errorf("create global agents directory: %w", err)
		}
		if err := scan(r, globalDir, config.CapabilityScopeGlobal); err != nil {
			return nil, fmt.Errorf("scan global agents: %w", err)
		}
	}
	if workspaceDir != "" {
		if err := scan(r, workspaceDir, config.CapabilityScopeWorkspace); err != nil {
			return nil, fmt.Errorf("scan workspace agents: %w", err)
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
		path := filepath.Join(baseDir, directory.Name(), "agent.yaml")
		definition, err := loadFile(path)
		if err != nil {
			continue
		}
		name := strings.TrimSpace(definition.Name)
		if name == "" {
			name = directory.Name()
		}
		r.index[name] = Entry{
			Scope:       scope,
			Name:        name,
			Description: strings.TrimSpace(definition.Description),
			Path:        path,
		}
	}
	return nil
}

func rebuildList(r *Registry) {
	r.entries = make([]Entry, 0, len(r.index))
	for _, entry := range r.index {
		r.entries = append(r.entries, entry)
	}
	sort.Slice(r.entries, func(i, j int) bool { return r.entries[i].Name < r.entries[j].Name })
}

func (r *Registry) List() []Entry {
	if r == nil {
		return nil
	}
	return append([]Entry(nil), r.entries...)
}

func (r *Registry) Resolve(name string) (Entry, bool) {
	if r == nil {
		return Entry{}, false
	}
	entry, ok := r.index[name]
	return entry, ok
}

func (r *Registry) Load(name string) (*Definition, error) {
	entry, ok := r.Resolve(name)
	if !ok {
		return nil, fmt.Errorf("agent %q not found", name)
	}
	return r.load(entry)
}

// LoadScoped validates the session's resolved scope before loading.
func (r *Registry) LoadScoped(scope config.CapabilityScope, name string) (*Definition, error) {
	entry, ok := r.Resolve(name)
	if !ok || entry.Scope != scope {
		return nil, fmt.Errorf("agent %q [%s] not found", name, scope)
	}
	return r.load(entry)
}

func (r *Registry) load(entry Entry) (*Definition, error) {
	definition, err := loadFile(entry.Path)
	if err != nil {
		return nil, err
	}
	if definition.Name == "" {
		definition.Name = entry.Name
	}
	return definition, nil
}

func loadFile(path string) (*Definition, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var definition Definition
	if err := yaml.Unmarshal(data, &definition); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	return &definition, nil
}
