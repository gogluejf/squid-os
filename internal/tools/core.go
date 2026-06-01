package tools

import (
	"bufio"
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"

	"squid-os/internal/config"
	"squid-os/internal/diff"
	"squid-os/internal/style"
	"squid-os/internal/util"
)

// ─── read_file ───────────────────────────────────────────────

var ReadFile = Tool{
	Name:         "read_file",
	Description:  "Read the contents of a file at the given path (relative to current directory or absolute). Returns the raw text content. Use for reading code, configs, documents, and any text-based files.",
	DisplayParam: "path",
	Style:        style.ToolStyle(),
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
		fe := BuildFileEntry(path, config.TraceRead, data, nil)
		return ToolResult{
			Status: ResultStatusSuccess,
			Result: string(data),
			Files:  []config.FileEntry{fe},
		}
	},
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
	Name:         "write_file",
	Description:  "Create a new file or completely overwrite an existing file with the given content. Use for new files or full rewrites only. Path can be relative to current directory or absolute.",
	DisplayParam: "path",
	Style:        style.ToolStyle(),
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
		res, err := doWriteFile(path, content, false)
		if err != nil {
			return ToolResult{Status: ResultStatusError, Error: err.Error()}
		}
		return res
	},
	Preview: func(args map[string]interface{}) ToolResult {
		path, ok := args["path"].(string)
		if !ok || path == "" {
			return ToolResult{Status: ResultStatusError, Error: "path is required and must be a string"}
		}
		path = resolvePath(path)
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
	Name:         "edit_file",
	Description:  "Perform a precise string replacement in an existing file. old_string must match exactly. replace_all replaces every occurrence. Prefer over write_file for modifications. Path can be relative to current directory or absolute.",
	DisplayParam: "path",
	Style:        style.ToolStyle(),
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

		res, err := doEditFile(path, oldStr, newStr, replaceAll, false)
		if err != nil {
			return ToolResult{Status: ResultStatusError, Error: err.Error()}
		}
		return res
	},
	Preview: func(args map[string]interface{}) ToolResult {
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

		res, err := doEditFile(path, oldStr, newStr, replaceAll, true)
		if err != nil {
			return ToolResult{Status: ResultStatusError, Error: err.Error()}
		}
		return res
	},
}

// ─── helpers ─────────────────────────────────────────────────

// BuildFileEntry constructs a FileEntry based on old and new data.
// If newData is nil, it's a read-only operation.
// If oldData is nil and newData is present, it's a create operation.
// If both are present, it computes a diff.
func BuildFileEntry(path string, trace string, oldData, newData []byte) config.FileEntry {
	var checksum string
	var diffText string

	if newData != nil {
		checksum = util.ComputeChecksum(newData)
		if oldData != nil {
			diffText = diff.Diff(string(oldData), string(newData))
		} else {
			diffText = diff.Diff("", string(newData))
		}
	} else if oldData != nil {
		// Read-only
		checksum = util.ComputeChecksum(oldData)
	}

	return config.FileEntry{
		Path:     path,
		Trace:    trace,
		Checksum: checksum,
		Time:     time.Now(),
		Diff:     diffText,
	}
}

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

// computeChecksumIfReadable attempts to read and hash a file. Returns "" on error.
func computeChecksumIfReadable(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return util.ComputeChecksum(data)
}

// parseStraceOutput parses the output of a strace run and extracts file entries.
func parseStraceOutput(straceFile string) []config.FileEntry {
	data, err := os.ReadFile(straceFile)
	if err != nil {
		return nil
	}

	var files []config.FileEntry
	seen := make(map[string]bool)

	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	for scanner.Scan() {
		line := scanner.Text()
		syscall, fpath, ok := parseStraceLine(line)
		if !ok || fpath == "" || seen[fpath] {
			continue
		}
		seen[fpath] = true
		checksum := computeChecksumIfReadable(fpath)
		files = append(files, config.FileEntry{
			Path:     fpath,
			Trace:    syscall,
			Checksum: checksum,
			Time:     time.Now(),
		})
	}
	return files
}

func parseStraceLine(line string) (syscall, path string, ok bool) {
	re := regexp.MustCompile(`^(openat|creat|unlink|rmdir|mkdir|rename)\([^,]+,\s*"([^"]+)"`)
	m := re.FindStringSubmatch(line)
	if m == nil {
		return "", "", false
	}
	return m[1], m[2], true
}
