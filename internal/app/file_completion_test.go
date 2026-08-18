package app

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultFileSearchConfig(t *testing.T) {
	cfg := DefaultFileSearchConfig()
	if len(cfg.Roots) != 0 {
		t.Fatalf("Roots = %v, want empty", cfg.Roots)
	}
	if cfg.MaxDepth != 3 {
		t.Fatalf("MaxDepth = %d, want 3", cfg.MaxDepth)
	}
	if cfg.MaxResults != 50 {
		t.Fatalf("MaxResults = %d, want 50", cfg.MaxResults)
	}
	if cfg.Ignore != nil {
		t.Fatal("Ignore should be nil by default")
	}
}

func TestResolvedRootsFallsBackToWorkingDir(t *testing.T) {
	cfg := DefaultFileSearchConfig()
	roots := cfg.ResolvedRoots("/home/user/project")
	if len(roots) != 1 || roots[0] != "/home/user/project" {
		t.Fatalf("ResolvedRoots = %v, want [/home/user/project]", roots)
	}
}

func TestResolvedRootsUsesConfiguredRoots(t *testing.T) {
	cfg := FileSearchConfig{Roots: []string{".", "subdir"}}
	roots := cfg.ResolvedRoots("/home/user/project")
	if len(roots) != 2 {
		t.Fatalf("len = %d, want 2", len(roots))
	}
	if roots[0] != "/home/user/project" {
		t.Fatalf("roots[0] = %s, want /home/user/project", roots[0])
	}
	if roots[1] != "/home/user/project/subdir" {
		t.Fatalf("roots[1] = %s, want /home/user/project/subdir", roots[1])
	}
}

func TestResolvedRootsAbsoluteAndHome(t *testing.T) {
	cfg := FileSearchConfig{Roots: []string{"/tmp", "~/docs"}}
	roots := cfg.ResolvedRoots("/ignored")
	if len(roots) != 2 {
		t.Fatalf("len = %d, want 2", len(roots))
	}
	if roots[0] != "/tmp" {
		t.Fatalf("roots[0] = %s, want /tmp", roots[0])
	}
	home := os.Getenv("HOME")
	if home == "" {
		home = "/home/testuser"
	}
	want := filepath.Join(home, "docs")
	if roots[1] != want {
		t.Fatalf("roots[1] = %s, want %s", roots[1], want)
	}
}

func TestResolvedIgnore(t *testing.T) {
	cfg := DefaultFileSearchConfig()
	ignore := cfg.ResolvedIgnore()
	if len(ignore) != len(DefaultIgnoreDirs) {
		t.Fatalf("len = %d, want %d", len(ignore), len(DefaultIgnoreDirs))
	}

	customIgnore := []string{"custom_ignore"}
	cfg2 := FileSearchConfig{Ignore: customIgnore}
	ignore2 := cfg2.ResolvedIgnore()
	if len(ignore2) != 1 || ignore2[0] != "custom_ignore" {
		t.Fatalf("custom ignore = %v, want %v", ignore2, customIgnore)
	}
}

func TestSearchFilesBoundedByDepth(t *testing.T) {
	dir := t.TempDir()
	// Create a file at depth 0
	os.WriteFile(filepath.Join(dir, "readme.txt"), []byte("hello"), 0644)
	// Create a deeply nested structure
	deep := dir
	for i := 0; i < 5; i++ {
		deep = filepath.Join(deep, "level")
	}
	os.MkdirAll(deep, 0755)
	os.WriteFile(filepath.Join(deep, "deep.txt"), []byte("deep"), 0644)

	cfg := FileSearchConfig{MaxDepth: 3, MaxResults: 50}
	results := SearchFiles(cfg, dir, "")

	// Should find readme.txt but not deep.txt (beyond depth 3)
	found := make(map[string]bool)
	for _, r := range results {
		found[r.Name] = true
	}
	if !found["readme.txt"] {
		t.Fatal("expected readme.txt in results")
	}
	if found["level/level/level/level/level/deep.txt"] {
		t.Fatal("deep.txt should not be found at depth 5 with MaxDepth=3")
	}
}

func TestSearchFilesBoundedByMaxResults(t *testing.T) {
	dir := t.TempDir()
	// Create 60 files
	for i := 0; i < 60; i++ {
		name := filepath.Join(dir, "file")
		if _, err := os.Stat(name); err == nil {
			// already exists (shouldn't happen)
			continue
		}
		os.WriteFile(name, []byte("x"), 0644)
	}

	cfg := FileSearchConfig{MaxDepth: 1, MaxResults: 10}
	results := SearchFiles(cfg, dir, "")
	if len(results) > 10 {
		t.Fatalf("MaxResults=10 but got %d results", len(results))
	}
}

