package environment

import (
	"os"
	"path/filepath"
	"testing"

	"squid-os/internal/config"
	"squid-os/internal/skills"
)

func TestLoadSkillEntriesMatchesScopeAndName(t *testing.T) {
	global, workspace := t.TempDir(), t.TempDir()
	writeEnvironmentSkill(t, global, "review", "global")
	writeEnvironmentSkill(t, workspace, "review", "workspace")
	registry, err := skills.LoadRegistry(global, workspace)
	if err != nil {
		t.Fatal(err)
	}

	if entries := loadSkillEntries(registry, []config.CapabilityRef{{Scope: config.CapabilityScopeGlobal, Name: "review"}}); len(entries) != 0 {
		t.Fatalf("stale global tuple should not match workspace catalog: %+v", entries)
	}
	entries := loadSkillEntries(registry, []config.CapabilityRef{{Scope: config.CapabilityScopeWorkspace, Name: "review"}})
	if len(entries) != 1 || entries[0].Scope != string(config.CapabilityScopeWorkspace) {
		t.Fatalf("workspace tuple not matched: %+v", entries)
	}
}

func writeEnvironmentSkill(t *testing.T, root, name, description string) {
	t.Helper()
	dir := filepath.Join(root, name)
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	content := "---\nname: " + name + "\ndescription: " + description + "\n---\nBody.\n"
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}
