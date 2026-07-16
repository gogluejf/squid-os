package config

import (
	"io/fs"
	"os"
	"path/filepath"

	"squid-os/defaultconfig"
)

// InitConfig unpacks the embedded default config tree into the target directory
// if it doesn't already exist.  Once the user has a config directory we never
// overwrite — the user owns their config forever.
//
// Call this once at the very start of the application, before any other config
// is loaded.
func InitConfig(cfgDir string) error {
	// If config already exists, nothing to do.
	if _, err := os.Stat(cfgDir); err == nil {
		return nil
	}

	// Collect all files from the embedded FS (skip the top-level "." directory entry).
	var files []struct {
		path string
		data []byte
	}
	if err := fs.WalkDir(defaultconfig.Defaults, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if path == "." {
			return nil
		}
		if d.IsDir() {
			return nil
		}
		data, err := fs.ReadFile(defaultconfig.Defaults, path)
		if err != nil {
			return err
		}
		files = append(files, struct {
			path string
			data []byte
		}{path: path, data: data})
		return nil
	}); err != nil {
		return err
	}

	// Write each file into the target config directory.
	for _, f := range files {
		target := filepath.Join(cfgDir, f.path)

		if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
			return err
		}
		if err := os.WriteFile(target, f.data, 0644); err != nil {
			return err
		}
	}

	return nil
}
