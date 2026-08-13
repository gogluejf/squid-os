package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestNewSessionDocKeepsInitialConfigIndependent(t *testing.T) {
	cfg := SessionConfig{
		Tools:  []string{"read"},
		Skills: []CapabilityRef{{Scope: "global", Name: "review"}},
		Agents: []CapabilityRef{{Scope: "global", Name: "researcher"}},
	}
	doc := NewSessionDoc(cfg)

	doc.Config.Tools[0] = "bash"
	doc.Config.Skills[0] = CapabilityRef{Scope: "global", Name: "plan"}
	doc.Config.Agents[0] = CapabilityRef{Scope: "global", Name: "coder"}

	if doc.Initial.Tools[0] != "read" || doc.Initial.Skills[0].Name != "review" || doc.Initial.Agents[0].Name != "researcher" {
		t.Fatalf("initial config mutated with current config: %+v", doc.Initial)
	}
}

func TestParseAuthorizationMode(t *testing.T) {
	for _, value := range []AuthorizationMode{AuthorizationAuto, AuthorizationAskOnWrite, AuthorizationAskForAll, AuthorizationEndOnWrite, AuthorizationEndOnAll} {
		got, err := ParseAuthorizationMode(string(value))
		if err != nil || got != value {
			t.Fatalf("value=%q got=%q err=%v", value, got, err)
		}
	}
	if _, err := ParseAuthorizationMode("wrong"); err == nil {
		t.Fatal("expected invalid mode error")
	}
}

func TestSessionStorageUsesFolderAndChatJSON(t *testing.T) {
	root := t.TempDir()
	paths := Paths{Sessions: root}
	doc := NewSessionDoc(SessionConfig{})

	if err := SaveSessionDoc(RootSessionDir(paths, "newest"), doc, nil); err != nil {
		t.Fatalf("SaveSessionDoc failed: %v", err)
	}
	want := filepath.Join(root, "newest", "chat.json")
	if got := SessionFilePath(RootSessionDir(paths, "newest")); got != want {
		t.Fatalf("SessionFilePath() = %q, want %q", got, want)
	}
	chatInfo, err := os.Stat(want)
	if err != nil {
		t.Fatalf("chat.json was not created: %v", err)
	}
	dirInfo, err := os.Stat(filepath.Dir(want))
	if err != nil {
		t.Fatalf("session folder was not created: %v", err)
	}
	if !dirInfo.ModTime().Equal(chatInfo.ModTime()) {
		t.Fatalf("folder mtime %v does not match chat.json mtime %v", dirInfo.ModTime(), chatInfo.ModTime())
	}
	if _, err := LoadSessionDoc(RootSessionDir(paths, "newest")); err != nil {
		t.Fatalf("LoadSessionDoc failed: %v", err)
	}
}

func TestListSessionsSortsByChatJSONModTime(t *testing.T) {
	root := t.TempDir()
	paths := Paths{Sessions: root}
	for _, name := range []string{"older", "newer"} {
		if err := SaveSessionDoc(RootSessionDir(paths, name), NewSessionDoc(SessionConfig{}), nil); err != nil {
			t.Fatalf("save %s: %v", name, err)
		}
	}
	older := time.Now().Add(-time.Hour)
	if err := os.Chtimes(SessionFilePath(RootSessionDir(paths, "older")), older, older); err != nil {
		t.Fatalf("set older chat time: %v", err)
	}

	sessions := ListSessions(paths)
	if len(sessions) != 2 {
		t.Fatalf("ListSessions returned %d sessions, want 2", len(sessions))
	}
	if sessions[0].Name != "newer" || sessions[1].Name != "older" {
		t.Fatalf("unexpected order: %q, %q", sessions[0].Name, sessions[1].Name)
	}
}

// --- Task 1.1: Session Identity and Child Execution Links ---

func TestNewSessionDocRootIdentity(t *testing.T) {
	doc := NewSessionDoc(SessionConfig{})

	if doc.Identity.ID == "" {
		t.Fatal("root session ID is empty")
	}
	if doc.Identity.RootID != doc.Identity.ID {
		t.Fatalf("root session RootID %q != ID %q", doc.Identity.RootID, doc.Identity.ID)
	}
	if doc.Identity.ParentID != "" {
		t.Fatalf("root session ParentID should be empty, got %q", doc.Identity.ParentID)
	}
	if doc.Identity.ParentToolCallID != "" {
		t.Fatalf("root session ParentToolCallID should be empty, got %q", doc.Identity.ParentToolCallID)
	}
	if doc.Identity.Depth != 0 {
		t.Fatalf("root session Depth should be 0, got %d", doc.Identity.Depth)
	}
}

func TestNewSessionDocMetaTimestampsOnly(t *testing.T) {
	doc := NewSessionDoc(SessionConfig{})

	if doc.Meta.CreatedAt == "" {
		t.Fatal("CreatedAt is empty")
	}
	if doc.Meta.UpdatedAt == "" {
		t.Fatal("UpdatedAt is empty")
	}
}

func TestChildSessionIdentityConstruction(t *testing.T) {
	child := SessionIdentity{
		ID:               "child-123",
		ParentID:         "parent-456",
		RootID:           "root-789",
		ParentToolCallID: "call-abc",
		Depth:            1,
	}

	if child.ID != "child-123" {
		t.Fatalf("unexpected child ID: %q", child.ID)
	}
	if child.ParentID != "parent-456" {
		t.Fatalf("unexpected ParentID: %q", child.ParentID)
	}
	if child.RootID != "root-789" {
		t.Fatalf("unexpected RootID: %q", child.RootID)
	}
	if child.ParentToolCallID != "call-abc" {
		t.Fatalf("unexpected ParentToolCallID: %q", child.ParentToolCallID)
	}
	if child.Depth != 1 {
		t.Fatalf("unexpected Depth: %d", child.Depth)
	}
}

func TestChildSessionIdentityDoesNotMutateConfig(t *testing.T) {
	cfg := SessionConfig{
		Tools: []string{"read"},
	}
	doc := NewSessionDoc(cfg)

	// Override identity to simulate a child session
	doc.Identity = SessionIdentity{
		ID:               "child",
		ParentID:         "parent",
		RootID:           "root",
		ParentToolCallID: "call-1",
		Depth:            1,
	}

	// Config should be untouched
	if len(doc.Config.Tools) != 1 || doc.Config.Tools[0] != "read" {
		t.Fatalf("Config mutated by identity change: %+v", doc.Config)
	}
}

func TestToolExecutionChildFieldsOmittedForNonAgent(t *testing.T) {
	tc := ToolCallEntry{
		ID:   "tc-1",
		Type: "tool_use",
	}
	tc.Instruction.Name = "read_file"
	tc.Execution.Status = "success"
	tc.Execution.Result = "file content"

	data, err := json.Marshal(tc)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}
	jsonStr := string(data)

	if contains(jsonStr, "child_session_id") {
		t.Fatalf("non-agent tool should omit child_session_id: %s", jsonStr)
	}
	if contains(jsonStr, "child_session_name") {
		t.Fatalf("non-agent tool should omit child_session_name: %s", jsonStr)
	}
}

