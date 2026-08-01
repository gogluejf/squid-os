package agent

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

type Registry struct {
	baseDir string
	entries []Entry
	index   map[string]Entry
}

var global *Registry

func InitRegistry(baseDir string) (*Registry, error) {
	if err := os.MkdirAll(baseDir, 0755); err != nil {
		return nil, fmt.Errorf("create agents directory: %w", err)
	}
	r := &Registry{baseDir: baseDir}
	if err := r.Rescan(); err != nil {
		return nil, err
	}
	global = r
	return r, nil
}

func GetRegistry() *Registry { return global }

func (r *Registry) Rescan() error {
	directories, err := os.ReadDir(r.baseDir)
	if err != nil {
		return fmt.Errorf("read agents directory: %w", err)
	}
	r.entries = nil
	r.index = make(map[string]Entry)
	for _, directory := range directories {
		if !directory.IsDir() {
			continue
		}
		path := filepath.Join(r.baseDir, directory.Name(), "agent.yaml")
		definition, err := loadFile(path)
		if err != nil {
			continue
		}
		name := strings.TrimSpace(definition.Name)
		if name == "" {
			name = directory.Name()
		}
		entry := Entry{Name: name, Description: strings.TrimSpace(definition.Description), Path: path}
		r.entries = append(r.entries, entry)
		r.index[name] = entry
	}
	sort.Slice(r.entries, func(i, j int) bool { return r.entries[i].Name < r.entries[j].Name })
	return nil
}

func (r *Registry) List() []Entry {
	if r == nil {
		return nil
	}
	return append([]Entry(nil), r.entries...)
}

func (r *Registry) Load(name string) (*Definition, error) {
	if r == nil {
		return nil, fmt.Errorf("agent registry not initialized")
	}
	entry, ok := r.index[name]
	if !ok {
		return nil, fmt.Errorf("agent %q not found", name)
	}
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
