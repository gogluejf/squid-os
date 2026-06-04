package git

import (
	"fmt"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"time"

	"squid-os/internal/style"

	"github.com/charmbracelet/lipgloss"
)

// shortstatRe matches git --shortstat output like " 3 files changed, 12 insertions(+), 5 deletions(-)"
var shortstatRe = regexp.MustCompile(`(\d+)\s+insertion|(\d+)\s+deletion`)

// gitCmd runs a git command in the given directory and returns stdout.
func gitCmd(dir string, args ...string) string {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// parseShortstat extracts insertions and deletions from git --shortstat output.
// Returns (insertions, deletions). Both 0 means no changes.
func parseShortstat(output string) (int, int) {
	matches := shortstatRe.FindAllStringSubmatch(output, -1)
	insertions := 0
	deletions := 0
	for _, m := range matches {
		if m[1] != "" {
			fmt.Sscanf(m[1], "%d", &insertions)
		}
		if m[2] != "" {
			fmt.Sscanf(m[2], "%d", &deletions)
		}
	}
	return insertions, deletions
}

// ShortStat returns a color-coded git status string for the given directory.
// Format for a git repo:
//
//	"~/path (git a1b2c3d +12 -5)"
//	- "git" word in orange (same as inline code)
//	- hash in orange (same as inline code)
//	- insertions in light green, deletions in light red
//	- if no uncommitted changes: "~/path (git a1b2c3d)"
//
// For non-git directories, returns the path unchanged.
// The output is pre-styled with lipgloss (ready to render in footer).
func ShortStat(dir string) string {
	if dir == "" || !HasGit(dir) {
		return dir
	}

	// Get short hash of last commit
	hash := gitCmd(dir, "rev-parse", "--short", "HEAD")
	if hash == "" {
		return dir // no commits yet
	}

	// Get unstaged changes
	unstaged := gitCmd(dir, "diff", "--shortstat")
	// Get staged changes
	staged := gitCmd(dir, "diff", "--cached", "--shortstat")

	insertions, deletions := 0, 0
	if unstaged != "" {
		i, d := parseShortstat(unstaged)
		insertions += i
		deletions += d
	}
	if staged != "" {
		i, d := parseShortstat(staged)
		insertions += i
		deletions += d
	}

	// Build styled output
	bg := lipgloss.Color(style.P.BgFooter)

	// "git" in primary color, hash in orange (matching inline code color)
	primaryStyle := lipgloss.NewStyle().
		Background(bg).
		Foreground(lipgloss.Color(style.P.TextPrimary))
	orangeStyle := lipgloss.NewStyle().
		Background(bg).
		Foreground(lipgloss.Color("209"))

	label := primaryStyle.Render("git ") + orangeStyle.Render(hash)

	if insertions > 0 || deletions > 0 {
		parts := []string{label}

		if insertions > 0 {
			// Light green for insertions
			insStyle := lipgloss.NewStyle().
				Background(bg).
				Foreground(lipgloss.Color("114"))
			parts = append(parts, insStyle.Render(fmt.Sprintf(" +%d", insertions)))
		}

		if deletions > 0 {
			// Light red for deletions
			delStyle := lipgloss.NewStyle().
				Background(bg).
				Foreground(lipgloss.Color("203"))
			parts = append(parts, delStyle.Render(fmt.Sprintf(" -%d", deletions)))
		}

		return dir + " (" + strings.Join(parts, "") + ")"
	}

	return dir + " (" + label + ")"
}

// --- Cached version for hot-path (footer) use ---

const shortStatTTL = 2 * time.Second

var (
	shortStatCacheMu sync.Mutex
	shortStatCache   = map[string]struct {
		value time.Time
		text  string
	}{}
)

// CachedShortStat returns the git shortstat string for the given directory,
// cached for shortStatTTL to avoid spawning git processes on every render tick.
func CachedShortStat(dir string) string {
	now := time.Now()

	shortStatCacheMu.Lock()
	entry, ok := shortStatCache[dir]
	if ok && now.Sub(entry.value) < shortStatTTL {
		cached := entry.text
		shortStatCacheMu.Unlock()
		return cached
	}
	shortStatCacheMu.Unlock()

	result := ShortStat(dir)

	shortStatCacheMu.Lock()
	shortStatCache[dir] = struct {
		value time.Time
		text  string
	}{value: now, text: result}
	shortStatCacheMu.Unlock()

	return result
}