func TestToolExecutionChildFieldsRoundTrip(t *testing.T) {
	tc := ToolCallEntry{
		ID:   "tc-agent",
		Type: "tool_use",
	}
	tc.Instruction.Name = "call_agent"
	tc.Execution.Status = "success"
	tc.Execution.ChildSessionID = "child-session-id"
	tc.Execution.ChildSessionName = "trader-abc123"

	data, err := json.Marshal(tc)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	var decoded ToolCallEntry
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	if decoded.Execution.ChildSessionID != "child-session-id" {
		t.Fatalf("ChildSessionID mismatch: %q", decoded.Execution.ChildSessionID)
	}
	if decoded.Execution.ChildSessionName != "trader-abc123" {
		t.Fatalf("ChildSessionName mismatch: %q", decoded.Execution.ChildSessionName)
	}
}

func TestSessionDocRoundTripWithIdentity(t *testing.T) {
	doc := NewSessionDoc(SessionConfig{})
	doc.Identity = SessionIdentity{
		ID:               "child-1",
		ParentID:         "parent-1",
		RootID:           "root-1",
		ParentToolCallID: "call-1",
		Depth:            2,
	}

	data, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	var decoded SessionDoc
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	if decoded.Identity.ID != "child-1" {
		t.Fatalf("Identity.ID mismatch: %q", decoded.Identity.ID)
	}
	if decoded.Identity.ParentID != "parent-1" {
		t.Fatalf("Identity.ParentID mismatch: %q", decoded.Identity.ParentID)
	}
	if decoded.Identity.RootID != "root-1" {
		t.Fatalf("Identity.RootID mismatch: %q", decoded.Identity.RootID)
	}
	if decoded.Identity.ParentToolCallID != "call-1" {
		t.Fatalf("Identity.ParentToolCallID mismatch: %q", decoded.Identity.ParentToolCallID)
	}
	if decoded.Identity.Depth != 2 {
		t.Fatalf("Identity.Depth mismatch: %d", decoded.Identity.Depth)
	}
}

func TestSaveLoadSessionPreservesIdentity(t *testing.T) {
	root := t.TempDir()
	paths := Paths{Sessions: root}

	doc := NewSessionDoc(SessionConfig{})
	doc.Identity = SessionIdentity{
		ID:               "persisted-child",
		ParentID:         "persisted-parent",
		RootID:           "persisted-root",
		ParentToolCallID: "persisted-call",
		Depth:            1,
	}

	if err := SaveSessionDoc(RootSessionDir(paths, "test-session"), doc, nil); err != nil {
		t.Fatalf("SaveSessionDoc failed: %v", err)
	}

	loaded, err := LoadSessionDoc(RootSessionDir(paths, "test-session"))
	if err != nil {
		t.Fatalf("LoadSessionDoc failed: %v", err)
	}

	if loaded.Identity.ID != "persisted-child" {
		t.Fatalf("loaded Identity.ID mismatch: %q", loaded.Identity.ID)
	}
	if loaded.Identity.ParentID != "persisted-parent" {
		t.Fatalf("loaded Identity.ParentID mismatch: %q", loaded.Identity.ParentID)
	}
	if loaded.Identity.RootID != "persisted-root" {
		t.Fatalf("loaded Identity.RootID mismatch: %q", loaded.Identity.RootID)
	}
	if loaded.Identity.Depth != 1 {
		t.Fatalf("loaded Identity.Depth mismatch: %d", loaded.Identity.Depth)
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr ||
		func() bool {
			for i := 0; i <= len(s)-len(substr); i++ {
				if s[i:i+len(substr)] == substr {
					return true
				}
			}
			return false
		}())
}

// --- Task 2.1: Parent-Relative Session Location Primitives ---

func TestValidateSessionNameAcceptsValidNames(t *testing.T) {
	for _, name := range []string{"my-session", "trader-abc123", "inline-call-456", "session_1"} {
		if err := ValidateSessionName(name); err != nil {
			t.Fatalf("ValidateSessionName(%q) = %v, want nil", name, err)
		}
	}
}

func TestValidateSessionNameRejectsInvalidNames(t *testing.T) {
	cases := []struct {
		name string
	}{
		{""},
		{"/absolute/path"},
		{"../escape"},
		{"./relative"},
		{"sub/dir"},
		{"sub\\dir"},
	}
	for _, tc := range cases {
		if err := ValidateSessionName(tc.name); err == nil {
			t.Fatalf("ValidateSessionName(%q) = nil, want error", tc.name)
		}
	}
}

func TestRootSessionDir(t *testing.T) {
	p := Paths{Sessions: "/home/user/.config/squid-os/sessions"}
	want := "/home/user/.config/squid-os/sessions/my-session"
	if got := RootSessionDir(p, "my-session"); got != want {
		t.Fatalf("RootSessionDir() = %q, want %q", got, want)
	}
}

func TestChildSessionDir(t *testing.T) {
	parentDir := "/home/user/.config/squid-os/sessions/my-session"
	childName := "trader-abc123"
	want := "/home/user/.config/squid-os/sessions/my-session/agents/trader-abc123"
	if got := ChildSessionDir(parentDir, childName); got != want {
		t.Fatalf("ChildSessionDir() = %q, want %q", got, want)
	}
}

func TestGrandchildSessionDirRecursive(t *testing.T) {
	rootDir := "/sessions/my-session"
	child := ChildSessionDir(rootDir, "child-1")
	wantChild := "/sessions/my-session/agents/child-1"
	if child != wantChild {
		t.Fatalf("child dir = %q, want %q", child, wantChild)
	}

	grandchild := ChildSessionDir(child, "grandchild-1")
	wantGrandchild := "/sessions/my-session/agents/child-1/agents/grandchild-1"
	if grandchild != wantGrandchild {
		t.Fatalf("grandchild dir = %q, want %q", grandchild, wantGrandchild)
	}
}

func TestSessionFilePath(t *testing.T) {
	dir := "/sessions/my-session"
	want := "/sessions/my-session/chat.json"
	if got := SessionFilePath(dir); got != want {
		t.Fatalf("SessionFilePath() = %q, want %q", got, want)
	}
}

func TestSaveLoadSessionDocRejectsEmptyDirectory(t *testing.T) {
	if err := SaveSessionDoc("", NewSessionDoc(SessionConfig{}), nil); err == nil {
		t.Fatal("SaveSessionDoc accepted an empty session directory")
	}
	if _, err := LoadSessionDoc(""); err == nil {
		t.Fatal("LoadSessionDoc accepted an empty session directory")
	}
}

func TestSaveLoadSessionDoc(t *testing.T) {
	root := t.TempDir()
	sessionDir := filepath.Join(root, "agents", "child-session")
	doc := NewSessionDoc(SessionConfig{})
	doc.Identity = SessionIdentity{
		ID:               "child-id",
		ParentID:         "parent-id",
		RootID:           "root-id",
		ParentToolCallID: "call-1",
		Depth:            1,
	}

	if err := SaveSessionDoc(sessionDir, doc, nil); err != nil {
		t.Fatalf("SaveSessionDoc failed: %v", err)
	}

	wantPath := filepath.Join(sessionDir, "chat.json")
	if _, err := os.Stat(wantPath); err != nil {
		t.Fatalf("chat.json not created at %q: %v", wantPath, err)
	}

	loaded, err := LoadSessionDoc(sessionDir)
	if err != nil {
		t.Fatalf("LoadSessionDoc failed: %v", err)
	}

	if loaded.Identity.ID != "child-id" {
		t.Fatalf("loaded Identity.ID = %q, want %q", loaded.Identity.ID, "child-id")
	}
	if loaded.Identity.ParentID != "parent-id" {
		t.Fatalf("loaded Identity.ParentID = %q, want %q", loaded.Identity.ParentID, "parent-id")
	}
	if loaded.Identity.Depth != 1 {
		t.Fatalf("loaded Identity.Depth = %d, want 1", loaded.Identity.Depth)
	}
}

