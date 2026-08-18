package app

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// FileSearchConfig controls bounded filesystem searches for file completion.
type FileSearchConfig struct {
	Roots      []string // directories to search (default: working directory)
	MaxDepth   int      // max directory depth from each root (default: 3)
	MaxResults int      // max files returned (default: 50)
	Ignore     []string // directory names to skip (default: hidden dirs + .git, node_modules, vendor)
}

// DefaultFileSearchConfig returns the default search config.
func DefaultFileSearchConfig() FileSearchConfig {
	return FileSearchConfig{
		Roots:      nil, // nil means "use working directory" — set explicitly in app
		MaxDepth:   3,
		MaxResults: 50,
		Ignore:     nil, // nil means use defaults
	}
}

// DefaultIgnoreDirs lists directories that should be skipped during file search.
var DefaultIgnoreDirs = []string{
	".git",
	".hg",
	".svn",
	"node_modules",
	"vendor",
	".idea",
	".vscode",
	"__pycache__",
	".pytest_cache",
	"dist",
	"build",
	"target",
	".next",
	".nuxt",
}

// ResolvedRoots returns the effective search roots.
// When Roots is nil or empty, it falls back to the working directory.
func (c FileSearchConfig) ResolvedRoots(workingDir string) []string {
	if len(c.Roots) == 0 {
		return []string{workingDir}
	}
	var resolved []string
	for _, root := range c.Roots {
		resolved = append(resolved, resolveSearchRoot(root, workingDir))
	}
	return resolved
}

// resolveSearchRoot expands a root path. Relative paths are resolved against
// the working directory. Paths with ~ are expanded to the home directory.
func resolveSearchRoot(root, workingDir string) string {
	if root == "." {
		return workingDir
	}
	if filepath.IsAbs(root) {
		return root
	}
	// Expand ~ to home directory
	if strings.HasPrefix(root, "~/") {
		home := os.Getenv("HOME")
		if home != "" {
			return filepath.Join(home, root[2:])
		}
		return root
	}
	return filepath.Join(workingDir, root)
}

// ResolvedIgnore returns the effective ignore list.
func (c FileSearchConfig) ResolvedIgnore() []string {
	if len(c.Ignore) == 0 {
		return DefaultIgnoreDirs
	}
	return c.Ignore
}

// ReferenceCandidate represents a file candidate for @ completion.
type ReferenceCandidate struct {
	Kind   string // "file"
	Name   string // display name (relative path)
	Source string // absolute file path
}

// SearchFiles walks configured roots and returns file candidates matching
// the query prefix. Results are bounded by MaxDepth and MaxResults.
func SearchFiles(cfg FileSearchConfig, workingDir string, query string) []ReferenceCandidate {
	roots := cfg.ResolvedRoots(workingDir)
	ignore := cfg.ResolvedIgnore()
	ignoreSet := make(map[string]bool, len(ignore))
	for _, name := range ignore {
		ignoreSet[name] = true
	}
	// Also ignore any directory whose name starts with a dot (hidden), unless
	// it's the root itself.
	var results []ReferenceCandidate
	seen := make(map[string]bool)

	for _, root := range roots {
		info, err := os.Stat(root)
		if err != nil || !info.IsDir() {
			continue
		}
		walkFiles(root, root, 0, cfg.MaxDepth, ignoreSet, query, &results, seen, cfg.MaxResults)
		if len(results) >= cfg.MaxResults {
			break
		}
	}

	// Sort: files before directories, then alphabetically by name.
	sort.Slice(results, func(i, j int) bool {
		nameI := results[i].Name
		nameJ := results[j].Name
		return nameI < nameJ
	})

	return results
}

// walkFiles recursively walks a directory tree up to maxDepth, collecting
// file matches for the query. It skips ignored directories.
func walkFiles(root, dir string, depth, maxDepth int, ignoreSet map[string]bool,
	query string, results *[]ReferenceCandidate, seen map[string]bool, maxResults int) {

	if depth > maxDepth || len(*results) >= maxResults {
		return
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}

	for _, entry := range entries {
		name := entry.Name()

		// Skip hidden files/directories (names starting with ".")
		if strings.HasPrefix(name, ".") {
			if entry.IsDir() {
				continue
			}
		}

		// Skip ignored directories
		if entry.IsDir() {
			if ignoreSet[name] {
				continue
			}
			// Recurse into non-ignored directories
			subDir := filepath.Join(dir, name)
			walkFiles(root, subDir, depth+1, maxDepth, ignoreSet, query, results, seen, maxResults)
			if len(*results) >= maxResults {
				return
			}
			continue
		}

	// It's a file — filter to attachable media only.
		ext := strings.ToLower(filepath.Ext(name))
		switch ext {
		// Images
		case ".png", ".jpg", ".jpeg", ".gif", ".webp", ".svg", ".bmp", ".ico", ".tiff", ".tif":
		// Documents
		case ".pdf", ".doc", ".docx", ".xls", ".xlsx", ".ppt", ".pptx":
		// Text
		case ".txt", ".md":
		// Audio
		case ".mp3", ".wav", ".ogg", ".flac", ".aac", ".m4a":
		// Video
		case ".mp4", ".webm", ".mov", ".avi", ".mkv":
		default:
			continue
		}

		// Check if it matches the query
		if query != "" && !strings.HasPrefix(name, query) {
			continue
		}

		absPath := filepath.Join(dir, name)
		if !seen[absPath] {
			seen[absPath] = true
			// Compute the display name: relative to root if possible.
			relPath := name
			if dir != root {
				if rel, err := filepath.Rel(root, absPath); err == nil {
					relPath = rel
				}
			}
			*results = append(*results, ReferenceCandidate{
				Kind:   "file",
				Name:   relPath,
				Source: absPath,
			})
		}
	}
}

