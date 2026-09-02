package runtime

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"squid-os/internal/config"
)

func TestCatalogFormatsAvailableAndMissingCapabilities(t *testing.T) {
	globalSkills, globalAgents, workspace := t.TempDir(), t.TempDir(), t.TempDir()
	writeCatalogSkill(t, globalSkills, "browser", "Browse sites")
	catalog, err := LoadCatalog(config.Paths{Skills: globalSkills, Agents: globalAgents}, workspace)
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.SessionConfig{
		SkillPolicy: config.CapabilityPolicy{Mode: config.PolicyModeAllowlist, Requested: []string{"browser", "build"}},
		AgentPolicy: config.CapabilityPolicy{Mode: config.PolicyModeAllowlist, Requested: []string{"reviewer"}},
	}
	resolved := catalog.Resolve(cfg.SkillPolicy, cfg.AgentPolicy)
	cfg.Skills, cfg.Agents = resolved.Skills, resolved.Agents

	skillText := catalog.FormatSkills(cfg)
	if !strings.Contains(skillText, "### Available Skills\n  - browser: [global] Browse sites") || !strings.Contains(skillText, "### Missing Skills\n- build") {
		t.Fatalf("unexpected skill state: %q", skillText)
	}
	agentText := catalog.FormatAgents(cfg)
	if !strings.Contains(agentText, "### Available Agents\n- none") || !strings.Contains(agentText, "### Missing Agents\n- reviewer") {
		t.Fatalf("unexpected agent state: %q", agentText)
	}

	// No missing skills → section omitted entirely.
	noMissingCfg := config.SessionConfig{
		SkillPolicy: config.CapabilityPolicy{Mode: config.PolicyModeAllowlist, Requested: []string{"browser"}},
		AgentPolicy: config.CapabilityPolicy{Mode: config.PolicyModeAll},
	}
	noMissingResolved := catalog.Resolve(noMissingCfg.SkillPolicy, noMissingCfg.AgentPolicy)
	noMissingCfg.Skills, noMissingCfg.Agents = noMissingResolved.Skills, noMissingResolved.Agents
	if got := catalog.FormatSkills(noMissingCfg); strings.Contains(got, "### Missing Skills") {
		t.Fatalf("expected no Missing Skills section, got: %q", got)
	}
	if got := catalog.FormatAgents(noMissingCfg); strings.Contains(got, "### Missing Agents") {
		t.Fatalf("expected no Missing Agents section, got: %q", got)
	}
}

func writeCatalogSkill(t *testing.T, root, name, description string) {
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