func TestSaveLoadSessionDocRecursiveChild(t *testing.T) {
	root := t.TempDir()
	parentDir := filepath.Join(root, "parent-session")
	childDir := ChildSessionDir(parentDir, "child-1")
	grandchildDir := ChildSessionDir(childDir, "grandchild-1")

	// Save root
	rootDoc := NewSessionDoc(SessionConfig{})
	if err := SaveSessionDoc(parentDir, rootDoc, nil); err != nil {
		t.Fatalf("save root: %v", err)
	}

	// Save child
	childDoc := NewSessionDoc(SessionConfig{})
	childDoc.Identity = SessionIdentity{ID: "child", ParentID: rootDoc.Identity.ID, RootID: rootDoc.Identity.ID, Depth: 1}
	if err := SaveSessionDoc(childDir, childDoc, nil); err != nil {
		t.Fatalf("save child: %v", err)
	}

	// Save grandchild
	gcDoc := NewSessionDoc(SessionConfig{})
	gcDoc.Identity = SessionIdentity{ID: "gc", ParentID: "child", RootID: rootDoc.Identity.ID, Depth: 2}
	if err := SaveSessionDoc(grandchildDir, gcDoc, nil); err != nil {
		t.Fatalf("save grandchild: %v", err)
	}

	// Verify paths
	if _, err := os.Stat(filepath.Join(parentDir, "chat.json")); err != nil {
		t.Fatalf("root chat.json missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(childDir, "chat.json")); err != nil {
		t.Fatalf("child chat.json missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(grandchildDir, "chat.json")); err != nil {
		t.Fatalf("grandchild chat.json missing: %v", err)
	}

	// Load grandchild
	loaded, err := LoadSessionDoc(grandchildDir)
	if err != nil {
		t.Fatalf("LoadSessionDoc grandchild: %v", err)
	}
	if loaded.Identity.Depth != 2 {
		t.Fatalf("grandchild depth = %d, want 2", loaded.Identity.Depth)
	}
}

func TestResolvedDirectorySaveLoad(t *testing.T) {
	root := t.TempDir()
	p := Paths{Sessions: root}
	sessionDir := RootSessionDir(p, "resolved-directory-test")

	doc := NewSessionDoc(SessionConfig{})
	if err := SaveSessionDoc(sessionDir, doc, nil); err != nil {
		t.Fatalf("SaveSessionDoc failed: %v", err)
	}

	path := SessionFilePath(sessionDir)
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("chat.json not at expected path %q: %v", path, err)
	}

	loaded, err := LoadSessionDoc(sessionDir)
	if err != nil {
		t.Fatalf("LoadSessionDoc failed: %v", err)
	}
	if loaded.Identity.ID != doc.Identity.ID {
		t.Fatalf("loaded ID mismatch: %q != %q", loaded.Identity.ID, doc.Identity.ID)
	}
}

func TestNestedSaveDoesNotTouchGlobalSessions(t *testing.T) {
	// SaveSessionDoc on a child directory should not write into Paths.Sessions
	globalSessions := t.TempDir()
	p := Paths{Sessions: globalSessions}

	// Use a temp dir to avoid collisions
	tmpParent := t.TempDir()
	tmpChildDir := ChildSessionDir(tmpParent, "child")

	doc := NewSessionDoc(SessionConfig{})
	if err := SaveSessionDoc(tmpChildDir, doc, nil); err != nil {
		t.Fatalf("SaveSessionDoc failed: %v", err)
	}

	// Global sessions dir should be empty
	entries, err := os.ReadDir(globalSessions)
	if err != nil {
		t.Fatalf("ReadDir global sessions: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("global sessions dir should be empty, got %d entries", len(entries))
	}

	// Child dir should have the file
	if _, err := os.Stat(SessionFilePath(tmpChildDir)); err != nil {
		t.Fatalf("child chat.json not found: %v", err)
	}

	// p is unused except to prove globalSessions stays untouched
	_ = p
}

// --- Task 3.3: LoadChildSession ---

func TestLoadChildSessionSuccess(t *testing.T) {
	root := t.TempDir()
	parentDir := filepath.Join(root, "parent-session")

	// Create parent doc
	parentDoc := NewSessionDoc(SessionConfig{})
	parentDoc.Identity = SessionIdentity{ID: "parent-id", RootID: "parent-id", Depth: 0}
	if err := SaveSessionDoc(parentDir, parentDoc, nil); err != nil {
		t.Fatalf("save parent: %v", err)
	}

	// Create child doc
	childName := "trader-tc-123"
	childDir := ChildSessionDir(parentDir, childName)
	childDoc := NewSessionDoc(SessionConfig{})
	childDoc.Identity = SessionIdentity{
		ID:               "child-id",
		ParentID:         "parent-id",
		RootID:           "parent-id",
		ParentToolCallID: "tc-123",
		Depth:            1,
	}
	if err := SaveSessionDoc(childDir, childDoc, nil); err != nil {
		t.Fatalf("save child: %v", err)
	}

	// Create tool call with child link
	toolCall := ToolCallEntry{
		ID: "tc-123",
	}
	toolCall.Instruction.Name = "call_agent"
	toolCall.Execution.ChildSessionID = "child-id"
	toolCall.Execution.ChildSessionName = childName

	// Load child via parent link
	loaded, resolvedDir, err := LoadChildSession(parentDir, parentDoc, toolCall)
	if err != nil {
		t.Fatalf("LoadChildSession failed: %v", err)
	}

	if loaded.Identity.ID != "child-id" {
		t.Fatalf("loaded child ID = %q, want %q", loaded.Identity.ID, "child-id")
	}
	if loaded.Identity.ParentID != "parent-id" {
		t.Fatalf("loaded child ParentID = %q, want %q", loaded.Identity.ParentID, "parent-id")
	}
	if loaded.Identity.RootID != "parent-id" {
		t.Fatalf("loaded child RootID = %q, want %q", loaded.Identity.RootID, "parent-id")
	}
	if loaded.Identity.Depth != 1 {
		t.Fatalf("loaded child Depth = %d, want 1", loaded.Identity.Depth)
	}
	if resolvedDir != childDir {
		t.Fatalf("resolved childDir = %q, want %q", resolvedDir, childDir)
	}
}

func TestLoadChildSessionMissingChildName(t *testing.T) {
	parentDoc := NewSessionDoc(SessionConfig{})
	toolCall := ToolCallEntry{ID: "tc-1"}
	toolCall.Instruction.Name = "read_file"
	// No child fields set

	_, _, err := LoadChildSession("/tmp/parent", parentDoc, toolCall)
	if err == nil {
		t.Fatal("expected error for missing child name, got nil")
	}
	if !strings.Contains(err.Error(), "no child session name") {
		t.Fatalf("unexpected error message: %v", err)
	}
}

func TestLoadChildSessionFileAbsent(t *testing.T) {
	parentDoc := NewSessionDoc(SessionConfig{})
	toolCall := ToolCallEntry{ID: "tc-1"}
	toolCall.Execution.ChildSessionName = "missing-child"

	_, _, err := LoadChildSession("/tmp/nonexistent-parent", parentDoc, toolCall)
	if err == nil {
		t.Fatal("expected error for absent child file, got nil")
	}
}

func TestLoadChildSessionChildIDMismatch(t *testing.T) {
	root := t.TempDir()
	parentDir := filepath.Join(root, "parent-session")

	parentDoc := NewSessionDoc(SessionConfig{})
	parentDoc.Identity = SessionIdentity{ID: "parent-id", RootID: "parent-id", Depth: 0}

	childName := "trader-tc-123"
	childDir := ChildSessionDir(parentDir, childName)
	childDoc := NewSessionDoc(SessionConfig{})
	childDoc.Identity = SessionIdentity{
		ID:               "wrong-child-id", // mismatch
		ParentID:         "parent-id",
		RootID:           "parent-id",
		ParentToolCallID: "tc-123",
		Depth:            1,
	}
	if err := SaveSessionDoc(childDir, childDoc, nil); err != nil {
		t.Fatalf("save child: %v", err)
	}

	toolCall := ToolCallEntry{ID: "tc-123"}
	toolCall.Execution.ChildSessionID = "child-id"
	toolCall.Execution.ChildSessionName = childName

	_, _, err := LoadChildSession(parentDir, parentDoc, toolCall)
	if err == nil {
		t.Fatal("expected error for child ID mismatch, got nil")
	}
	if !strings.Contains(err.Error(), "child ID mismatch") {
		t.Fatalf("unexpected error message: %v", err)
	}
}

func TestLoadChildSessionParentIDMismatch(t *testing.T) {
	root := t.TempDir()
	parentDir := filepath.Join(root, "parent-session")

	parentDoc := NewSessionDoc(SessionConfig{})
	parentDoc.Identity = SessionIdentity{ID: "parent-id", RootID: "parent-id", Depth: 0}

	childName := "trader-tc-123"
	childDir := ChildSessionDir(parentDir, childName)
	childDoc := NewSessionDoc(SessionConfig{})
	childDoc.Identity = SessionIdentity{
		ID:               "child-id",
		ParentID:         "wrong-parent-id", // mismatch
		RootID:           "parent-id",
		ParentToolCallID: "tc-123",
		Depth:            1,
	}
	if err := SaveSessionDoc(childDir, childDoc, nil); err != nil {
		t.Fatalf("save child: %v", err)
	}

	toolCall := ToolCallEntry{ID: "tc-123"}
	toolCall.Execution.ChildSessionID = "child-id"
	toolCall.Execution.ChildSessionName = childName

	_, _, err := LoadChildSession(parentDir, parentDoc, toolCall)
	if err == nil {
		t.Fatal("expected error for ParentID mismatch, got nil")
	}
	if !strings.Contains(err.Error(), "ParentID mismatch") {
		t.Fatalf("unexpected error message: %v", err)
	}
}

func TestLoadChildSessionRootIDMismatch(t *testing.T) {
	root := t.TempDir()
	parentDir := filepath.Join(root, "parent-session")

	parentDoc := NewSessionDoc(SessionConfig{})
	parentDoc.Identity = SessionIdentity{ID: "parent-id", RootID: "parent-root-id", Depth: 0}

	childName := "trader-tc-123"
	childDir := ChildSessionDir(parentDir, childName)
	childDoc := NewSessionDoc(SessionConfig{})
	childDoc.Identity = SessionIdentity{
		ID:               "child-id",
		ParentID:         "parent-id",
		RootID:           "wrong-root-id", // mismatch
		ParentToolCallID: "tc-123",
		Depth:            1,
	}
	if err := SaveSessionDoc(childDir, childDoc, nil); err != nil {
		t.Fatalf("save child: %v", err)
	}

	toolCall := ToolCallEntry{ID: "tc-123"}
	toolCall.Execution.ChildSessionID = "child-id"
	toolCall.Execution.ChildSessionName = childName

	_, _, err := LoadChildSession(parentDir, parentDoc, toolCall)
	if err == nil {
		t.Fatal("expected error for RootID mismatch, got nil")
	}
	if !strings.Contains(err.Error(), "RootID mismatch") {
		t.Fatalf("unexpected error message: %v", err)
	}
}

func TestLoadChildSessionParentToolCallIDMismatch(t *testing.T) {
	root := t.TempDir()
	parentDir := filepath.Join(root, "parent-session")

	parentDoc := NewSessionDoc(SessionConfig{})
	parentDoc.Identity = SessionIdentity{ID: "parent-id", RootID: "parent-id", Depth: 0}

	childName := "trader-tc-123"
	childDir := ChildSessionDir(parentDir, childName)
	childDoc := NewSessionDoc(SessionConfig{})
	childDoc.Identity = SessionIdentity{
		ID:               "child-id",
		ParentID:         "parent-id",
		RootID:           "parent-id",
		ParentToolCallID: "tc-999", // mismatch
		Depth:            1,
	}
	if err := SaveSessionDoc(childDir, childDoc, nil); err != nil {
		t.Fatalf("save child: %v", err)
	}

	toolCall := ToolCallEntry{ID: "tc-123"}
	toolCall.Execution.ChildSessionID = "child-id"
	toolCall.Execution.ChildSessionName = childName

	_, _, err := LoadChildSession(parentDir, parentDoc, toolCall)
	if err == nil {
		t.Fatal("expected error for ParentToolCallID mismatch, got nil")
	}
	if !strings.Contains(err.Error(), "ParentToolCallID mismatch") {
		t.Fatalf("unexpected error message: %v", err)
	}
}

func TestLoadChildSessionDepthMismatch(t *testing.T) {
	root := t.TempDir()
	parentDir := filepath.Join(root, "parent-session")

	parentDoc := NewSessionDoc(SessionConfig{})
	parentDoc.Identity = SessionIdentity{ID: "parent-id", RootID: "parent-id", Depth: 0}

	childName := "trader-tc-123"
	childDir := ChildSessionDir(parentDir, childName)
	childDoc := NewSessionDoc(SessionConfig{})
	childDoc.Identity = SessionIdentity{
		ID:               "child-id",
		ParentID:         "parent-id",
		RootID:           "parent-id",
		ParentToolCallID: "tc-123",
		Depth:            5, // wrong depth; should be 1
	}
	if err := SaveSessionDoc(childDir, childDoc, nil); err != nil {
		t.Fatalf("save child: %v", err)
	}

	toolCall := ToolCallEntry{ID: "tc-123"}
	toolCall.Execution.ChildSessionID = "child-id"
	toolCall.Execution.ChildSessionName = childName

	_, _, err := LoadChildSession(parentDir, parentDoc, toolCall)
	if err == nil {
		t.Fatal("expected error for depth mismatch, got nil")
	}
	if !strings.Contains(err.Error(), "Depth mismatch") {
		t.Fatalf("unexpected error message: %v", err)
	}
}

func TestLoadChildSessionGrandchildRecursive(t *testing.T) {
	root := t.TempDir()
	parentDir := filepath.Join(root, "parent-session")

	// Create parent doc (depth 0)
	parentDoc := NewSessionDoc(SessionConfig{})
	parentDoc.Identity = SessionIdentity{ID: "parent-id", RootID: "root-id", Depth: 0}
	if err := SaveSessionDoc(parentDir, parentDoc, nil); err != nil {
		t.Fatalf("save parent: %v", err)
	}

	// Create child doc (depth 1)
	childName := "trader-tc-1"
	childDir := ChildSessionDir(parentDir, childName)
	childDoc := NewSessionDoc(SessionConfig{})
	childDoc.Identity = SessionIdentity{
		ID:               "child-id",
		ParentID:         "parent-id",
		RootID:           "root-id",
		ParentToolCallID: "tc-1",
		Depth:            1,
	}
	if err := SaveSessionDoc(childDir, childDoc, nil); err != nil {
		t.Fatalf("save child: %v", err)
	}

	// Create grandchild doc (depth 2)
	grandchildName := "inline-tc-2"
	grandchildDir := ChildSessionDir(childDir, grandchildName)
	grandchildDoc := NewSessionDoc(SessionConfig{})
	grandchildDoc.Identity = SessionIdentity{
		ID:               "grandchild-id",
		ParentID:         "child-id",
		RootID:           "root-id",
		ParentToolCallID: "tc-2",
		Depth:            2,
	}
	if err := SaveSessionDoc(grandchildDir, grandchildDoc, nil); err != nil {
		t.Fatalf("save grandchild: %v", err)
	}

	// Load grandchild via child's tool call
	childToolCall := ToolCallEntry{ID: "tc-2"}
	childToolCall.Execution.ChildSessionID = "grandchild-id"
	childToolCall.Execution.ChildSessionName = grandchildName

	loaded, resolvedDir, err := LoadChildSession(childDir, childDoc, childToolCall)
	if err != nil {
		t.Fatalf("LoadChildSession grandchild failed: %v", err)
	}

	if loaded.Identity.ID != "grandchild-id" {
		t.Fatalf("loaded grandchild ID = %q, want %q", loaded.Identity.ID, "grandchild-id")
	}
	if loaded.Identity.Depth != 2 {
		t.Fatalf("loaded grandchild Depth = %d, want 2", loaded.Identity.Depth)
	}
	if resolvedDir != grandchildDir {
		t.Fatalf("resolved grandchildDir = %q, want %q", resolvedDir, grandchildDir)
	}
}

func TestLoadChildSessionDoesNotScanGlobalSessions(t *testing.T) {
	// Verify that LoadChildSession resolves via parentDir, not via global sessions.
	// It should not call ListSessions or scan the global sessions directory.
	root := t.TempDir()
	parentDir := filepath.Join(root, "isolated-parent")

	parentDoc := NewSessionDoc(SessionConfig{})
	parentDoc.Identity = SessionIdentity{ID: "p", RootID: "p", Depth: 0}

	childName := "trader-tc-1"
	childDir := ChildSessionDir(parentDir, childName)
	childDoc := NewSessionDoc(SessionConfig{})
	childDoc.Identity = SessionIdentity{
		ID:               "c",
		ParentID:         "p",
		RootID:           "p",
		ParentToolCallID: "tc-1",
		Depth:            1,
	}
	if err := SaveSessionDoc(childDir, childDoc, nil); err != nil {
		t.Fatalf("save child: %v", err)
	}

	toolCall := ToolCallEntry{ID: "tc-1"}
	toolCall.Execution.ChildSessionID = "c"
	toolCall.Execution.ChildSessionName = childName

	// LoadChildSession uses parentDir directly — it never touches a global sessions path.
	loaded, _, err := LoadChildSession(parentDir, parentDoc, toolCall)
	if err != nil {
		t.Fatalf("LoadChildSession failed: %v", err)
	}
	if loaded.Identity.ID != "c" {
		t.Fatalf("loaded ID = %q, want %q", loaded.Identity.ID, "c")
	}
}

// --- Task 4.1: ForkSessionTree ---

func TestForkSessionTreeCopiesRootOnly(t *testing.T) {
	sourceDir := filepath.Join(t.TempDir(), "source-session")
	destDir := filepath.Join(t.TempDir(), "dest-session")

	// Create source root
	sourceDoc := NewSessionDoc(SessionConfig{})
	sourceDoc.Identity = SessionIdentity{ID: "src-root", RootID: "src-root", Depth: 0}
	sourceDoc.Messages = []Message{{ID: "m1", Role: RoleUser, Text: "hello"}}
	if err := SaveSessionDoc(sourceDir, sourceDoc, nil); err != nil {
		t.Fatalf("save source: %v", err)
	}

	result, err := ForkSessionTree(sourceDir, destDir)
	if err != nil {
		t.Fatalf("ForkSessionTree failed: %v", err)
	}

	// Verify new root ID is generated
	if result.RootIdentity.ID == "" {
		t.Fatal("forked root ID is empty")
	}
	if result.RootIdentity.RootID != result.RootIdentity.ID {
		t.Fatalf("forked root RootID %q != ID %q", result.RootIdentity.RootID, result.RootIdentity.ID)
	}
	if result.RootIdentity.Depth != 0 {
		t.Fatalf("forked root Depth = %d, want 0", result.RootIdentity.Depth)
	}

	// Verify forked document exists
	forkedDoc, err := LoadSessionDoc(destDir)
	if err != nil {
		t.Fatalf("load forked: %v", err)
	}

	// New root ID should differ from source
	if forkedDoc.Identity.ID == sourceDoc.Identity.ID {
		t.Fatal("forked root should have a new ID")
	}
	if forkedDoc.Identity.ParentID != "" {
		t.Fatalf("forked root ParentID should be empty, got %q", forkedDoc.Identity.ParentID)
	}

	// Messages should be preserved
	if len(forkedDoc.Messages) != 1 || forkedDoc.Messages[0].Text != "hello" {
		t.Fatalf("forked messages not preserved: %+v", forkedDoc.Messages)
	}

	// Source should be unchanged
	loadedSource, err := LoadSessionDoc(sourceDir)
	if err != nil {
		t.Fatalf("load source after fork: %v", err)
	}
	if loadedSource.Identity.ID != "src-root" {
		t.Fatalf("source ID changed after fork: %q", loadedSource.Identity.ID)
	}
}

func TestForkSessionTreeCopiesRecursiveChildren(t *testing.T) {
	sourceDir := filepath.Join(t.TempDir(), "source-session")
	destDir := filepath.Join(t.TempDir(), "dest-session")

	// Create source root
	rootDoc := NewSessionDoc(SessionConfig{})
	rootDoc.Identity = SessionIdentity{ID: "root", RootID: "root", Depth: 0}
	if err := SaveSessionDoc(sourceDir, rootDoc, nil); err != nil {
		t.Fatalf("save root: %v", err)
	}

	// Create child
	childName := "trader-tc-1"
	childDir := ChildSessionDir(sourceDir, childName)
	childDoc := NewSessionDoc(SessionConfig{})
	childDoc.Identity = SessionIdentity{ID: "child", ParentID: "root", RootID: "root", ParentToolCallID: "tc-1", Depth: 1}
	if err := SaveSessionDoc(childDir, childDoc, nil); err != nil {
		t.Fatalf("save child: %v", err)
	}

	// Create grandchild
	gcName := "inline-tc-2"
	gcDir := ChildSessionDir(childDir, gcName)
	gcDoc := NewSessionDoc(SessionConfig{})
	gcDoc.Identity = SessionIdentity{ID: "grandchild", ParentID: "child", RootID: "root", ParentToolCallID: "tc-2", Depth: 2}
	if err := SaveSessionDoc(gcDir, gcDoc, nil); err != nil {
		t.Fatalf("save grandchild: %v", err)
	}

	result, err := ForkSessionTree(sourceDir, destDir)
	if err != nil {
		t.Fatalf("ForkSessionTree failed: %v", err)
	}

	// IDMap should have 3 entries
	if len(result.IDMap) != 3 {
		t.Fatalf("IDMap has %d entries, want 3", len(result.IDMap))
	}

	// Verify directory layout
	forkedChildDir := ChildSessionDir(destDir, childName)
	forkedGCDir := ChildSessionDir(forkedChildDir, gcName)

	if _, err := os.Stat(SessionFilePath(forkedChildDir)); err != nil {
		t.Fatalf("forked child not found at %s: %v", SessionFilePath(forkedChildDir), err)
	}
	if _, err := os.Stat(SessionFilePath(forkedGCDir)); err != nil {
		t.Fatalf("forked grandchild not found at %s: %v", SessionFilePath(forkedGCDir), err)
	}

	// Load forked child
	forkedChild, err := LoadSessionDoc(forkedChildDir)
	if err != nil {
		t.Fatalf("load forked child: %v", err)
	}
	if forkedChild.Identity.ParentID != result.IDMap["root"] {
		t.Fatalf("forked child ParentID %q != mapped root ID %q", forkedChild.Identity.ParentID, result.IDMap["root"])
	}
	if forkedChild.Identity.RootID != result.RootIdentity.ID {
		t.Fatalf("forked child RootID %q != new root ID %q", forkedChild.Identity.RootID, result.RootIdentity.ID)
	}
	if forkedChild.Identity.Depth != 1 {
		t.Fatalf("forked child Depth = %d, want 1", forkedChild.Identity.Depth)
	}
	// ParentToolCallID should be preserved (unchanged)
	if forkedChild.Identity.ParentToolCallID != "tc-1" {
		t.Fatalf("forked child ParentToolCallID changed: %q", forkedChild.Identity.ParentToolCallID)
	}

	// Load forked grandchild
	forkedGC, err := LoadSessionDoc(forkedGCDir)
	if err != nil {
		t.Fatalf("load forked grandchild: %v", err)
	}
	if forkedGC.Identity.ParentID != result.IDMap["child"] {
		t.Fatalf("forked grandchild ParentID %q != mapped child ID %q", forkedGC.Identity.ParentID, result.IDMap["child"])
	}
	if forkedGC.Identity.RootID != result.RootIdentity.ID {
		t.Fatalf("forked grandchild RootID %q != new root ID %q", forkedGC.Identity.RootID, result.RootIdentity.ID)
	}
	if forkedGC.Identity.Depth != 2 {
		t.Fatalf("forked grandchild Depth = %d, want 2", forkedGC.Identity.Depth)
	}
}

func TestForkSessionTreeRemapsToolExecutionChildLinks(t *testing.T) {
	sourceDir := filepath.Join(t.TempDir(), "source-session")
	destDir := filepath.Join(t.TempDir(), "dest-session")

	// Create source root with a tool call that references a child
	rootDoc := NewSessionDoc(SessionConfig{})
	rootDoc.Identity = SessionIdentity{ID: "root", RootID: "root", Depth: 0}
	rootDoc.Messages = []Message{
		{ID: "m1", Role: RoleAssistant, ToolCalls: []ToolCallEntry{
			{
				ID: "tc-1",
			},
		}},
	}
	rootDoc.Messages[0].ToolCalls[0].Instruction.Name = "call_agent"
	rootDoc.Messages[0].ToolCalls[0].Execution.ChildSessionID = "child-id"
	rootDoc.Messages[0].ToolCalls[0].Execution.ChildSessionName = "trader-tc-1"
	if err := SaveSessionDoc(sourceDir, rootDoc, nil); err != nil {
		t.Fatalf("save root: %v", err)
	}

	// Create child
	childDir := ChildSessionDir(sourceDir, "trader-tc-1")
	childDoc := NewSessionDoc(SessionConfig{})
	childDoc.Identity = SessionIdentity{ID: "child-id", ParentID: "root", RootID: "root", ParentToolCallID: "tc-1", Depth: 1}
	if err := SaveSessionDoc(childDir, childDoc, nil); err != nil {
		t.Fatalf("save child: %v", err)
	}

	result, err := ForkSessionTree(sourceDir, destDir)
	if err != nil {
		t.Fatalf("ForkSessionTree failed: %v", err)
	}

	// Load forked root and check child link is remapped
	forkedRoot, err := LoadSessionDoc(destDir)
	if err != nil {
		t.Fatalf("load forked root: %v", err)
	}
	tc := forkedRoot.Messages[0].ToolCalls[0]
	if tc.Execution.ChildSessionID != result.IDMap["child-id"] {
		t.Fatalf("ChildSessionID not remapped: got %q, want %q", tc.Execution.ChildSessionID, result.IDMap["child-id"])
	}
	// ChildSessionName should be preserved
	if tc.Execution.ChildSessionName != "trader-tc-1" {
		t.Fatalf("ChildSessionName changed: %q", tc.Execution.ChildSessionName)
	}
}

func TestForkSessionTreePreservesChildNames(t *testing.T) {
	sourceDir := filepath.Join(t.TempDir(), "source-session")
	destDir := filepath.Join(t.TempDir(), "dest-session")

	rootDoc := NewSessionDoc(SessionConfig{})
	rootDoc.Identity = SessionIdentity{ID: "root", RootID: "root", Depth: 0}
	rootDoc.Messages = []Message{
		{ID: "m1", Role: RoleAssistant, ToolCalls: []ToolCallEntry{
			{ID: "tc-1"},
			{ID: "tc-2"},
		}},
	}
	rootDoc.Messages[0].ToolCalls[0].Instruction.Name = "call_agent"
	rootDoc.Messages[0].ToolCalls[0].Execution.ChildSessionID = "child1"
	rootDoc.Messages[0].ToolCalls[0].Execution.ChildSessionName = "trader-tc-1"
	rootDoc.Messages[0].ToolCalls[1].Instruction.Name = "call_agent"
	rootDoc.Messages[0].ToolCalls[1].Execution.ChildSessionID = "child2"
	rootDoc.Messages[0].ToolCalls[1].Execution.ChildSessionName = "inline-tc-2"
	if err := SaveSessionDoc(sourceDir, rootDoc, nil); err != nil {
		t.Fatalf("save root: %v", err)
	}

	// Create two children
	for _, pair := range [][2]string{{"trader-tc-1", "child1"}, {"inline-tc-2", "child2"}} {
		cDir := ChildSessionDir(sourceDir, pair[0])
		cDoc := NewSessionDoc(SessionConfig{})
		cDoc.Identity = SessionIdentity{ID: pair[1], ParentID: "root", RootID: "root", Depth: 1}
		if err := SaveSessionDoc(cDir, cDoc, nil); err != nil {
			t.Fatalf("save child %s: %v", pair[0], err)
		}
	}

	result, err := ForkSessionTree(sourceDir, destDir)
	if err != nil {
		t.Fatalf("ForkSessionTree failed: %v", err)
	}

	// Verify both child names are preserved in directory structure
	for _, name := range []string{"trader-tc-1", "inline-tc-2"} {
		cDir := ChildSessionDir(destDir, name)
		if _, err := os.Stat(SessionFilePath(cDir)); err != nil {
			t.Fatalf("forked child %q not found at %s: %v", name, SessionFilePath(cDir), err)
		}
	}

	// Load forked root and check names preserved
	forkedRoot, err := LoadSessionDoc(destDir)
	if err != nil {
		t.Fatalf("load forked root: %v", err)
	}
	if forkedRoot.Messages[0].ToolCalls[0].Execution.ChildSessionName != "trader-tc-1" {
		t.Fatalf("ChildSessionName 0 changed: %q", forkedRoot.Messages[0].ToolCalls[0].Execution.ChildSessionName)
	}
	if forkedRoot.Messages[0].ToolCalls[1].Execution.ChildSessionName != "inline-tc-2" {
		t.Fatalf("ChildSessionName 1 changed: %q", forkedRoot.Messages[0].ToolCalls[1].Execution.ChildSessionName)
	}

	// Verify all 3 IDs are in the map (root + 2 children)
	if len(result.IDMap) != 3 {
		t.Fatalf("IDMap has %d entries, want 3", len(result.IDMap))
	}
}

func TestForkSessionTreePreservesMessages(t *testing.T) {
	sourceDir := filepath.Join(t.TempDir(), "source-session")
	destDir := filepath.Join(t.TempDir(), "dest-session")

	rootDoc := NewSessionDoc(SessionConfig{})
	rootDoc.Identity = SessionIdentity{ID: "root", RootID: "root", Depth: 0}
	rootDoc.Messages = []Message{
		{ID: "m1", Role: RoleUser, Text: "user message"},
		{ID: "m2", Role: RoleAssistant, Text: "assistant reply"},
	}
	if err := SaveSessionDoc(sourceDir, rootDoc, nil); err != nil {
		t.Fatalf("save source: %v", err)
	}

	_, err := ForkSessionTree(sourceDir, destDir)
	if err != nil {
		t.Fatalf("ForkSessionTree failed: %v", err)
	}

	forked, err := LoadSessionDoc(destDir)
	if err != nil {
		t.Fatalf("load forked: %v", err)
	}

	if len(forked.Messages) != 2 {
		t.Fatalf("forked message count = %d, want 2", len(forked.Messages))
	}
	if forked.Messages[0].Text != "user message" {
		t.Fatalf("forked message 0 text = %q", forked.Messages[0].Text)
	}
	if forked.Messages[1].Text != "assistant reply" {
		t.Fatalf("forked message 1 text = %q", forked.Messages[1].Text)
	}
}

func TestForkSessionTreePreservesDepth(t *testing.T) {
	sourceDir := filepath.Join(t.TempDir(), "source-session")
	destDir := filepath.Join(t.TempDir(), "dest-session")

	// Root (depth 0)
	rootDoc := NewSessionDoc(SessionConfig{})
	rootDoc.Identity = SessionIdentity{ID: "root", RootID: "root", Depth: 0}
	if err := SaveSessionDoc(sourceDir, rootDoc, nil); err != nil {
		t.Fatalf("save root: %v", err)
	}

	// Child (depth 1)
	childDir := ChildSessionDir(sourceDir, "c1")
	childDoc := NewSessionDoc(SessionConfig{})
	childDoc.Identity = SessionIdentity{ID: "c1", ParentID: "root", RootID: "root", Depth: 1}
	if err := SaveSessionDoc(childDir, childDoc, nil); err != nil {
		t.Fatalf("save child: %v", err)
	}

	// Grandchild (depth 2)
	gcDir := ChildSessionDir(childDir, "gc1")
	gcDoc := NewSessionDoc(SessionConfig{})
	gcDoc.Identity = SessionIdentity{ID: "gc1", ParentID: "c1", RootID: "root", Depth: 2}
	if err := SaveSessionDoc(gcDir, gcDoc, nil); err != nil {
		t.Fatalf("save grandchild: %v", err)
	}

	_, err := ForkSessionTree(sourceDir, destDir)
	if err != nil {
		t.Fatalf("ForkSessionTree failed: %v", err)
	}

	forkedRoot, err := LoadSessionDoc(destDir)
	if err != nil {
		t.Fatalf("load forked root: %v", err)
	}
	if forkedRoot.Identity.Depth != 0 {
		t.Fatalf("forked root Depth = %d, want 0", forkedRoot.Identity.Depth)
	}

	forkedChild, err := LoadSessionDoc(ChildSessionDir(destDir, "c1"))
	if err != nil {
		t.Fatalf("load forked child: %v", err)
	}
	if forkedChild.Identity.Depth != 1 {
		t.Fatalf("forked child Depth = %d, want 1", forkedChild.Identity.Depth)
	}

	forkedGC, err := LoadSessionDoc(ChildSessionDir(ChildSessionDir(destDir, "c1"), "gc1"))
	if err != nil {
		t.Fatalf("load forked grandchild: %v", err)
	}
	if forkedGC.Identity.Depth != 2 {
		t.Fatalf("forked grandchild Depth = %d, want 2", forkedGC.Identity.Depth)
	}
}

func TestForkSessionTreeNoDuplicateIDsInFork(t *testing.T) {
	sourceDir := filepath.Join(t.TempDir(), "source-session")
	destDir := filepath.Join(t.TempDir(), "dest-session")

	rootDoc := NewSessionDoc(SessionConfig{})
	rootDoc.Identity = SessionIdentity{ID: "root", RootID: "root", Depth: 0}
	if err := SaveSessionDoc(sourceDir, rootDoc, nil); err != nil {
		t.Fatalf("save root: %v", err)
	}

	childDir := ChildSessionDir(sourceDir, "c1")
	childDoc := NewSessionDoc(SessionConfig{})
	childDoc.Identity = SessionIdentity{ID: "c1", ParentID: "root", RootID: "root", Depth: 1}
	if err := SaveSessionDoc(childDir, childDoc, nil); err != nil {
		t.Fatalf("save child: %v", err)
	}

	result, err := ForkSessionTree(sourceDir, destDir)
	if err != nil {
		t.Fatalf("ForkSessionTree failed: %v", err)
	}

	// All new IDs should be unique
	seen := make(map[string]bool)
	for _, newID := range result.IDMap {
		if seen[newID] {
			t.Fatalf("duplicate ID in fork: %q", newID)
		}
		seen[newID] = true
	}
}

func TestForkSessionTreeRejectsDanglingChildLinkWithoutDestination(t *testing.T) {
	sourceDir := filepath.Join(t.TempDir(), "source-session")
	destinationDir := filepath.Join(t.TempDir(), "destination-session")

	root := NewSessionDoc(SessionConfig{})
	root.Messages = []Message{{ID: "m1", Role: RoleAssistant, ToolCalls: []ToolCallEntry{{ID: "tc-1"}}}}
	root.Messages[0].ToolCalls[0].Execution.ChildSessionID = "missing-child"
	root.Messages[0].ToolCalls[0].Execution.ChildSessionName = "missing-child"
	if err := SaveSessionDoc(sourceDir, root, nil); err != nil {
		t.Fatal(err)
	}

	if _, err := ForkSessionTree(sourceDir, destinationDir); err == nil {
		t.Fatal("expected dangling child link error")
	}
	if _, err := os.Stat(destinationDir); !os.IsNotExist(err) {
		t.Fatalf("failed fork left destination behind: %v", err)
	}
}

func TestForkSessionTreeFailsOnMissingSource(t *testing.T) {
	_, err := ForkSessionTree("/nonexistent/source", "/some/dest")
	if err == nil {
		t.Fatal("expected error for missing source, got nil")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestForkSessionTreeFailsOnDestinationCollision(t *testing.T) {
	sourceDir := filepath.Join(t.TempDir(), "source-session")
	destDir := filepath.Join(t.TempDir(), "dest-session")

	// Create source
	sourceDoc := NewSessionDoc(SessionConfig{})
	if err := SaveSessionDoc(sourceDir, sourceDoc, nil); err != nil {
		t.Fatalf("save source: %v", err)
	}

	// Create destination (collision)
	destDoc := NewSessionDoc(SessionConfig{})
	if err := SaveSessionDoc(destDir, destDoc, nil); err != nil {
		t.Fatalf("save dest: %v", err)
	}

	_, err := ForkSessionTree(sourceDir, destDir)
	if err == nil {
		t.Fatal("expected error for destination collision, got nil")
	}
	if !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestForkSessionTreeFailsOnDuplicateSourceIDs(t *testing.T) {
	sourceDir := filepath.Join(t.TempDir(), "source-session")
	destDir := filepath.Join(t.TempDir(), "dest-session")

	// Create root with a manually crafted duplicate ID scenario
	rootDoc := NewSessionDoc(SessionConfig{})
	rootDoc.Identity = SessionIdentity{ID: "same-id", RootID: "same-id", Depth: 0}
	if err := SaveSessionDoc(sourceDir, rootDoc, nil); err != nil {
		t.Fatalf("save root: %v", err)
	}

	// Create child with same ID as root
	childDir := ChildSessionDir(sourceDir, "c1")
	childDoc := NewSessionDoc(SessionConfig{})
	childDoc.Identity = SessionIdentity{ID: "same-id", ParentID: "same-id", RootID: "same-id", Depth: 1}
	if err := SaveSessionDoc(childDir, childDoc, nil); err != nil {
		t.Fatalf("save child: %v", err)
	}

	_, err := ForkSessionTree(sourceDir, destDir)
	if err == nil {
		t.Fatal("expected error for duplicate source IDs, got nil")
	}
	if !strings.Contains(err.Error(), "duplicate ID") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestForkSessionTreeFailsOnBrokenLineage(t *testing.T) {
	sourceDir := filepath.Join(t.TempDir(), "source-session")
	destDir := filepath.Join(t.TempDir(), "dest-session")

	// Create root
	rootDoc := NewSessionDoc(SessionConfig{})
	rootDoc.Identity = SessionIdentity{ID: "root", RootID: "root", Depth: 0}
	if err := SaveSessionDoc(sourceDir, rootDoc, nil); err != nil {
		t.Fatalf("save root: %v", err)
	}

	// Create child whose ParentID references an ID not in the tree
	childDir := ChildSessionDir(sourceDir, "c1")
	childDoc := NewSessionDoc(SessionConfig{})
	childDoc.Identity = SessionIdentity{ID: "child", ParentID: "nonexistent-parent", RootID: "root", Depth: 1}
	if err := SaveSessionDoc(childDir, childDoc, nil); err != nil {
		t.Fatalf("save child: %v", err)
	}

	_, err := ForkSessionTree(sourceDir, destDir)
	if err == nil {
		t.Fatal("expected error for broken lineage, got nil")
	}
	if !strings.Contains(err.Error(), "broken lineage") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestForkSessionTreeIndependentFromSource(t *testing.T) {
	sourceDir := filepath.Join(t.TempDir(), "source-session")
	destDir := filepath.Join(t.TempDir(), "dest-session")

	// Create source tree
	rootDoc := NewSessionDoc(SessionConfig{})
	rootDoc.Identity = SessionIdentity{ID: "root", RootID: "root", Depth: 0}
	if err := SaveSessionDoc(sourceDir, rootDoc, nil); err != nil {
		t.Fatalf("save root: %v", err)
	}

	childDir := ChildSessionDir(sourceDir, "c1")
	childDoc := NewSessionDoc(SessionConfig{})
	childDoc.Identity = SessionIdentity{ID: "child", ParentID: "root", RootID: "root", Depth: 1}
	if err := SaveSessionDoc(childDir, childDoc, nil); err != nil {
		t.Fatalf("save child: %v", err)
	}

	_, err := ForkSessionTree(sourceDir, destDir)
	if err != nil {
		t.Fatalf("ForkSessionTree failed: %v", err)
	}

	// Remove source entirely
	if err := os.RemoveAll(sourceDir); err != nil {
		t.Fatalf("remove source: %v", err)
	}

	// Forked tree should still be navigable
	forkedRoot, err := LoadSessionDoc(destDir)
	if err != nil {
		t.Fatalf("load forked root after source removal: %v", err)
	}
	if forkedRoot.Identity.RootID != forkedRoot.Identity.ID {
		t.Fatalf("forked root RootID != ID after source removal")
	}

	forkedChild, err := LoadSessionDoc(ChildSessionDir(destDir, "c1"))
	if err != nil {
		t.Fatalf("load forked child after source removal: %v", err)
	}
	if forkedChild.Identity.RootID != forkedRoot.Identity.ID {
		t.Fatalf("forked child RootID doesn't match forked root ID")
	}
	if forkedChild.Identity.ParentID != forkedRoot.Identity.ID {
		t.Fatalf("forked child ParentID doesn't match forked root ID")
	}
}

func TestForkSessionTreeRemapsParentToolCallIDOnlyInIdentity(t *testing.T) {
	// ParentToolCallID should NOT be remapped — it identifies the tool call
	// within the parent session's message history, which is preserved as-is.
	sourceDir := filepath.Join(t.TempDir(), "source-session")
	destDir := filepath.Join(t.TempDir(), "dest-session")

	rootDoc := NewSessionDoc(SessionConfig{})
	rootDoc.Identity = SessionIdentity{ID: "root", RootID: "root", Depth: 0}
	if err := SaveSessionDoc(sourceDir, rootDoc, nil); err != nil {
		t.Fatalf("save root: %v", err)
	}

	childDir := ChildSessionDir(sourceDir, "c1")
	childDoc := NewSessionDoc(SessionConfig{})
	childDoc.Identity = SessionIdentity{
		ID:               "child",
		ParentID:         "root",
		RootID:           "root",
		ParentToolCallID: "original-tc-id",
		Depth:            1,
	}
	if err := SaveSessionDoc(childDir, childDoc, nil); err != nil {
		t.Fatalf("save child: %v", err)
	}

	_, err := ForkSessionTree(sourceDir, destDir)
	if err != nil {
		t.Fatalf("ForkSessionTree failed: %v", err)
	}

	forkedChild, err := LoadSessionDoc(ChildSessionDir(destDir, "c1"))
	if err != nil {
		t.Fatalf("load forked child: %v", err)
	}
	// ParentToolCallID should be preserved unchanged
	if forkedChild.Identity.ParentToolCallID != "original-tc-id" {
		t.Fatalf("ParentToolCallID was remapped: %q", forkedChild.Identity.ParentToolCallID)
	}
}
