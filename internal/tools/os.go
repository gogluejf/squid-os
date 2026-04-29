package tools

import (
	"fmt"
	"os/exec"
	"runtime"
)

// Open opens a file, URL, or directory with the system default application.
var Open = Tool{
	Name:        "open",
	Description: "Open a file, URL, or directory with the system default application (xdg-open on Linux, open on macOS, start on Windows). Use for launching browsers, editors, or previewing files.",
	Schema: map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"path_or_url": map[string]interface{}{
				"type":        "string",
				"description": "The file path, URL, or directory to open",
			},
		},
		"required": []string{"path_or_url"},
	},
	Execute: func(args map[string]interface{}) (string, error) {
		target, ok := args["path_or_url"].(string)
		if !ok || target == "" {
			return "", fmt.Errorf("path_or_url is required and must be a string")
		}

		var cmd *exec.Cmd
		switch runtime.GOOS {
		case "linux":
			cmd = exec.Command("xdg-open", target)
		case "darwin":
			cmd = exec.Command("open", target)
		case "windows":
			cmd = exec.Command("cmd", "/c", "start", "", target)
		default:
			return "", fmt.Errorf("open is not supported on %s", runtime.GOOS)
		}

		err := cmd.Start()
		if err != nil {
			return "", fmt.Errorf("failed to open %s: %w", target, err)
		}

		return fmt.Sprintf("opened: %s", target), nil
	},
}
