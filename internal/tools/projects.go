package tools

import (
	"fmt"
	"os"
	"path/filepath"

	"squid-os/internal/environment"
	"squid-os/internal/style"
)

// SetWorkingDirCallback is called by the tool to notify the app of a working dir change.
var SetWorkingDirCallback func(path string)

// projectDir is set by the app at startup via SetProjectDir.
var projectDir string

// workingDir is the working directory for all file tools.
var workingDir string

// SetProjectDir sets the project directory for the list_projects tool.
func SetProjectDir(dir string) {
	projectDir = dir
}

// SetWorkingDir sets the working directory used by file tools and bash.
func SetWorkingDir(dir string) {
	workingDir = dir
}

// resolvePath resolves a relative path against the working directory.
// If the path is already absolute, returns it unchanged.
func resolvePath(p string) string {
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
	Execute: func(args map[string]interface{}) ToolResult {
		path, ok := args["path"].(string)
		if !ok || path == "" {
			return ToolResult{Status: ResultStatusError, Error: "path is required and must be a string"}
		}
		if _, err := os.Stat(path); os.IsNotExist(err) {
			return ToolResult{Status: ResultStatusError, Error: fmt.Sprintf("path does not exist: %s", path)}
		}
		if SetWorkingDirCallback != nil {
			SetWorkingDirCallback(path)
		}
		info := environment.LoadProjectInfo(path, projectDir)
		return ToolResult{Status: ResultStatusSuccess, Result: environment.FormatProjectInfo(info)}
	},
}