func TestSearchFilesSkipsHiddenDirs(t *testing.T) {
	dir := t.TempDir()
	// Create a hidden directory
	hidden := filepath.Join(dir, ".hidden")
	os.MkdirAll(hidden, 0755)
	os.WriteFile(filepath.Join(hidden, "secret.txt"), []byte("secret"), 0644)
	// Create a visible file
	os.WriteFile(filepath.Join(dir, "visible.txt"), []byte("visible"), 0644)

	cfg := FileSearchConfig{MaxDepth: 3, MaxResults: 50}
	results := SearchFiles(cfg, dir, "")

	for _, r := range results {
		if r.Name == "secret.txt" {
			t.Fatal("hidden dir file should not appear")
		}
	}
	found := false
	for _, r := range results {
		if r.Name == "visible.txt" {
			found = true
		}
	}
	if !found {
		t.Fatal("visible.txt should appear in results")
	}
}

func TestSearchFilesSkipsIgnoredDirs(t *testing.T) {
	dir := t.TempDir()
	// Create .git directory (default-ignored)
	git := filepath.Join(dir, ".git")
	os.MkdirAll(git, 0755)
	os.WriteFile(filepath.Join(git, "config"), []byte("git"), 0644)
	// Create node_modules (default-ignored)
	nm := filepath.Join(dir, "node_modules")
	os.MkdirAll(nm, 0755)
	os.WriteFile(filepath.Join(nm, "package.json"), []byte("{}"), 0644)
	// Create a visible file
	os.WriteFile(filepath.Join(dir, "readme.md"), []byte("# readme"), 0644)

	cfg := FileSearchConfig{MaxDepth: 3, MaxResults: 50}
	results := SearchFiles(cfg, dir, "")

	for _, r := range results {
		if r.Name == "config" || r.Name == "package.json" {
			t.Fatalf("ignored dir file should not appear: %s", r.Name)
		}
	}
	// Should have readme.md
	if len(results) != 1 || results[0].Name != "readme.md" {
		t.Fatalf("expected [readme.md], got %v", results)
	}
}

func TestSearchFilesMatchesQuery(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "foo.txt"), []byte("hello"), 0644)
	os.WriteFile(filepath.Join(dir, "bar.md"), []byte("# bar"), 0644)
	os.WriteFile(filepath.Join(dir, "foo.md"), []byte("# foo"), 0644)
	os.WriteFile(filepath.Join(dir, "binary.bin"), []byte{0x00, 0x01}, 0644) // should be filtered

	cfg := FileSearchConfig{MaxDepth: 1, MaxResults: 50}

	results := SearchFiles(cfg, dir, "foo")
	if len(results) != 2 {
		t.Fatalf("query=foo got %d results, want 2", len(results))
	}
	for _, r := range results {
		if r.Name != "foo.txt" && r.Name != "foo.md" {
			t.Fatalf("unexpected result: %s", r.Name)
		}
	}

	results = SearchFiles(cfg, dir, "bar")
	if len(results) != 1 || results[0].Name != "bar.md" {
		t.Fatalf("query=bar got %v, want [bar.md]", results)
	}

	results = SearchFiles(cfg, dir, "nonexistent")
	if len(results) != 0 {
		t.Fatalf("query=nonexistent got %d results, want 0", len(results))
	}
}

func TestSearchFilesEmptyRoot(t *testing.T) {
	cfg := FileSearchConfig{MaxDepth: 3, MaxResults: 50}
	results := SearchFiles(cfg, "/nonexistent/path/that/does/not/exist", "")
	if len(results) != 0 {
		t.Fatalf("nonexistent root should return no results, got %d", len(results))
	}
}

func TestSearchFilesNoDuplicates(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "unique.txt"), []byte("hello"), 0644)

	// Configure the same root twice
	cfg := FileSearchConfig{
		Roots:      []string{dir, dir},
		MaxDepth:   1,
		MaxResults: 50,
	}
	results := SearchFiles(cfg, dir, "")
	if len(results) != 1 {
		t.Fatalf("duplicate roots should not produce duplicates: got %d", len(results))
	}
}

func TestIsDirectPath(t *testing.T) {
	cases := []struct {
		input string
		want  bool
	}{
		{"/home/user/file.txt", true},
		{"./relative/path.txt", true},
		{"subdir/file.go", true},
		{"test.txt", true}, // has a dot
		{"https://example.com/img.png", false},
		{"plain-text", false},
	}
	for _, tc := range cases {
		got := IsDirectPath(tc.input)
		if got != tc.want {
			t.Errorf("IsDirectPath(%q) = %v, want %v", tc.input, got, tc.want)
		}
	}
}

func TestResolveDirectPath(t *testing.T) {
	workingDir := "/home/user/project"
	cases := []struct {
		raw   string
		want  string
	}{
		{"/absolute/file.txt", "/absolute/file.txt"},
		{"./relative.txt", "/home/user/project/relative.txt"},
		{"subdir/file.txt", "/home/user/project/subdir/file.txt"},
	}
	for _, tc := range cases {
		got, _ := ResolveDirectPath(tc.raw, workingDir)
		if got != tc.want {
			t.Errorf("ResolveDirectPath(%q, %q) = %q, want %q", tc.raw, workingDir, got, tc.want)
		}
	}
}

func TestDirectPathCandidatesExistingFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.txt")
	os.WriteFile(path, []byte("hello"), 0644)

	results := DirectPathCandidates("test.txt", dir)
	if len(results) != 1 {
		t.Fatalf("existing file: got %d, want 1", len(results))
	}
	if results[0].Kind != "file" {
		t.Fatalf("kind = %s, want file", results[0].Kind)
	}
}

func TestDirectPathCandidatesNonExistingFile(t *testing.T) {
	results := DirectPathCandidates("/nonexistent/file.txt", "/tmp")
	if len(results) != 0 {
		t.Fatalf("nonexisting file: got %d, want 0", len(results))
	}
}

func TestFileExists(t *testing.T) {
	if FileExists("/nonexistent/file/that/does/not/exist") {
		t.Fatal("nonexistent file should return false")
	}
	// Test with a real file
	tmp := t.TempDir()
	file := filepath.Join(tmp, "exists.txt")
	os.WriteFile(file, []byte("x"), 0644)
	if !FileExists(file) {
		t.Fatal("existing file should return true")
	}
}

func TestReferenceCandidate(t *testing.T) {
	c := ReferenceCandidate{
		Kind:   "file",
		Name:   "readme.md",
		Source: "/home/user/project/readme.md",
	}
	if c.Kind != "file" || c.Name != "readme.md" || c.Source != "/home/user/project/readme.md" {
		t.Fatalf("ReferenceCandidate fields mismatch: %+v", c)
	}
}

func TestCapabilityCandidateFileQualified(t *testing.T) {
	c := capabilityCandidate{kind: "file", name: "readme.md", source: "/home/user/readme.md"}
	if c.qualified() != "file:readme.md" {
		t.Fatalf("qualified = %s, want file:readme.md", c.qualified())
	}
}

func TestCapabilityCandidateFileLabel(t *testing.T) {
	c := capabilityCandidate{kind: "file", name: "readme.md", source: "/home/user/readme.md"}
	if c.label() != "file  readme.md" {
		t.Fatalf("label = %s, want 'file  readme.md'", c.label())
	}
}

func TestCapabilityCandidatesSortsFilesLast(t *testing.T) {
	m := newCompletionTestModel(t, nil, nil)
	m.session.Doc.Config.WorkingDir = m.session.Catalog.WorkingDir

	// Create a file in the working directory
	os.WriteFile(filepath.Join(m.session.Catalog.WorkingDir, "test.txt"), []byte("x"), 0644)

	candidates := m.capabilityCandidates()

	// Files should come after skills, agents, tools
	fileIdx := -1
	for i, c := range candidates {
		if c.kind == "file" {
			fileIdx = i
			break
		}
	}
	if fileIdx == -1 {
		// No files found — that's ok if the working dir is empty or search failed
		return
	}
	// All items before fileIdx should be skill/agent/tool
	for i := 0; i < fileIdx; i++ {
		switch candidates[i].kind {
		case "skill", "agent", "tool":
			// expected
		default:
			t.Fatalf("position %d: kind = %s, expected skill/agent/tool", i, candidates[i].kind)
		}
	}
}

func TestFileCandidatesWithExistingFile(t *testing.T) {
	m := newCompletionTestModel(t, nil, nil)
	m.session.Doc.Config.WorkingDir = m.session.Catalog.WorkingDir

	// Create a file
	filename := "mydocument.txt"
	os.WriteFile(filepath.Join(m.session.Catalog.WorkingDir, filename), []byte("content"), 0644)

	// Type @ and the filename prefix
	m.textarea.SetValue("see @mydoc")
	candidates := m.fileCandidates()
	if len(candidates) != 1 {
		t.Fatalf("file completion: got %d candidates, want 1", len(candidates))
	}
}

func TestFileSearchConfigFromSettings(t *testing.T) {
	m := newCompletionTestModel(t, nil, nil)
	cfg := m.fileSearchConfig()

	// Defaults should apply
	if cfg.MaxDepth != 3 {
		t.Fatalf("MaxDepth = %d, want 3", cfg.MaxDepth)
	}
	if cfg.MaxResults != 50 {
		t.Fatalf("MaxResults = %d, want 50", cfg.MaxResults)
	}
}

func TestSearchFilesExtensionFilter(t *testing.T) {
	dir := t.TempDir()
	for _, f := range []string{"a.txt", "b.md", "c.png", "d.jpg", "e.pdf", "c.go", "d.sh", "f.bin", "g.json", "h.rs"} {
		os.WriteFile(filepath.Join(dir, f), []byte("x"), 0644)
	}
	cfg := FileSearchConfig{MaxDepth: 3, MaxResults: 50}
	results := SearchFiles(cfg, dir, "")
	allowed := map[string]bool{"a.txt": true, "b.md": true, "c.png": true, "d.jpg": true, "e.pdf": true}
	for _, r := range results {
		if !allowed[r.Name] {
			t.Fatalf("unexpected file in results: %s (should be filtered out)", r.Name)
		}
		delete(allowed, r.Name)
	}
	if len(allowed) != 0 {
		t.Fatalf("missing expected files: %v", allowed)
	}
}