// IsDirectPath checks whether the given string looks like a file path
// (absolute or relative) that might exist.
func IsDirectPath(s string) bool {
	// Quick heuristic: contains a path separator and doesn't look like a URL
	if strings.Contains(s, "://") {
		return false
	}
	return strings.ContainsAny(s, "/\\.") || strings.ContainsAny(s, "/\\")
}

// ResolveDirectPath attempts to resolve a direct path string to an
// absolute path. Returns the absolute path and true if the file exists,
// or the absolute path (possibly non-existent) and false otherwise.
func ResolveDirectPath(raw string, workingDir string) (string, bool) {
	if filepath.IsAbs(raw) {
		return raw, true
	}
	if strings.HasPrefix(raw, "~/") {
		home := os.Getenv("HOME")
		if home != "" {
			return filepath.Join(home, raw[2:]), true
		}
	}
	return filepath.Join(workingDir, raw), true
}

// FileExists checks whether the given path exists and is a file.
func FileExists(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return !info.IsDir()
}

// DirectPathCandidates checks if the query looks like a direct file path and
// returns a candidate if it resolves.
func DirectPathCandidates(query string, workingDir string) []ReferenceCandidate {
	if IsDirectPath(query) {
		abs, ok := ResolveDirectPath(query, workingDir)
		if ok && FileExists(abs) {
			return []ReferenceCandidate{{
				Kind:   "file",
				Name:   filepath.Base(abs),
				Source: abs,
			}}
		}
	}
	return nil
}

// fileCandidates returns file and URL candidates for the current @ completion query.
func (m Model) fileCandidates() []capabilityCandidate {
	lines := strings.Split(m.textarea.Value(), "\n")
	lineIndex := m.textarea.Line()
	if lineIndex < 0 || lineIndex >= len(lines) {
		return nil
	}

	line := []rune(lines[lineIndex])
	info := m.textarea.LineInfo()
	cursor := info.StartColumn + info.ColumnOffset
	if cursor < 0 || cursor > len(line) {
		return nil
	}

	workingDir := m.session.Doc.Config.WorkingDir
	if workingDir == "" {
		workingDir = m.session.Catalog.WorkingDir
	}
	if workingDir == "" {
		workingDir, _ = os.Getwd()
	}

	// Extract query from text after the last @.
	query := ""
	start := cursor - 1
	for start >= 0 && isCapabilityNameRune(line[start]) {
		start--
	}
	if start >= 0 && line[start] == '@' {
		query = string(line[start+1 : cursor])
	}

	// Check for direct path.
	if query != "" {
		direct := DirectPathCandidates(query, workingDir)
		if len(direct) > 0 {
			var result []capabilityCandidate
			for _, c := range direct {
				result = append(result, capabilityCandidate{
					kind:   c.Kind,
					name:   c.Name,
					source: c.Source,
				})
			}
			return result
		}
	}

	// Search files in configured roots.
	cfg := m.fileSearchConfig()
	searchResults := SearchFiles(cfg, workingDir, query)
	var result []capabilityCandidate
	for _, ref := range searchResults {
		result = append(result, capabilityCandidate{
			kind:   ref.Kind,
			name:   ref.Name,
			source: ref.Source,
		})
	}
	return result
}

// fileSearchConfig builds a FileSearchConfig from settings.
func (m Model) fileSearchConfig() FileSearchConfig {
	cfg := DefaultFileSearchConfig()
	fs := m.settings.FileSearch
	if len(fs.Roots) > 0 {
		cfg.Roots = fs.Roots
	}
	if fs.MaxDepth > 0 {
		cfg.MaxDepth = fs.MaxDepth
	}
	if fs.MaxResults > 0 {
		cfg.MaxResults = fs.MaxResults
	}
	if len(fs.Ignore) > 0 {
		cfg.Ignore = fs.Ignore
	}
	return cfg
}
