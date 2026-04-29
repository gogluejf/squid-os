package tools

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"golang.org/x/sys/unix"
)

// Bash executes a shell command and returns stdout/stderr.
var Bash = Tool{
	Name:        "bash",
	Description: "Execute a shell command and return stdout/stderr. Use for git, find, grep, curl, and other CLI tools. Does not modify files. Timeout: 120 seconds.",
	Schema: map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"command": map[string]interface{}{
				"type":        "string",
				"description": "The shell command to execute",
			},
			"timeout": map[string]interface{}{
				"type":        "number",
				"description": "Timeout in milliseconds (default 120000)",
			},
		},
		"required": []string{"command"},
	},
	Execute: func(args map[string]interface{}) (string, error) {
		cmdStr, ok := args["command"].(string)
		if !ok || cmdStr == "" {
			return "", fmt.Errorf("command is required and must be a string")
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

		cmd := exec.CommandContext(ctx, "bash", "-c", cmdStr)

		// xdg-open blocks the terminal; run it detached
		if strings.Contains(cmdStr, "xdg-open") {
			cmd.SysProcAttr = &unix.SysProcAttr{Setpgid: true}
			err := cmd.Start()
			if err != nil {
				return "", fmt.Errorf("failed to run %s: %w", cmdStr, err)
			}
			return fmt.Sprintf("launched: %s", cmdStr), nil
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
			return result.String(), fmt.Errorf("command failed: %w", err)
		}

		return result.String(), nil
	},
}
