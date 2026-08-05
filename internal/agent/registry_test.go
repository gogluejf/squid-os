package agent

import (
	"os"
	"path/filepath"
	"testing"

	"squid-os/internal/config"
)

func TestRegistryScanAndLoad(t *testing.T) {
	root := t.TempDir()
	writeTestAgent(t, root, "review", "Reviews code", "openai/test")
	r, err := LoadRegistry(root, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(r.List()) != 1 {
		t.Fatal("missing entry")
	}
	d, err := r.Load("review")
	if err != nil || d.Model != "openai/test" {
		t.Fatalf("%+v %v", d, err)
	}
}

func TestRegistryWorkspaceShadowsGlobalWithoutDuplicate(t *testing.T) {
	global, workspace := t.TempDir(), t.TempDir()
	writeTestAgent(t, global, "review", "global", "openai/global")
	writeTestAgent(t, workspace, "review", "workspace", "openai/workspace")

	r, err := LoadRegistry(global, workspace)
	if err != nil {
		t.Fatal(err)
	}
	if entries := r.List(); len(entries) != 1 || entries[0].Scope != config.CapabilityScopeWorkspace {
		t.Fatalf("expected one workspace entry: %+v", entries)
	}
	if _, err := r.LoadScoped(config.CapabilityScopeGlobal, "review"); err == nil {
		t.Fatal("expected stale global scope rejection")
	}
	definition, err := r.LoadScoped(config.CapabilityScopeWorkspace, "review")
	if err != nil || definition.Model != "openai/workspace" {
		t.Fatalf("scoped load failed: %+v %v", definition, err)
	}
}

func writeTestAgent(t *testing.T, root, name, description, model string) {
	t.Helper()
	dir := filepath.Join(root, name)
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	body := "name: " + name + "\ndescription: " + description + "\nmodel: " + model + "\n"
	if err := os.WriteFile(filepath.Join(dir, "agent.yaml"), []byte(body), 0644); err != nil {
		t.Fatal(err)
	}
}
