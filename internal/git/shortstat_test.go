package git

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestUntrackedStatsCountsTextFilesAndAllFileKinds(t *testing.T) {
	dir := newTestRepo(t)
	writeTestFile(t, dir, "notes.md", []byte("one\ntwo\nthree"))
	writeTestFile(t, dir, "empty.txt", nil)
	writeTestFile(t, dir, "binary.dat", []byte{'a', 0, 'b'})
	writeTestFile(t, dir, "odd\nname.txt", []byte("line\n"))

	lines, files := untrackedStats(dir)
	if lines != 4 || files != 4 {
		t.Fatalf("untrackedStats = (%d lines, %d files), want (4, 4)", lines, files)
	}
}

func TestUntrackedStatsExcludesIgnoredAndStagedFiles(t *testing.T) {
	dir := newTestRepo(t)
	writeTestFile(t, dir, ".gitignore", []byte("ignored.txt\n"))
	runGit(t, dir, "add", ".gitignore")
	runGit(t, dir, "commit", "-m", "add ignore")

	writeTestFile(t, dir, "ignored.txt", []byte("ignored\n"))
	writeTestFile(t, dir, "staged.txt", []byte("staged\n"))
	runGit(t, dir, "add", "staged.txt")
	writeTestFile(t, dir, "new.txt", []byte("one\ntwo\n"))

	lines, files := untrackedStats(dir)
	if lines != 2 || files != 1 {
		t.Fatalf("untrackedStats = (%d lines, %d files), want (2, 1)", lines, files)
	}
}

func TestShortStatIncludesUntrackedLinesAndCount(t *testing.T) {
	dir := newTestRepo(t)
	writeTestFile(t, dir, "new.txt", []byte("one\ntwo\nthree\n"))
	writeTestFile(t, dir, "empty.txt", nil)

	got := ShortStat(dir)
	if !strings.Contains(got, "+3") || !strings.Contains(got, "?2") {
		t.Fatalf("ShortStat missing untracked summary: %q", got)
	}
}

func newTestRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	runGit(t, dir, "init", "-q")
	runGit(t, dir, "config", "user.email", "test@example.com")
	runGit(t, dir, "config", "user.name", "Test User")
	writeTestFile(t, dir, "tracked.txt", []byte("base\n"))
	runGit(t, dir, "add", "tracked.txt")
	runGit(t, dir, "commit", "-q", "-m", "initial")
	return dir
}

func writeTestFile(t *testing.T, dir, name string, data []byte) {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatal(err)
	}
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, output)
	}
}
