package environment

import (
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
)

// execCheck runs a command with one arg and returns nil on success.
func execCheck(cmd string, arg string) error {
	return exec.Command(cmd, arg).Run()
}

// runCommandSilent checks whether a CLI tool is installed.
func runCommandSilent(cmd string, arg string) bool {
	return execCheck(cmd, arg) == nil
}

// CollectOSInfo gathers OS-level context. currentDir is the working directory.
// Home is resolved internally via os.UserHomeDir().
func CollectOSInfo(currentDir string) OSInfo {
	home, _ := os.UserHomeDir()
	return OSInfo{
		OS:   runtime.GOOS,
		Arch: runtime.GOARCH,
		Home: home,
		CurrentDir: currentDir,
		GitInstalled:  runCommandSilent("git", "--version"),
		TreeInstalled: runCommandSilent("tree", "--version"),
	}
}

// GenerateTree returns a directory listing. Uses `tree` if available,
// falls back to `find` for a flat listing limited to maxDepth levels.
func GenerateTree(dir string, maxDepth int) string {
	if runCommandSilent("tree", "--version") {
		data, err := exec.Command("tree", "-a", "--gitignore", "-I", "node_modules|.git|.vscode|.idea|.cache|.next|.nuxt|.pytest_cache|.dart_tool|.gradle|.terraform|.parcel-cache|.eslintcache", "--dirsfirst", dir).Output()
		if err == nil {
			out := strings.TrimSpace(string(data))
			lines := strings.Split(out, "\n")
			if len(lines) > 200 {
				lines = lines[:200]
				lines = append(lines, "... (truncated)")
			}
			return strings.Join(lines, "\n")
		}
	}

	// Fallback: find with depth limit, prune common messy dirs
	find := exec.Command("find", dir, "-maxdepth", strconv.Itoa(maxDepth),
		"!", "-path", "*/node_modules/*", "!", "-path", "*/.git/*",
		"!", "-path", "*/.vscode/*", "!", "-path", "*/.idea/*",
		"!", "-path", "*/.cache/*", "!", "-path", "*/.next/*",
		"!", "-path", "*/.nuxt/*", "!", "-path", "*/.pytest_cache/*",
		"!", "-path", "*/.dart_tool/*", "!", "-path", "*/.gradle/*",
		"!", "-path", "*/.terraform/*", "!", "-path", "*/.parcel-cache/*",
		"!", "-path", "*/.eslintcache/*",
		"-print")
	data, err := find.Output()
	if err != nil {
		return "(directory listing unavailable)"
	}
	out := strings.TrimSpace(string(data))
	lines := strings.Split(out, "\n")
	if len(lines) > 200 {
		lines = lines[:200]
		lines = append(lines, "... (truncated)")
	}
	return strings.Join(lines, "\n")
}
