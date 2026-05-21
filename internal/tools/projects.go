package tools

import (
	"fmt"
	"os"
	"path/filepath"

	"squid-os/internal/environment"
	"squid-os/internal/style"
)

// SetCurrentDirCallback is called by the tool to notify the app of a current dir change.
var SetCurrentDirCallback func(path string)

// projectDir is set by the app at startup via SetProjectDir.
var projectDir string

// currentDir is the current  directory for all file tools.
var currentDir string

// SetProjectDir sets the project directory for the list_projects tool.
func SetProjectDir(dir string) {
	projectDir = dir
}

// SetCurrentCurrentDir sets the current directory used by file tools and bash.
func SetCurrentCurrentDir(dir string) {
	currentDir = dir
}

// resolvePath resolves a relative path against the current directory.
// If the path is already absolute, returns it unchanged.
func resolvePath(p string) string {
	if filepath.IsAbs(p) {
		return p
	}
	if currentDir != "" {
		return filepath.Join(currentDir, p)
	}
	return p
}

// ListProjects returns all git-initialized projects under the configured project directory.
var ListProjects = Tool{
	Name:        "list_projects",
	Description: "List all projects (git repos) under the configured project directory.",
	Style:       style.ToolStyle(),
	Schema: []byte(`{
	"type": "object",
	"properties": {},
	"required": []
}`),
	Execute: func(args map[string]interface{}) ToolResult {
		entries := environment.FindProjects(projectDir)
		if len(entries) == 0 {
			return ToolResult{Status: ResultStatusSuccess, Result: "No projects found"}
		}
		var lines []string
		for _, e := range entries {
			lines = append(lines, fmt.Sprintf("  - %s (%s)", e.Name, e.Path))
		}
		return ToolResult{Status: ResultStatusSuccess, Result: "Projects:\n" + joinStrs(lines)}
	},
}

// SetCurrentDir sets the current current directory and returns project info.
var SetCurrentDir = Tool{
	Name:         "set_current_dir",
	Description:  "Set the current directory. Make tools calls relative to that dir possible. Use when user request or just switch context to other project",
	DisplayParam: "path",
	Style:        style.ToolStyle(),
	Schema: []byte(`{
	"type": "object",
	"properties": {
		"path": {
			"type": "string",
			"description": "Absolute path to set as the directory"
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
		if SetCurrentDirCallback != nil {
			SetCurrentDirCallback(path)
		}
		info := environment.LoadProjectInfo(path, projectDir)
		return ToolResult{Status: ResultStatusSuccess, Result: environment.FormatProjectInfo(info)}
	},
}

// joinStrs joins strings with newlines.
func joinStrs(ss []string) string {
	result := ""
	for i, s := range ss {
		if i > 0 {
			result += "\n"
		}
		result += s
	}
	return result
}
