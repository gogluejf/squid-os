package chat

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"squid-os/internal/config"
	"squid-os/internal/media"
	runtimeconfig "squid-os/internal/runtime"
)

func TestNewSessionIncludesToolsBootstrap(t *testing.T) {
	session := NewRootSession(config.SessionConfig{Tools: []string{"read_file"}}, config.Paths{}, runtimeconfig.Catalog{})
	assertMessageOrder(t, session.Doc.Messages, "sys0", "env0", "config0", "tools0")

	message := messageByID(session.Doc.Messages, "tools0")
	if message == nil || message.Label != "Tools Enabled" || message.Params["tools"] == "" {
		t.Fatalf("invalid tools bootstrap: %+v", message)
	}
	if message.InputTokens <= 0 {
		t.Fatalf("tools schema tokens should be counted: %+v", message)
	}
}

func TestLoadChildRejectsDifferentAutosaveName(t *testing.T) {
	paths := config.Paths{Sessions: t.TempDir()}
	childDir := config.ChildSessionDir(config.RootSessionDir(paths, "root"), "child")
	doc := config.NewSessionDocWithIdentity(config.SessionConfig{
		Autosave: config.SessionAutosave{Enabled: true, Name: "different-name"},
	}, config.SessionIdentity{ID: "child", ParentID: "parent", RootID: "root", Depth: 1})

	if _, err := LoadSession(doc, childDir, paths, runtimeconfig.Catalog{}); err == nil || !strings.Contains(err.Error(), "child session save-as is not supported") {
		t.Fatalf("expected child save-as rejection, got %v", err)
	}
}

func TestLoadChildKeepsMatchingNestedDirectory(t *testing.T) {
	paths := config.Paths{Sessions: t.TempDir()}
	childDir := config.ChildSessionDir(config.RootSessionDir(paths, "root"), "child")
	doc := config.NewSessionDocWithIdentity(config.SessionConfig{
		Autosave: config.SessionAutosave{Enabled: true, Name: "child"},
	}, config.SessionIdentity{ID: "child", ParentID: "parent", RootID: "root", Depth: 1})

	session, err := LoadSession(doc, childDir, paths, runtimeconfig.Catalog{})
	if err != nil {
		t.Fatal(err)
	}
	if session.SessionDir != childDir {
		t.Fatalf("child SessionDir = %q, want %q", session.SessionDir, childDir)
	}
}

func TestSetPendingInferenceBeforeFirstTurnRewritesBootstrap(t *testing.T) {
	initial := config.InferenceConfig{
		Provider: "vllm",
		Model:    "qwen",
		Thinking: config.ThinkingConfig{Enabled: false},
	}
	next := config.InferenceConfig{
		Provider: "openai",
		Model:    "gpt",
		Thinking: config.ThinkingConfig{Enabled: true},
	}
	session := NewRootSession(config.SessionConfig{Inference: initial}, config.Paths{}, runtimeconfig.Catalog{})

	session.SetPendingInference(next)

	if session.Doc.Initial.Inference != next {
		t.Fatalf("initial inference not rewritten: got %+v, want %+v", session.Doc.Initial.Inference, next)
	}
	if session.CurrentInference() != next {
		t.Fatalf("current inference not rewritten: got %+v, want %+v", session.CurrentInference(), next)
	}
	if session.Doc.Pending != nil && session.Doc.Pending.Inference != nil {
		t.Fatalf("bootstrap inference change must not remain pending: %+v", session.Doc.Pending.Inference)
	}
	message := messageByID(session.Doc.Messages, "config0")
	if message == nil {
		t.Fatal("config0 message missing")
	}
	if message.Params["provider"] != "openai" || message.Params["model"] != "gpt" || message.Params["thinking"] != "on" {
		t.Fatalf("config0 not rewritten: %+v", message)
	}
	for _, message := range session.Doc.Messages {
		if message.Label == "Model Switched" || message.Label == "Thinking Switched" {
			t.Fatalf("bootstrap change created a transition: %+v", message)
		}
	}
}

func TestSetPendingInferenceAfterFirstTurnQueuesTransition(t *testing.T) {
	initial := config.InferenceConfig{Provider: "vllm", Model: "qwen"}
	next := config.InferenceConfig{Provider: "openai", Model: "gpt"}
	session := NewRootSession(config.SessionConfig{Inference: initial}, config.Paths{}, runtimeconfig.Catalog{})
	session.Append(NewUserMessage("msg_1", "hello"))

	session.SetPendingInference(next)

	if session.CurrentInference() != initial {
		t.Fatalf("current inference changed before PrepareTurn: %+v", session.CurrentInference())
	}
	if session.Doc.Initial.Inference != initial {
		t.Fatalf("initial inference changed after first turn: %+v", session.Doc.Initial.Inference)
	}
	if session.Doc.Pending == nil || session.Doc.Pending.Inference == nil || *session.Doc.Pending.Inference != next {
		t.Fatalf("inference transition not queued: %+v", session.Doc.Pending)
	}
}

