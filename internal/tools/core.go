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
		fe := config.FileEntry{
			Path:     path,
			Trace:    config.TraceRead,
			Checksum: util.ComputeChecksum(data),
			Time:     time.Now(),
		}
		return ToolResult{
			Status: ResultStatusSuccess,
			Result: string(data),
			Files:  []config.FileEntry{fe},
		}
	},
}

// ─── write_file ──────────────────────────────────────────────

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

		existed := false
		var diffText string
		if _, statErr := os.Stat(path); statErr == nil {
			existed = true
			oldData, readErr := os.ReadFile(path)
			if readErr == nil {
				diffText = diff.Diff(string(oldData), content)
			}
		}

		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			return ToolResult{Status: ResultStatusError, Error: fmt.Sprintf("failed to write file %s: %w", path, err)}
		}

		trace := config.TraceCreate
		if existed {
			trace = config.TraceWrite
		}

		fe := config.FileEntry{
			Path:     path,
			Trace:    trace,
			Checksum: util.ComputeChecksum([]byte(content)),
			Time:     time.Now(),
			Diff:     diffText,
		}
		return ToolResult{
			Status: ResultStatusSuccess,
			Result: fmt.Sprintf("file written: %s (%d bytes)", path, len(content)),
			Files:  []config.FileEntry{fe},
		}
	},
}

// ─── edit_file ───────────────────────────────────────────────

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

		data, err := os.ReadFile(path)
		if err != nil {
			return ToolResult{Status: ResultStatusError, Error: fmt.Sprintf("failed to read file %s: %w", path, err)}
		}
		oldContent := string(data)

		if replaceAll {
			newContent, count := replaceAllOccurrences(oldContent, oldStr, newStr)
			if count == 0 {
				fe := config.FileEntry{
					Path:     path,
					Trace:    config.TraceRead,
					Checksum: util.ComputeChecksum(data),
					Time:     time.Now(),
				}
				return ToolResult{
					Status: ResultStatusSuccess,
					Result: "old_string not found, no changes made",
					Files:  []config.FileEntry{fe},
				}
			}
			if err := os.WriteFile(path, []byte(newContent), 0644); err != nil {
				return ToolResult{Status: ResultStatusError, Error: fmt.Sprintf("failed to write file %s: %w", path, err)}
			}
			fe := config.FileEntry{
				Path:     path,
				Trace:    config.TraceEdit,
				Checksum: util.ComputeChecksum([]byte(newContent)),
				Time:     time.Now(),
				Diff:     diff.Diff(oldContent, newContent),
			}
			return ToolResult{
				Status: ResultStatusSuccess,
				Result: fmt.Sprintf("replaced %d occurrences in %s", count, path),
				Files:  []config.FileEntry{fe},
			}
		}

		idx := indexStr(oldContent, oldStr)
		if idx == -1 {
			fe := config.FileEntry{
				Path:     path,
				Trace:    "read",
				Checksum: util.ComputeChecksum(data),
				Time:     time.Now(),
			}
			return ToolResult{
				Status: ResultStatusSuccess,
				Result: "old_string not found, no changes made",
				Files:  []config.FileEntry{fe},
			}
		}

		newContent := oldContent[:idx] + newStr + oldContent[idx+len(oldStr):]
		if err := os.WriteFile(path, []byte(newContent), 0644); err != nil {
			return ToolResult{Status: ResultStatusError, Error: fmt.Sprintf("failed to write file %s: %w", path, err)}
		}
		fe := config.FileEntry{
			Path:     path,
			Trace:    "edit",
			Checksum: util.ComputeChecksum([]byte(newContent)),
			Time:     time.Now(),
			Diff:     diff.Diff(oldContent, newContent),
		}
		return ToolResult{
			Status: ResultStatusSuccess,
			Result: fmt.Sprintf("replaced 1 occurrence in %s", path),
			Files:  []config.FileEntry{fe},
		}
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
