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
// The optional showLabel flag controls whether the "git: " label is included.
func ShortStat(dir string, showLabels ...bool) string {
	if dir == "" || !HasGit(dir) {
		return dir
	}
	showLabel := true
	if len(showLabels) > 0 {
		showLabel = showLabels[0]
	}

	hash := gitCmd(dir, "rev-parse", "--short", "HEAD")
	if hash == "" {
		return dir
	}

	unstaged := gitCmd(dir, "diff", "--shortstat")
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

	bg := lipgloss.Color(style.P.BgFooter)
	dimStyle := lipgloss.NewStyle().
		Background(bg).
		Foreground(lipgloss.Color(style.P.TextDim))
	orangeStyle := lipgloss.NewStyle().
		Background(bg).
		Foreground(lipgloss.Color("209"))

	prefix := "[git: "
	if !showLabel {
		prefix = "["
	}
	label := dimStyle.Render(prefix) + orangeStyle.Render(hash)

	if insertions > 0 || deletions > 0 {
		parts := []string{label}

		if insertions > 0 {
			insStyle := lipgloss.NewStyle().
				Background(bg).
				Foreground(lipgloss.Color("114"))
			parts = append(parts, insStyle.Render(fmt.Sprintf(" +%d", insertions)))
		}

		if deletions > 0 {
			delStyle := lipgloss.NewStyle().
				Background(bg).
				Foreground(lipgloss.Color("203"))
			parts = append(parts, delStyle.Render(fmt.Sprintf(" -%d", deletions)))
		}

		return dir + " " + strings.Join(parts, "") + dimStyle.Render("]")
	}

	return dir + " " + label + dimStyle.Render("]")
}

// --- Cached version for hot-path (footer) use ---

const shortStatTTL = 2 * time.Second

type shortStatCacheKey struct {
	dir       string
	showLabel bool
}

var (
	shortStatCacheMu sync.Mutex
	shortStatCache   = map[shortStatCacheKey]struct {
		value time.Time
		text  string
	}{}
)

// CachedShortStat returns the git shortstat string for the given directory,
// cached for shortStatTTL to avoid spawning git processes on every render tick.
func CachedShortStat(dir string, showLabels ...bool) string {
	showLabel := true
	if len(showLabels) > 0 {
		showLabel = showLabels[0]
	}
	key := shortStatCacheKey{dir: dir, showLabel: showLabel}
	now := time.Now()

	shortStatCacheMu.Lock()
	entry, ok := shortStatCache[key]
	if ok && now.Sub(entry.value) < shortStatTTL {
		cached := entry.text
		shortStatCacheMu.Unlock()
		return cached
	}
	shortStatCacheMu.Unlock()

	result := ShortStat(dir, showLabel)

	shortStatCacheMu.Lock()
	shortStatCache[key] = struct {
		value time.Time
		text  string
	}{value: now, text: result}
	shortStatCacheMu.Unlock()

	return result
}
