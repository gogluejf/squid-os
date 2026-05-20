package environment

import (
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

// CollectOSInfo gathers OS-level context.
func CollectOSInfo(homeDir, currentDir string) OSInfo {
	return OSInfo{
		OS:            runtime.GOOS,
		Arch:          runtime.GOARCH,
		Home:          homeDir,
		CurrentDir:    currentDir,
		GitInstalled:  runCommandSilent("git", "--version"),
		TreeInstalled: runCommandSilent("tree", "--version"),
	}
}

// GenerateTree returns a directory listing. Uses `tree` if available,
// falls back to `find` for a flat listing limited to maxDepth levels.
func GenerateTree(dir string, maxDepth int) string {
	if runCommandSilent("tree", "--version") {
		data, err := exec.Command("tree", "-a", "--gitignore", "-I", "node_modules", "--dirsfirst", dir).Output()
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

	// Fallback: find with depth limit, prune node_modules and .git
	find := exec.Command("find", dir, "-maxdepth", strconv.Itoa(maxDepth),
		"!", "-path", "*/node_modules/*", "!", "-path", "*/.git/*",
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
