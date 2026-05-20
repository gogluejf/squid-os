package tools

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"squid-os/internal/style"
)

// Bash executes a shell command and returns stdout/stderr.
var Bash = Tool{
	Name:         "bash",
	Description:  "Execute a shell command and return stdout/stderr. Use for git, find, grep, curl, and other CLI tools. Does not modify files. Timeout: 120 seconds.",
	DisplayParam: "command",
	Style:        style.ToolStyle(),
	Schema: []byte(`{
	"type": "object",
	"properties": {
		"command": {
			"type": "string",
			"description": "The shell command to execute"
		},
		"timeout": {
			"type": "number",
			"description": "Timeout in milliseconds (default 120000)"
		}
	},
	"required": ["command"]
}`),
	Execute: func(args map[string]interface{}) ToolResult {
		cmdStr, ok := args["command"].(string)
		if !ok || cmdStr == "" {
			return ToolResult{Status: ResultStatusError, Error: "command is required and must be a string"}
		}

		timeoutMs := 120000
		if t, ok := args["timeout"]; ok {
			switch v := t.(type) {
			case float64:
				timeoutMs = int(v)
			case int:
				timeoutMs = v
			}
		}

		ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timeoutMs)*time.Millisecond)
		defer cancel()

		// cd to working dir if set
		runDir := ""
		if workingDir != "" {
			runDir = workingDir
		}
		cmd := exec.CommandContext(ctx, "bash", "-c", cmdStr)
		if runDir != "" {
			cmd.Dir = runDir
		}

		// xdg-open blocks the terminal; run it detached via nohup
		if strings.Contains(cmdStr, "xdg-open") {
			cmd = exec.Command("nohup", "bash", "-c", cmdStr)
			cmd.Stdout = nil
			cmd.Stderr = nil
			err := cmd.Start()
			if err != nil {
				return ToolResult{Status: ResultStatusError, Error: fmt.Sprintf("failed to run %s: %w", cmdStr, err)}
			}
			return ToolResult{Status: ResultStatusSuccess, Result: fmt.Sprintf("launched: %s", cmdStr)}
		}

		var stdout, stderr bytes.Buffer
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr

		err := cmd.Run()
		var result strings.Builder
		if stdout.Len() > 0 {
			result.WriteString(stdout.String())
		}
		if stderr.Len() > 0 {
			if result.Len() > 0 {
				result.WriteString("\n")
			}
			result.WriteString("stderr: ")
			result.WriteString(stderr.String())
		}
		if err != nil {
			if result.Len() > 0 {
				result.WriteString("\n")
			}
			result.WriteString(fmt.Sprintf("exit code: %v", err))
			return ToolResult{Status: ResultStatusError, Error: result.String()}
		}

		return ToolResult{Status: ResultStatusSuccess, Result: result.String()}
	},
}
