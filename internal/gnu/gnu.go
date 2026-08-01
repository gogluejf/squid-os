package gnu

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strings"
)

type Platform struct{ OS, Description, PackageManager, Shell, Home, User, WorkingDir string }
type Suggestion struct {
	Command     string `json:"command"`
	InstallHint string `json:"install_hint"`
}

func DetectPlatform(workingDir string) Platform {
	platform := Platform{OS: runtime.GOOS, Shell: "sh", Home: os.Getenv("HOME"), WorkingDir: workingDir}
	platform.User = os.Getenv("USER")
	if shell := os.Getenv("SHELL"); shell != "" {
		platform.Shell = shell
	}
	switch runtime.GOOS {
	case "darwin":
		platform.Description, platform.PackageManager = "macOS with BSD userland", "brew install"
	case "linux":
		platform.Description, platform.PackageManager = linuxDescription(), linuxPackageManager()
	default:
		platform.Description, platform.PackageManager = runtime.GOOS, "unknown"
	}
	return platform
}

func BuildPrompt(request string, platform Platform) string {
	return fmt.Sprintf(`You generate shell commands for the user's current environment.
Return exactly one JSON object and no markdown or commentary:
{"command":"...","install_hint":"..."}

Rules:
- Output only the JSON object. The first byte must be { and the last byte must be }.
- command must be the actual shell command, not a format label like json, shell, bash, or command.
- command is one executable shell command suitable for %s.
- Prefer portable built-in tools and quote paths safely.
- Never use sudo unless the user explicitly requests it.
- Never invent files, devices, package names, or command flags.
- Avoid destructive operations when a read-only command can satisfy the request.
- If the command is destructive, preserve the user's exact scope and do not broaden it.
- install_hint is empty unless a non-standard dependency is required.
- If required, install_hint is the exact package command using %s.
- On macOS, use BSD command syntax unless the command explicitly installs GNU tools.

Environment:
OS: %s
Shell: %s
Home: %s
User: %s
Working directory: %s

Request:
%s`, platform.Shell, platform.PackageManager, platform.Description, platform.Shell, platform.Home, platform.User, platform.WorkingDir, request)
}

func ParseSuggestion(text string) (Suggestion, error) {
	text = strings.TrimSpace(text)
	if suggestion, ok := parseFirstJSONObject(text); ok {
		return suggestion, nil
	}
	var suggestion Suggestion
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		line = strings.Trim(line, "`")
		line = strings.TrimSpace(line)
		if line == "" || isFormatLabel(line) || strings.HasPrefix(line, "{") || strings.HasPrefix(line, "}") {
			continue
		}
		if strings.HasPrefix(strings.ToUpper(line), "INSTALL:") {
			suggestion.InstallHint = strings.TrimSpace(line[len("INSTALL:"):])
			continue
		}
		if suggestion.Command == "" {
			suggestion.Command = line
		}
	}
	if suggestion.Command == "" {
		return Suggestion{}, fmt.Errorf("model returned no shell command")
	}
	if isFormatLabel(suggestion.Command) {
		return Suggestion{}, fmt.Errorf("model returned a format label instead of a shell command: %s", suggestion.Command)
	}
	return suggestion, nil
}

func parseFirstJSONObject(text string) (Suggestion, bool) {
	for start := strings.Index(text, "{"); start >= 0 && start < len(text); {
		depth := 0
		inString := false
		escaped := false
		for index := start; index < len(text); index++ {
			ch := text[index]
			if inString {
				if escaped {
					escaped = false
					continue
				}
				if ch == '\\' {
					escaped = true
					continue
				}
				if ch == '"' {
					inString = false
				}
				continue
			}
			switch ch {
			case '"':
				inString = true
			case '{':
				depth++
			case '}':
				depth--
				if depth == 0 {
					var suggestion Suggestion
					if err := json.Unmarshal([]byte(text[start:index+1]), &suggestion); err == nil && suggestion.Command != "" && !isFormatLabel(suggestion.Command) {
						return suggestion, true
					}
					break
				}
			}
		}
		next := strings.Index(text[start+1:], "{")
		if next < 0 {
			break
		}
		start += next + 1
	}
	return Suggestion{}, false
}

func isFormatLabel(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "json", "bash", "sh", "shell", "command", "text", "markdown":
		return true
	default:
		return false
	}
}

func Confirm(reader io.Reader, writer io.Writer) (bool, error) {
	return ConfirmWithPrompt(reader, writer, "Execute? [y/N] ")
}

func ConfirmWithPrompt(reader io.Reader, writer io.Writer, prompt string) (bool, error) {
	fmt.Fprint(writer, prompt)
	line, err := bufio.NewReader(reader).ReadString('\n')
	if err != nil && err != io.EOF {
		return false, err
	}
	switch strings.ToLower(strings.TrimSpace(line)) {
	case "y", "yes":
		return true, nil
	default:
		return false, nil
	}
}

func Execute(command, workingDir string, stdin io.Reader, stdout, stderr io.Writer) error {
	if runtime.GOOS == "windows" {
		return fmt.Errorf("gnu command is not supported on native Windows")
	}
	shell := "/bin/sh"
	if configured := os.Getenv("SHELL"); configured != "" {
		shell = configured
	}
	cmd := exec.Command(shell, "-c", command)
	cmd.Dir, cmd.Stdin, cmd.Stdout, cmd.Stderr = workingDir, stdin, stdout, stderr
	return cmd.Run()
}

func linuxDescription() string {
	data, err := os.ReadFile("/etc/os-release")
	if err != nil {
		return "Linux"
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "PRETTY_NAME=") {
			value := strings.Trim(strings.TrimPrefix(line, "PRETTY_NAME="), "\"")
			if isWSL() {
				return value + " under WSL2"
			}
			return value
		}
	}
	return "Linux"
}
func linuxPackageManager() string {
	for _, item := range []struct{ name, command string }{{"apt-get", "apt install"}, {"dnf", "dnf install"}, {"pacman", "pacman -S"}, {"zypper", "zypper install"}} {
		if _, err := exec.LookPath(item.name); err == nil {
			return item.command
		}
	}
	return "your system package manager"
}
func isWSL() bool {
	data, _ := os.ReadFile("/proc/sys/kernel/osrelease")
	return strings.Contains(strings.ToLower(string(data)), "microsoft")
}
