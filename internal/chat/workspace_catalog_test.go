package chat

import (
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"squid-os/internal/config"
	runtimeconfig "squid-os/internal/runtime"
)

func TestSetWorkingDirToolSwapsSessionCatalog(t *testing.T) {
	globalSkills, globalAgents := t.TempDir(), t.TempDir()
	workspaceA, workspaceB := t.TempDir(), t.TempDir()
	writeSessionSkill(t, filepath.Join(workspaceA, ".squid-os", "skills"), "build-a")
	writeSessionSkill(t, filepath.Join(workspaceB, ".squid-os", "skills"), "build-b")
	paths := config.Paths{Skills: globalSkills, Agents: globalAgents}
	catalog, err := runtimeconfig.LoadCatalog(paths, workspaceA)
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.SessionConfig{
		WorkingDir:  workspaceA,
		Tools:       []string{"set_working_dir"},
		SkillPolicy: config.CapabilityPolicy{Mode: config.PolicyModeAll},
		AgentPolicy: config.CapabilityPolicy{Mode: config.PolicyModeAll},
		Skills:      []config.CapabilityRef{{Scope: config.CapabilityScopeWorkspace, Name: "build-a"}},
	}
	session := NewSession(cfg, paths, catalog)
	args, _ := json.Marshal(map[string]any{"path": workspaceB})
	session.Stream.PartialTools = []PartialTool{{ID: "tool-1", Name: "set_working_dir", Args: string(args), FirstAt: time.Now(), DoneAt: time.Now()}}

	result := ExecuteTools(session, ToolExecOptions{MsgIdx: -1})
	if result.Action != ToolExecDone || session.Catalog.WorkingDir != workspaceB {
		t.Fatalf("catalog was not swapped: result=%+v catalog=%+v", result, session.Catalog)
	}
	if _, ok := session.Catalog.Skills.Resolve("build-b"); !ok {
		t.Fatal("new workspace skill missing")
	}
	if _, ok := session.Catalog.Skills.Resolve("build-a"); ok {
		t.Fatal("old workspace skill leaked")
	}
	if len(session.Doc.Config.Skills) != 1 || session.Doc.Config.Skills[0].Name != "build-b" {
		t.Fatalf("effective list not applied: %+v", session.Doc.Config.Skills)
	}
}