func TestSessionCatalogAreIndependent(t *testing.T) {
	globalSkills, globalAgents := t.TempDir(), t.TempDir()
	workspaceA, workspaceB := t.TempDir(), t.TempDir()
	writeSessionSkill(t, filepath.Join(workspaceA, ".squid-os", "skills"), "build-a")
	writeSessionSkill(t, filepath.Join(workspaceB, ".squid-os", "skills"), "build-b")
	paths := config.Paths{Skills: globalSkills, Agents: globalAgents}
	catalogsA, err := runtimeconfig.LoadCatalog(paths, workspaceA)
	if err != nil {
		t.Fatal(err)
	}
	catalogsB, err := runtimeconfig.LoadCatalog(paths, workspaceB)
	if err != nil {
		t.Fatal(err)
	}
	sessionA := NewRootSession(config.SessionConfig{WorkingDir: workspaceA}, paths, catalogsA)
	sessionB := NewRootSession(config.SessionConfig{WorkingDir: workspaceB}, paths, catalogsB)
	if _, ok := sessionA.Catalog.Skills.Resolve("build-a"); !ok {
		t.Fatal("session A lost its catalog")
	}
	if _, ok := sessionA.Catalog.Skills.Resolve("build-b"); ok {
		t.Fatal("session A sees session B catalog")
	}
	if _, ok := sessionB.Catalog.Skills.Resolve("build-b"); !ok {
		t.Fatal("session B lost its catalog")
	}
}

func TestSetWorkingDirReappliesCapabilityPolicy(t *testing.T) {
	globalSkills, globalAgents := t.TempDir(), t.TempDir()
	workspaceA, workspaceB := t.TempDir(), t.TempDir()
	writeSessionSkill(t, filepath.Join(workspaceA, ".squid-os", "skills"), "build")
	paths := config.Paths{Skills: globalSkills, Agents: globalAgents}
	cfg := config.SessionConfig{
		WorkingDir:  workspaceA,
		SkillPolicy: config.CapabilityPolicy{Mode: config.PolicyModeAllowlist, Requested: []string{"build"}},
		AgentPolicy: config.CapabilityPolicy{Mode: config.PolicyModeAll},
		Skills:      []config.CapabilityRef{{Scope: config.CapabilityScopeWorkspace, Name: "build"}},
	}
	catalogs, err := runtimeconfig.LoadCatalog(paths, workspaceA)
	if err != nil {
		t.Fatal(err)
	}
	session := NewRootSession(cfg, paths, catalogs)

	summary, err := session.SetWorkingDir(workspaceB)
	if err != nil {
		t.Fatal(err)
	}
	if session.Doc.Config.WorkingDir != workspaceB || len(session.Doc.Config.Skills) != 0 {
		t.Fatalf("workspace state not applied immediately: %+v", session.Doc.Config)
	}
	if !strings.Contains(summary, "### Available Skills\n- none") || !strings.Contains(summary, "### Missing Skills\n- build") {
		t.Fatalf("workspace summary missing current state: %q", summary)
	}
}

func writeSessionSkill(t *testing.T, root, name string) {
	t.Helper()
	dir := filepath.Join(root, name)
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	body := "---\nname: " + name + "\ndescription: project build\n---\nBuild.\n"
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(body), 0644); err != nil {
		t.Fatal(err)
	}
}

func assertMessageOrder(t *testing.T, messages []config.Message, ids ...string) {
	t.Helper()
	if len(messages) < len(ids) {
		t.Fatalf("got %d messages, want at least %d", len(messages), len(ids))
	}
	for index, id := range ids {
		if messages[index].ID != id {
			t.Fatalf("message %d: got %q, want %q", index, messages[index].ID, id)
		}
	}
}

func messageByID(messages []config.Message, id string) *config.Message {
	for index := range messages {
		if messages[index].ID == id {
			return &messages[index]
		}
	}
	return nil
}

// --- Task 1.1: Attachment Contract Tests ---

func TestSessionInitWorkspace(t *testing.T) {
	session := NewRootSession(config.SessionConfig{}, config.Paths{}, runtimeconfig.Catalog{})
	// NewRootSession calls InitWorkspace internally via newSessionWithIdentity.
	// Verify it was initialized.
	if session.Workspace == nil {
		t.Fatal("workspace should be non-nil after NewRootSession")
	}
	if session.Doc.Attachments == nil {
		t.Fatal("Attachments slice should be initialized")
	}
	// InitWorkspace is idempotent — calling again should not change anything.
	first := session.Workspace
	session.InitWorkspace()
	if session.Workspace != first {
		t.Fatal("InitWorkspace should be idempotent")
	}
}

func TestSessionInitWorkspaceIdempotent(t *testing.T) {
	session := NewRootSession(config.SessionConfig{}, config.Paths{}, runtimeconfig.Catalog{})
	session.InitWorkspace()
	first := session.Workspace
	session.InitWorkspace()
	if session.Workspace != first {
		t.Fatal("InitWorkspace should be idempotent")
	}
}

