package tools

import (
	"fmt"
	"os"
	"strings"

	"squid-os/internal/config"
	"squid-os/internal/style"
)

// ─── read_file ───────────────────────────────────────────────

var ReadFile = Tool{
	Name:         "read_file",
	Description:  "Read a complete file by default. For a partial read, provide both start_line and end_line.",
	DisplayParam: "path",
	Style:        style.ToolStyle(),
	Schema: []byte(`{
	"type": "object",
	"properties": {
		"path": {
			"type": "string",
			"description": "Path to the file to read (relative or absolute)"
		},
		"start_line": {
			"type": "integer",
			"description": "Optional 1-based start line, inclusive. For a full-file read, OMIT both start_line and end_line. Only provide this together with end_line when intentionally reading a specific range. Never guess a value."
		},
		"end_line": {
			"type": "integer",
			"description": "Optional 1-based end line, inclusive. For a full-file read, OMIT both start_line and end_line. Only provide this together with start_line when intentionally limiting the read to a known range. Never guess the file's final line number; values beyond EOF stop at EOF."
		}
	},
	"required": ["path"]
}`),
	Execute: func(args map[string]interface{}, rt RuntimeContext) ToolResult {
		path, ok := args["path"].(string)
		if !ok || path == "" {
			return ToolResult{Status: ResultStatusError, Error: "path is required and must be a string"}
		}
		path = ResolvePath(path, rt.Config.WorkingDir)
		data, err := os.ReadFile(path)
		if err != nil {
			return ToolResult{Status: ResultStatusError, Error: fmt.Sprintf("failed to read file %s: %v", path, err)}
		}

		startLine, endLine, ranged, err := ParseReadRange(args)
		if err != nil {
			return ToolResult{Status: ResultStatusError, Error: err.Error()}
		}

		if ranged {

			lines := strings.Split(string(data), "\n")
			// Validate range bounds against file length
			if startLine > len(lines) {
				return ToolResult{Status: ResultStatusError, Error: fmt.Sprintf("start_line %d exceeds file length (%d lines)", startLine, len(lines))}
			}
			if endLine > len(lines) {
				endLine = len(lines)
			}

			ranged := lines[startLine-1 : endLine]
			content := strings.Join(ranged, "\n")

			fe := BuildFileEntry(path, config.TraceRead, data, nil)
			return ToolResult{
				Status: ResultStatusSuccess,
				Result: content,
				Files:  []config.FileEntry{fe},
			}
		}

		fe := BuildFileEntry(path, config.TraceRead, data, nil)
		return ToolResult{
			Status: ResultStatusSuccess,
			Result: string(data),
			Files:  []config.FileEntry{fe},
		}
	},
}

// ParseReadRange classifies omitted or start_line <= 1-only ranges as full reads.
func ParseReadRange(args map[string]interface{}) (startLine, endLine int, ranged bool, err error) {
	startLine, hasStart, err := parseIntegralArg(args, "start_line")
	if err != nil {
		return 0, 0, false, err
	}
	endLine, hasEnd, err := parseIntegralArg(args, "end_line")
	if err != nil {
		return 0, 0, false, err
	}
	if hasStart && startLine <= 1 && !hasEnd {
		return 0, 0, false, nil
	}
	if !hasStart && !hasEnd {
		return 0, 0, false, nil
	}
	if !hasStart || !hasEnd {
		return 0, 0, false, fmt.Errorf("both start_line and end_line must be provided together")
	}
	if startLine <= 1 {
		startLine = 1
	}
	if endLine < 1 {
		return 0, 0, false, fmt.Errorf("line numbers must be positive (1-based), got start_line=%d end_line=%d", startLine, endLine)
	}
	if startLine > endLine {
		return 0, 0, false, fmt.Errorf("start_line (%d) must not exceed end_line (%d)", startLine, endLine)
	}
	return startLine, endLine, true, nil
}

// ─── write_file ──────────────────────────────────────────────

func doWriteFile(path, content string, dryRun bool) (ToolResult, error) {
	var oldData []byte
	existed := false
	if _, statErr := os.Stat(path); statErr == nil {
		existed = true
		oldData, _ = os.ReadFile(path)
	}

	if !dryRun {
		dir := path[:strings.LastIndex(path, "/")]
		if dir != "" {
			if err := os.MkdirAll(dir, 0755); err != nil {
				return ToolResult{}, fmt.Errorf("failed to create directory %s: %w", dir, err)
			}
		}
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			return ToolResult{}, fmt.Errorf("failed to write file %s: %w", path, err)
		}
	}

	trace := config.TraceCreate
	if existed {
		trace = config.TraceWrite
	}

	fe := BuildFileEntry(path, trace, oldData, []byte(content))
	return ToolResult{
		Status: ResultStatusSuccess,
		Result: fmt.Sprintf("file written: %s (%d bytes)", path, len(content)),
		Files:  []config.FileEntry{fe},
	}, nil
}

