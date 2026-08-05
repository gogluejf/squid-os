package skills

import (
	"os"
	"path/filepath"
	"testing"

	"squid-os/internal/config"
)

func TestRegistryWorkspaceShadowsGlobalWithoutDuplicate(t *testing.T) {
	global, workspace := t.TempDir(), t.TempDir()
	writeTestSkill(t, global, "review", "global")
	writeTestSkill(t, workspace, "review", "workspace")
	writeTestSkill(t, global, "other", "other")

	r, err := LoadRegistry(global, workspace)
	if err != nil {
		t.Fatal(err)
	}
	entries := r.List()
	if len(entries) != 2 {
		t.Fatalf("expected deduplicated catalog, got %+v", entries)
	}
	entry, ok := r.Resolve("review")
	if !ok || entry.Scope != config.CapabilityScopeWorkspace || entry.Description != "workspace" {
		t.Fatalf("workspace did not shadow global: %+v", entry)
	}
	if _, err := r.LoadScoped(config.CapabilityScopeGlobal, "review"); err == nil {
		t.Fatal("expected stale global scope to be rejected")
	}
	if skill, err := r.LoadScoped(config.CapabilityScopeWorkspace, "review"); err != nil || skill.Description != "workspace" {
		t.Fatalf("scoped workspace load failed: %+v %v", skill, err)
	}
}

func writeTestSkill(t *testing.T, root, name, description string) {
	t.Helper()
	dir := filepath.Join(root, name)
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	body := "---\nname: " + name + "\ndescription: " + description + "\n---\nInstructions.\n"
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(body), 0644); err != nil {
		t.Fatal(err)
	}
}