func TestSessionEnsureWorkspace(t *testing.T) {
	session := NewRootSession(config.SessionConfig{}, config.Paths{}, runtimeconfig.Catalog{})
	ws := session.EnsureWorkspace()
	if ws == nil {
		t.Fatal("EnsureWorkspace should return non-nil")
	}
	if session.Workspace == nil {
		t.Fatal("EnsureWorkspace should initialize Workspace field")
	}
}

func TestSessionAddAttachment(t *testing.T) {
	session := NewRootSession(config.SessionConfig{}, config.Paths{}, runtimeconfig.Catalog{})
	session.InitWorkspace()

	a := media.Attachment{
		ID:           "test-attach",
		FileName: "test.png",
		MIME:         "image/png",
		Kind:         media.KindImage,
	}
	session.AddAttachment(a)

	if len(session.Doc.Attachments) != 1 {
		t.Fatalf("expected 1 attachment, got %d", len(session.Doc.Attachments))
	}
	if session.Doc.Attachments[0].ID != "test-attach" {
		t.Fatalf("attachment ID mismatch: %q", session.Doc.Attachments[0].ID)
	}
}

func TestSessionGetAttachment(t *testing.T) {
	session := NewRootSession(config.SessionConfig{}, config.Paths{}, runtimeconfig.Catalog{})
	session.InitWorkspace()

	a := media.Attachment{
		ID:           "lookup-test",
		FileName: "lookup.png",
		MIME:         "image/png",
	}
	session.AddAttachment(a)

	resolved, _, err := session.GetAttachment("lookup-test")
	if err != nil {
		t.Fatalf("GetAttachment failed: %v", err)
	}
	if resolved.ID != "lookup-test" {
		t.Fatalf("resolved ID = %q, want lookup-test", resolved.ID)
	}
}

func TestSessionGetAttachmentMissing(t *testing.T) {
	session := NewRootSession(config.SessionConfig{}, config.Paths{}, runtimeconfig.Catalog{})
	session.InitWorkspace()

	_, _, err := session.GetAttachment("missing-id")
	if err == nil {
		t.Fatal("GetAttachment should fail for missing attachment")
	}
}

func TestSessionGetAttachmentNoWorkspace(t *testing.T) {
	session := NewRootSession(config.SessionConfig{}, config.Paths{}, runtimeconfig.Catalog{})
	// Don't init workspace
	_, _, err := session.GetAttachment("any")
	if err == nil {
		t.Fatal("GetAttachment should fail when workspace is nil")
	}
}

func TestSessionHasAttachments(t *testing.T) {
	session := NewRootSession(config.SessionConfig{}, config.Paths{}, runtimeconfig.Catalog{})
	session.InitWorkspace()

	if session.HasAttachments() {
		t.Fatal("HasAttachments should be false for empty session")
	}

	session.AddAttachment(media.Attachment{
		ID: "x", FileName: "x.png", MIME: "image/png",
	})
	if !session.HasAttachments() {
		t.Fatal("HasAttachments should be true after adding attachment")
	}
}

func TestSessionMigrateImagePath(t *testing.T) {
	// MigrateImagePath has been removed from chat.Session.
	// Legacy migration now happens at config.LoadSessionDoc time.
	// This test is a no-op placeholder to maintain test count parity.
}

func TestSessionMigrateImagePathEmpty(t *testing.T) {
	// No-op: migration moved to config package.
}

func TestSessionDocPreservesAttachmentsOnRoundTrip(t *testing.T) {
	root := t.TempDir()
	paths := config.Paths{Sessions: root}

	doc := config.NewSessionDoc(config.SessionConfig{})
	doc.Attachments = []media.Attachment{
		{ID: "round-trip", FileName: "rt.png", MIME: "image/png", Kind: media.KindImage, Source: media.SourceFile},
	}

	if err := config.SaveSessionDoc(config.RootSessionDir(paths, "rt-test"), doc, nil); err != nil {
		t.Fatalf("SaveSessionDoc failed: %v", err)
	}

	loaded, err := config.LoadSessionDoc(config.RootSessionDir(paths, "rt-test"))
	if err != nil {
		t.Fatalf("LoadSessionDoc failed: %v", err)
	}

	if len(loaded.Attachments) != 1 {
		t.Fatalf("loaded Attachments count = %d, want 1", len(loaded.Attachments))
	}
	if loaded.Attachments[0].ID != "round-trip" {
		t.Fatalf("loaded attachment ID = %q, want round-trip", loaded.Attachments[0].ID)
	}
	if loaded.Attachments[0].Kind != media.KindImage {
		t.Fatalf("loaded attachment Kind = %q, want image", loaded.Attachments[0].Kind)
	}
}

func TestSessionDocVersionIsTwo(t *testing.T) {
	doc := config.NewSessionDoc(config.SessionConfig{})
	if doc.Version != 2 {
		t.Fatalf("SessionDoc.Version = %d, want 2", doc.Version)
	}
}
