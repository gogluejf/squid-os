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

// CollectOSInfo gathers OS-level context. workingDir is the working directory.
// Home is resolved internally via os.UserHomeDir().
func CollectOSInfo(workingDir string) OSInfo {
	home, _ := os.UserHomeDir()
	return OSInfo{
		OS:            runtime.GOOS,
		Arch:          runtime.GOARCH,
		Home:          home,
		WorkingDir:    workingDir,
		GitInstalled:  runCommandSilent("git", "--version"),
		TreeInstalled: runCommandSilent("tree", "--version"),
	}
}

// GenerateTree returns a directory listing. Uses `tree` if available,
// falls back to `find` for a flat listing limited to maxDepth levels.
func GenerateTree(dir string, maxDepth int) string {
	if runCommandSilent("tree", "--version") {
		cmd := exec.Command("tree", "-d", "-a", "--gitignore", "-I", "data|node_modules|.git|.vscode|.idea|.cache|.next|.nuxt|.pytest_cache|.dart_tool|.gradle|.terraform|.parcel-cache|.eslintcache", "--dirsfirst", dir)
		data, err := cmd.CombinedOutput()
		// tree may exit non-zero (e.g., permission errors on some dirs) but still produce output
		if err == nil || len(data) > 0 {
			out := strings.TrimSpace(string(data))
			if out != "" {
				return out
			}
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
	return strings.TrimSpace(string(data))
}