var WriteFile = Tool{
	Name:          "write_file",
	Description:   "Create a new file or completely overwrite an existing file with the given content. Use for new files or full rewrites only. Path can be relative to current directory or absolute.",
	DisplayParam:  "path",
	Style:         style.ToolStyle(),
	IsDestructive: func(args map[string]interface{}) bool { return true },
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
	Execute: func(args map[string]interface{}, rt RuntimeContext) ToolResult {
		path, ok := args["path"].(string)
		if !ok || path == "" {
			return ToolResult{Status: ResultStatusError, Error: "path is required and must be a string"}
		}
		path = ResolvePath(path, rt.Config.WorkingDir)
		content, ok := args["content"].(string)
		if !ok {
			return ToolResult{Status: ResultStatusError, Error: "content is required and must be a string"}
		}
		res, err := doWriteFile(path, content, false)
		if err != nil {
			return ToolResult{Status: ResultStatusError, Error: err.Error()}
		}
		return res
	},
	Preview: func(args map[string]interface{}, rt RuntimeContext) ToolResult {
		path, ok := args["path"].(string)
		if !ok || path == "" {
			return ToolResult{Status: ResultStatusError, Error: "path is required and must be a string"}
		}
		path = ResolvePath(path, rt.Config.WorkingDir)
		content, ok := args["content"].(string)
		if !ok {
			return ToolResult{Status: ResultStatusError, Error: "content is required and must be a string"}
		}
		res, err := doWriteFile(path, content, true)
		if err != nil {
			return ToolResult{Status: ResultStatusError, Error: err.Error()}
		}
		return res
	},
}

// ─── edit_file ───────────────────────────────────────────────

func doEditFile(path, oldStr, newStr string, replaceAll bool, dryRun bool) (ToolResult, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return ToolResult{}, fmt.Errorf("failed to read file %s: %w", path, err)
	}
	oldContent := string(data)

	var newContent string
	if replaceAll {
		newContent, _ = replaceAllOccurrences(oldContent, oldStr, newStr)
	} else {
		idx := indexStr(oldContent, oldStr)
		if idx == -1 {
			return ToolResult{
				Status: ResultStatusSuccess,
				Result: "old_string not found, no changes made",
				Files:  []config.FileEntry{BuildFileEntry(path, config.TraceRead, data, nil)},
			}, nil
		}
		newContent = oldContent[:idx] + newStr + oldContent[idx+len(oldStr):]
	}

	// If nothing changed, return read-only entry
	if oldContent == newContent {
		return ToolResult{
			Status: ResultStatusSuccess,
			Result: "no changes made",
			Files:  []config.FileEntry{BuildFileEntry(path, config.TraceRead, data, nil)},
		}, nil
	}

	if !dryRun {
		if err := os.WriteFile(path, []byte(newContent), 0644); err != nil {
			return ToolResult{}, fmt.Errorf("failed to write file %s: %w", path, err)
		}
	}

	fe := BuildFileEntry(path, config.TraceEdit, data, []byte(newContent))
	return ToolResult{
		Status: ResultStatusSuccess,
		Result: fmt.Sprintf("replaced in %s", path),
		Files:  []config.FileEntry{fe},
	}, nil
}

var EditFile = Tool{
	Name:          "edit_file",
	Description:   "Perform a precise string replacement in an existing file. old_string must match exactly. replace_all replaces every occurrence. Prefer over write_file for modifications. Path can be relative to current directory or absolute.",
	DisplayParam:  "path",
	Style:         style.ToolStyle(),
	IsDestructive: func(args map[string]interface{}) bool { return true },
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
	Execute: func(args map[string]interface{}, rt RuntimeContext) ToolResult {
		path, ok := args["path"].(string)
		if !ok || path == "" {
			return ToolResult{Status: ResultStatusError, Error: "path is required and must be a string"}
		}
		path = ResolvePath(path, rt.Config.WorkingDir)
		oldStr, ok := args["old_string"].(string)
		if !ok {
			return ToolResult{Status: ResultStatusError, Error: "old_string is required and must be a string"}
		}
		newStr, ok := args["new_string"].(string)
		if !ok {
			return ToolResult{Status: ResultStatusError, Error: "new_string is required and must be a string"}
		}
		replaceAll, _ := args["replace_all"].(bool)

		res, err := doEditFile(path, oldStr, newStr, replaceAll, false)
		if err != nil {
			return ToolResult{Status: ResultStatusError, Error: err.Error()}
		}
		return res
	},
	Preview: func(args map[string]interface{}, rt RuntimeContext) ToolResult {
		path, ok := args["path"].(string)
		if !ok || path == "" {
			return ToolResult{Status: ResultStatusError, Error: "path is required and must be a string"}
		}
		path = ResolvePath(path, rt.Config.WorkingDir)
		oldStr, ok := args["old_string"].(string)
		if !ok {
			return ToolResult{Status: ResultStatusError, Error: "old_string is required and must be a string"}
		}
		newStr, ok := args["new_string"].(string)
		if !ok {
			return ToolResult{Status: ResultStatusError, Error: "new_string is required and must be a string"}
		}
		replaceAll, _ := args["replace_all"].(bool)

		res, err := doEditFile(path, oldStr, newStr, replaceAll, true)
		if err != nil {
			return ToolResult{Status: ResultStatusError, Error: err.Error()}
		}
		return res
	},
}

// ─── helpers ─────────────────────────────────────────────────

func indexStr(s, substr string) int {
	return strings.Index(s, substr)
}

func replaceAllOccurrences(content, oldStr, newStr string) (string, int) {
	count := 0
	result := content
	for {
		idx := strings.Index(result, oldStr)
		if idx == -1 {
			break
		}
		result = result[:idx] + newStr + result[idx+len(oldStr):]
		count++
	}
	return result, count
}
