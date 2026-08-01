package tools

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"squid-os/internal/config"
	"squid-os/internal/environment"
	"squid-os/internal/style"
)

// projectDir is set by the app at startup via SetProjectDir.
var projectDir string

// SetProjectDir sets the project directory used for project metadata.
func SetProjectDir(dir string) {
	projectDir = dir
}

// ResolvePath resolves a path against the provided session working directory.
func ResolvePath(p, workingDir string) string {
	if strings.HasPrefix(p, "~") {
		home, _ := os.UserHomeDir()
		p = strings.Replace(p, "~", home, 1)
	}
	if filepath.IsAbs(p) {
		return p
	}
	if workingDir != "" {
		return filepath.Join(workingDir, p)
	}
	return p
}

// SetWorkingDirTool is the tool that sets the working directory and returns project info.
var SetWorkingDirTool = Tool{
	Name:         "set_working_dir",
	Description:  "Set the working directory. Tool calls will use this as the base for relative paths. Use when user requests or to switch context to another project.",
	DisplayParam: "path",
	Style:        style.ToolStyle(),
	Schema: []byte(`{
	"type": "object",
	"properties": {
		"path": {
			"type": "string",
			"description": "Absolute path to set as the working directory"
		}
	},
	"required": ["path"]
}`),
	Execute: func(args map[string]interface{}, _ config.SessionConfig) ToolResult {
		path, ok := args["path"].(string)
		if !ok || path == "" {
			return ToolResult{Status: ResultStatusError, Error: "path is required and must be a string"}
		}
		if _, err := os.Stat(path); os.IsNotExist(err) {
			return ToolResult{Status: ResultStatusError, Error: fmt.Sprintf("path does not exist: %s", path)}
		}
		info := environment.LoadProjectInfo(path, projectDir)
		return ToolResult{Status: ResultStatusSuccess, Result: environment.FormatProjectInfo(info)}
	},
}
