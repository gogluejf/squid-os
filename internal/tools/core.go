package tools

import (
	"fmt"
	"os"
)

// ReadFile reads a file and returns its contents.
var ReadFile = Tool{
	Name:        "read_file",
	Description: "Read the contents of a file at the given path. Returns the raw text content. Use for reading code, configs, documents, and any text-based files.",
	Schema: map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"path": map[string]interface{}{
				"type":        "string",
				"description": "Absolute path to the file to read",
			},
		},
		"required": []string{"path"},
	},
	Execute: func(args map[string]interface{}) (string, error) {
		path, ok := args["path"].(string)
		if !ok || path == "" {
			return "", fmt.Errorf("path is required and must be a string")
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return "", fmt.Errorf("failed to read file %s: %w", path, err)
		}
		return string(data), nil
	},
}

// WriteFile creates a new file or overwrites an existing one with the given content.
var WriteFile = Tool{
	Name:        "write_file",
	Description: "Create a new file or completely overwrite an existing file with the given content. Use for new files or full rewrites only. Path must be absolute.",
	Schema: map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"path": map[string]interface{}{
				"type":        "string",
				"description": "Absolute path to the file to write",
			},
			"content": map[string]interface{}{
				"type":        "string",
				"description": "The content to write to the file",
			},
		},
		"required": []string{"path", "content"},
	},
	Execute: func(args map[string]interface{}) (string, error) {
		path, ok := args["path"].(string)
		if !ok || path == "" {
			return "", fmt.Errorf("path is required and must be a string")
		}
		content, ok := args["content"].(string)
		if !ok {
			return "", fmt.Errorf("content is required and must be a string")
		}
		err := os.WriteFile(path, []byte(content), 0644)
		if err != nil {
			return "", fmt.Errorf("failed to write file %s: %w", path, err)
		}
		return fmt.Sprintf("file written: %s (%d bytes)", path, len(content)), nil
	},
}

// EditFile performs a precise string replacement in an existing file.
var EditFile = Tool{
	Name:        "edit_file",
	Description: "Perform a precise string replacement in an existing file. old_string must match exactly. replace_all replaces every occurrence. Prefer over write_file for modifications.",
	Schema: map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"path": map[string]interface{}{
				"type":        "string",
				"description": "Absolute path to the file to edit",
			},
			"old_string": map[string]interface{}{
				"type":        "string",
				"description": "The exact text to replace",
			},
			"new_string": map[string]interface{}{
				"type":        "string",
				"description": "The replacement text",
			},
			"replace_all": map[string]interface{}{
				"type":        "boolean",
				"description": "Replace all occurrences (default: false)",
			},
		},
		"required": []string{"path", "old_string", "new_string"},
	},
	Execute: func(args map[string]interface{}) (string, error) {
		path, ok := args["path"].(string)
		if !ok || path == "" {
			return "", fmt.Errorf("path is required and must be a string")
		}
		oldStr, ok := args["old_string"].(string)
		if !ok {
			return "", fmt.Errorf("old_string is required and must be a string")
		}
		newStr, ok := args["new_string"].(string)
		if !ok {
			return "", fmt.Errorf("new_string is required and must be a string")
		}
		replaceAll, _ := args["replace_all"].(bool)

		data, err := os.ReadFile(path)
		if err != nil {
			return "", fmt.Errorf("failed to read file %s: %w", path, err)
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
				return "old_string not found, no changes made", nil
			}
			return fmt.Sprintf("replaced %d occurrences in %s", imports, path), writeIfChanged(path, content, data)
		}

		idx := indexStr(content, oldStr)
		if idx == -1 {
			return "old_string not found, no changes made", nil
		}
		content = content[:idx] + newStr + content[idx+len(oldStr):]
		return fmt.Sprintf("replaced 1 occurrence in %s", path), writeIfChanged(path, content, data)
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
