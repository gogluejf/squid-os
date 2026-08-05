package tools

import (
	"fmt"
	"os"
	"path/filepath"

	"squid-os/internal/environment"
	"squid-os/internal/style"
	"squid-os/internal/util"
)

// projectDir is set by the app at startup via SetProjectDir.
var projectDir string

// SetProjectDir sets the project directory used for project metadata.
func SetProjectDir(dir string) {
	projectDir = dir
}

// ResolvePath resolves a path against the provided session working directory.
func ResolvePath(p, workingDir string) string {
	p = util.ExpandHome(p)
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
	Execute: func(args map[string]interface{}, _ RuntimeContext) ToolResult {
		path, ok := args["path"].(string)
		if !ok || path == "" {
			return ToolResult{Status: ResultStatusError, Error: "path is required and must be a string"}
		}
		path = util.ExpandHome(path)
		if !filepath.IsAbs(path) {
			return ToolResult{Status: ResultStatusError, Error: "path must be absolute"}
		}
		stat, err := os.Stat(path)
		if err != nil {
			return ToolResult{Status: ResultStatusError, Error: fmt.Sprintf("invalid working directory: %s: %v", path, err)}
		}
		if !stat.IsDir() {
			return ToolResult{Status: ResultStatusError, Error: fmt.Sprintf("working directory is not a directory: %s", path)}
		}
		info := environment.LoadProjectInfo(path, projectDir)
		result := environment.FormatProjectInfo(info)
		workspaceRoot := filepath.Join(path, ".squid-os")
		result += fmt.Sprintf("- workspace-memory: %s\n", filepath.Join(workspaceRoot, "memory"))
		result += fmt.Sprintf("- workspace-skills: %s\n", filepath.Join(workspaceRoot, "skills"))
		result += fmt.Sprintf("- workspace-agents: %s\n", filepath.Join(workspaceRoot, "agents"))
		return ToolResult{Status: ResultStatusSuccess, Result: result}
	},
}
