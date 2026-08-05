package app

import (
	"os"
	"path/filepath"
	"testing"

	"squid-os/internal/config"
	runtimeconfig "squid-os/internal/runtime"
	"squid-os/internal/skills"
	"squid-os/internal/ui/component"
)

func TestSkillPickerUsesSessionCatalog(t *testing.T) {
	global, workspaceA := t.TempDir(), t.TempDir()
	writePickerSkill(t, filepath.Join(workspaceA, ".squid-os", "skills"), "build-a")
	registryA, err := skills.LoadRegistry(global, filepath.Join(workspaceA, ".squid-os", "skills"))
	if err != nil {
		t.Fatal(err)
	}
	model := New(StartupOptions{Session: runtimeconfig.SessionRequest{
		Config:  config.SessionConfig{Skills: []config.CapabilityRef{{Scope: config.CapabilityScopeWorkspace, Name: "build-a"}}},
		Catalog: runtimeconfig.Catalog{WorkingDir: workspaceA, Skills: registryA},
	}})

	updated, _ := model.openSkillPicker()
	picker, ok := updated.activeComponent.(*component.Picker)
	if !ok {
		t.Fatalf("skill picker not opened: %T", updated.activeComponent)
	}
	if len(picker.Items) != 2 || picker.Items[1].Value != "build-a" {
		t.Fatalf("picker did not use session catalog: %+v", picker.Items)
	}
}

func writePickerSkill(t *testing.T, root, name string) {
	t.Helper()
	dir := filepath.Join(root, name)
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	content := "---\nname: " + name + "\ndescription: picker test\n---\nBody.\n"
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}
