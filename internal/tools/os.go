package tools

import (
	"fmt"
	"os/exec"
	"runtime"
	"strings"

	"squid-os/internal/style"
)

// Open opens a file, URL, or directory with the system default application.
var Open = Tool{
	Name:         "open",
	Description:  "Open a file, URL, or directory with the system default application (xdg-open on Linux, open on macOS, start on Windows). Use for launching browsers, editors, or previewing files.",
	DisplayParams: []string{"path"},
	Style:        style.ToolStyle(),
	Schema: []byte(`{
	"type": "object",
	"properties": {
		"path": {
			"type": "string",
			"description": "The file path, URL, or directory to open"
		}
	},
	"required": ["path"]
}`),
	Execute: func(args map[string]interface{}, rt RuntimeContext) ToolResult {
		target, ok := args["path"].(string)
		if !ok || target == "" {
			return ToolResult{Status: ResultStatusError, Error: "path is required and must be a string"}
		}

		if !strings.HasPrefix(target, "http://") && !strings.HasPrefix(target, "https://") {
			target = ResolvePath(target, rt.Config.WorkingDir)
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
			return ToolResult{Status: ResultStatusError, Error: fmt.Sprintf("open is not supported on %s", runtime.GOOS)}
		}

		err := cmd.Start()
		if err != nil {
			return ToolResult{Status: ResultStatusError, Error: fmt.Sprintf("failed to open %s: %v", target, err)}
		}

		return ToolResult{Status: ResultStatusSuccess, Result: fmt.Sprintf("opened: %s", target)}
	},
}
