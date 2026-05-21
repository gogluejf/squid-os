package tools

import (
	"fmt"
	"os"

	"squid-os/internal/style"
)

// ReadFile reads a file and returns its contents.
var ReadFile = Tool{
	Name:        "read_file",
	Description: "Read the contents of a file at the given path (relative to current directory or absolute). Returns the raw text content. Use for reading code, configs, documents, and any text-based files.",
	DisplayParam: "path",
	Style:       style.ToolStyle(),
	Schema: []byte(`{
	"type": "object",
	"properties": {
		"path": {
			"type": "string",
			"description": "Path to the file to read (relative or absolute)"
		}
	},
	"required": ["path"]
}`),
	Execute: func(args map[string]interface{}) ToolResult {
		path, ok := args["path"].(string)
		if !ok || path == "" {
			return ToolResult{Status: ResultStatusError, Error: "path is required and must be a string"}
		}
		path = resolvePath(path)
		data, err := os.ReadFile(path)
		if err != nil {
			return ToolResult{Status: ResultStatusError, Error: fmt.Sprintf("failed to read file %s: %w", path, err)}
		}
		return ToolResult{Status: ResultStatusSuccess, Result: string(data)}
	},
}

// WriteFile creates a new file or overwrites an existing one with the given content.
var WriteFile = Tool{
	Name:        "write_file",
	Description: "Create a new file or completely overwrite an existing file with the given content. Use for new files or full rewrites only. Path can be relative to current directory or absolute.",
	DisplayParam: "path",
	Style:       style.ToolStyle(),
	Schema: []byte(`{
	"type": "object",
	"properties": {
		"path": {
			"type": "string",
			"description": "Path to the file to write (relative or absolute)"
		},
		"content": {
			"type": "string",
			"description": "The content to write to the file"
		}
	},
	"required": ["path", "content"]
}`),
	Execute: func(args map[string]interface{}) ToolResult {
		path, ok := args["path"].(string)
		if !ok || path == "" {
			return ToolResult{Status: ResultStatusError, Error: "path is required and must be a string"}
		}
		path = resolvePath(path)
		content, ok := args["content"].(string)
		if !ok {
			return ToolResult{Status: ResultStatusError, Error: "content is required and must be a string"}
		}
		err := os.WriteFile(path, []byte(content), 0644)
		if err != nil {
			return ToolResult{Status: ResultStatusError, Error: fmt.Sprintf("failed to write file %s: %w", path, err)}
		}
		return ToolResult{Status: ResultStatusSuccess, Result: fmt.Sprintf("file written: %s (%d bytes)", path, len(content))}
	},
}

// EditFile performs a precise string replacement in an existing file.
var EditFile = Tool{
	Name:        "edit_file",
	Description: "Perform a precise string replacement in an existing file. old_string must match exactly. replace_all replaces every occurrence. Prefer over write_file for modifications. Path can be relative to current directory or absolute.",
	DisplayParam: "path",
	Style:       style.ToolStyle(),
	Schema: []byte(`{
	"type": "object",
	"properties": {
		"path": {
			"type": "string",
			"description": "Path to the file to edit (relative or absolute)"
		},
		"old_string": {
			"type": "string",
			"description": "The exact text to replace"
		},
		"new_string": {
			"type": "string",
			"description": "The replacement text"
		},
		"replace_all": {
			"type": "boolean",
			"description": "Replace all occurrences (default: false)"
		}
	},
	"required": ["path", "old_string", "new_string"]
}`),
	Execute: func(args map[string]interface{}) ToolResult {
		path, ok := args["path"].(string)
		if !ok || path == "" {
			return ToolResult{Status: ResultStatusError, Error: "path is required and must be a string"}
		}
		path = resolvePath(path)
		oldStr, ok := args["old_string"].(string)
		if !ok {
			return ToolResult{Status: ResultStatusError, Error: "old_string is required and must be a string"}
		}
		newStr, ok := args["new_string"].(string)
		if !ok {
			return ToolResult{Status: ResultStatusError, Error: "new_string is required and must be a string"}
		}
		replaceAll, _ := args["replace_all"].(bool)

		data, err := os.ReadFile(path)
		if err != nil {
			return ToolResult{Status: ResultStatusError, Error: fmt.Sprintf("failed to read file %s: %w", path, err)}
		}
		content := string(data)

		if replaceAll {
			imports := 0
			for {
				idx := indexStr(content, oldStr)
				if idx == -1 {
					break
				}
				content = content[:idx] + newStr + content[idx+len(oldStr):]
				imports++
			}
			if imports == 0 {
				return ToolResult{Status: ResultStatusSuccess, Result: "old_string not found, no changes made"}
			}
			writeErr := writeIfChanged(path, content, data)
			if writeErr != nil {
				return ToolResult{Status: ResultStatusError, Error: fmt.Sprintf("failed to write file %s: %w", path, writeErr)}
			}
			return ToolResult{Status: ResultStatusSuccess, Result: fmt.Sprintf("replaced %d occurrences in %s", imports, path)}
		}

		idx := indexStr(content, oldStr)
		if idx == -1 {
			return ToolResult{Status: ResultStatusSuccess, Result: "old_string not found, no changes made"}
		}
		content = content[:idx] + newStr + content[idx+len(oldStr):]
		writeErr := writeIfChanged(path, content, data)
		if writeErr != nil {
			return ToolResult{Status: ResultStatusError, Error: fmt.Sprintf("failed to write file %s: %w", path, writeErr)}
		}
		return ToolResult{Status: ResultStatusSuccess, Result: fmt.Sprintf("replaced 1 occurrence in %s", path)}
	},
}

func indexStr(s, substr string) int {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}

func writeIfChanged(path, content string, original []byte) error {
	if string(original) != content {
		return os.WriteFile(path, []byte(content), 0644)
	}
	return nil
}
