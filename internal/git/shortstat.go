package git

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
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
	return strings.TrimSpace(string(gitCmdBytes(dir, args...)))
}

// gitCmdBytes runs a git command and preserves raw output, including NULs.
func gitCmdBytes(dir string, args ...string) []byte {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return nil
	}
	return out
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

const maxUntrackedTextBytes = 8 << 20

// untrackedStats returns added text lines and the number of untracked files.
// Porcelain -z keeps filenames unquoted and NUL-delimited. Binary, oversized,
// unreadable, and empty files still contribute to the file count but not lines.
func untrackedStats(dir string) (lines, files int) {
	output := gitCmdBytes(dir, "status", "--porcelain=v1", "-z", "--untracked-files=all")
	for _, entry := range bytes.Split(output, []byte{0}) {
		if len(entry) < 4 || entry[0] != '?' || entry[1] != '?' || entry[2] != ' ' {
			continue
		}
		files++
		path := filepath.Join(dir, string(entry[3:]))
		info, err := os.Lstat(path)
		if err != nil || !info.Mode().IsRegular() || info.Size() == 0 || info.Size() > maxUntrackedTextBytes {
			continue
		}
		file, err := os.Open(path)
		if err != nil {
			continue
		}
		data, readErr := io.ReadAll(io.LimitReader(file, maxUntrackedTextBytes+1))
		closeErr := file.Close()
		if readErr != nil || closeErr != nil || len(data) > maxUntrackedTextBytes || bytes.IndexByte(data, 0) >= 0 {
			continue
		}
		lines += bytes.Count(data, []byte{'\n'})
		if data[len(data)-1] != '\n' {
			lines++
		}
	}
	return lines, files
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
	untrackedLines, untrackedFiles := untrackedStats(dir)
	insertions += untrackedLines

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

	if insertions > 0 || deletions > 0 || untrackedFiles > 0 {
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

		if untrackedFiles > 0 {
			parts = append(parts, dimStyle.Render(fmt.Sprintf(" ?%d", untrackedFiles)))
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
