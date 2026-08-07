package tools

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"squid-os/internal/agent"
	"squid-os/internal/config"
	"squid-os/internal/skills"
)

type skillCatalog struct{ registry *skills.Registry }

func (c skillCatalog) ResolveSkill(name string) (skills.SkillEntry, bool) {
	return c.registry.Resolve(name)
}
func (c skillCatalog) ResolveAgent(string) (agent.Entry, bool) { return agent.Entry{}, false }
func (c skillCatalog) LoadSkill(scope config.CapabilityScope, name string) (*skills.Skill, error) {
	return c.registry.LoadScoped(scope, name)
}
func (c skillCatalog) LoadAgent(config.CapabilityScope, string) (*agent.Definition, error) {
	return nil, nil
}

func TestSkillLoadUsesSessionScope(t *testing.T) {
	global, workspace := t.TempDir(), t.TempDir()
	writeToolSkill(t, global, "review", "global body")
	writeToolSkill(t, workspace, "review", "workspace body")
	registry, err := skills.LoadRegistry(global, workspace)
	if err != nil {
		t.Fatal(err)
	}

	globalConfig := config.SessionConfig{Skills: []config.CapabilityRef{{Scope: config.CapabilityScopeGlobal, Name: "review"}}}
	if result := SkillLoad.Execute(map[string]interface{}{"name": "review"}, RuntimeContext{Config: globalConfig, Catalog: skillCatalog{registry}}); result.Status != ResultStatusError {
		t.Fatalf("stale global scope should fail: %+v", result)
	}

	workspaceConfig := config.SessionConfig{Skills: []config.CapabilityRef{{Scope: config.CapabilityScopeWorkspace, Name: "review"}}}
	result := SkillLoad.Execute(map[string]interface{}{"name": "review"}, RuntimeContext{Config: workspaceConfig, Catalog: skillCatalog{registry}})
	if result.Status != ResultStatusSuccess || !strings.Contains(result.Result, "workspace body") {
		t.Fatalf("workspace scope was not loaded: %+v", result)
	}
}

func writeToolSkill(t *testing.T, root, name, body string) {
	t.Helper()
	dir := filepath.Join(root, name)
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	content := "---\nname: " + name + "\ndescription: test\n---\n" + body + "\n"
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}
