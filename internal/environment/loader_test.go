package environment

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"squid-os/internal/config"
	runtimeconfig "squid-os/internal/runtime"
)

func TestEnvironmentFormatsCatalogAvailableAndMissingSections(t *testing.T) {
	globalSkills, globalAgents, workspace := t.TempDir(), t.TempDir(), t.TempDir()
	writeEnvironmentSkill(t, globalSkills, "review", "Reviews code")
	catalog, err := runtimeconfig.LoadCatalog(config.Paths{Skills: globalSkills, Agents: globalAgents}, workspace)
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.SessionConfig{
		SkillPolicy: config.CapabilityPolicy{Mode: config.PolicyModeAllowlist, Requested: []string{"review", "build"}},
		AgentPolicy: config.CapabilityPolicy{Mode: config.PolicyModeAll},
	}
	resolved := catalog.Resolve(cfg.SkillPolicy, cfg.AgentPolicy)
	cfg.Skills, cfg.Agents = resolved.Skills, resolved.Agents

	formatted := FormatEnvironment(LoadEnvironment(config.Paths{}, cfg, catalog))
	if !strings.Contains(formatted, "### Available Skills\n  - review: [global] Reviews code") || !strings.Contains(formatted, "### Missing Skills\n- build") {
		t.Fatalf("environment capability sections missing: %q", formatted)
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
