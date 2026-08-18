package media

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// MigrationResult describes the outcome of persisting a temporary workspace
// to a destination directory.
type MigrationResult struct {
	// Workspace is the new persistent workspace pointing at the destination.
	Workspace Workspace
	// Moved lists the file names that were transferred from the temp
	// directory to the destination.
	Moved []string
}

// PersistWorkspace moves all media files from a temporary workspace into the
// destination directory's media folder. It performs the move atomically per
// file (write-then-rename) and returns the list of successfully moved paths.
//
// If the move fails after some files have been transferred, the partially
// written destination is cleaned up so the caller can retry or fall back.
// Attachment metadata is NOT modified — the caller updates
// session documents separately.
//
// Returns a MigrationResult containing the new PersistentWorkspace and the
// list of moved relative paths.
func PersistWorkspace(ctx context.Context, src Workspace, destination string) (MigrationResult, error) {
	if src == nil {
		return MigrationResult{}, fmt.Errorf("source workspace is nil")
	}
	if destination == "" {
		return MigrationResult{}, fmt.Errorf("destination is empty")
	}

	srcMedia := src.Dir()
	dstWs := NewPersistentWorkspace(destination)
	dstMedia := dstWs.Dir()

	// If source media dir doesn't exist or is empty, nothing to move.
	entries, err := os.ReadDir(srcMedia)
	if err != nil {
		if os.IsNotExist(err) {
			return MigrationResult{
				Workspace: dstWs,
				Moved:     nil,
			}, nil
		}
		return MigrationResult{}, fmt.Errorf("read source media dir %s: %w", srcMedia, err)
	}
	if len(entries) == 0 {
		return MigrationResult{
			Workspace: dstWs,
			Moved:     nil,
		}, nil
	}

	// Collect files to move
	var files []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		files = append(files, e.Name())
	}

	// Create destination media directory
	if err := os.MkdirAll(dstMedia, 0755); err != nil {
		return MigrationResult{}, fmt.Errorf("create destination media dir: %w", err)
	}

	moved := make([]string, 0, len(files))

	// Move each file atomically
	for _, fileName := range files {
		srcPath := filepath.Join(srcMedia, fileName)
		dstPath := filepath.Join(dstMedia, fileName)

		if err := moveFile(srcPath, dstPath); err != nil {
			// Rollback: remove any files we've already moved
			for _, m := range moved {
				_ = os.Remove(filepath.Join(dstMedia, filepath.Base(m)))
			}
			_ = os.Remove(dstMedia)
			return MigrationResult{}, fmt.Errorf("move %s: %w", fileName, err)
		}
		moved = append(moved, fileName)
	}

	return MigrationResult{
		Workspace: dstWs,
		Moved:     moved,
	}, nil
}

// moveFile copies src to dst and removes src on success. It uses a
// temporary file + rename for atomicity.
func moveFile(src, dst string) error {
	srcFile, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("open source: %w", err)
	}
	defer srcFile.Close()

	// Write to a temp file in the destination directory, then rename
	tmpPath := dst + ".tmp"
	dstFile, err := os.Create(tmpPath)
	if err != nil {
		return fmt.Errorf("create temp dest: %w", err)
	}

	_, err = io.Copy(dstFile, srcFile)
	closeErr := dstFile.Close()
	if err != nil {
		_ = dstFile.Close()
		_ = os.Remove(tmpPath)
		return fmt.Errorf("copy content: %w", err)
	}
	if closeErr != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("close temp dest: %w", closeErr)
	}

	// Preserve permissions
	info, err := os.Stat(src)
	if err == nil {
		_ = os.Chmod(tmpPath, info.Mode())
	}

	if err := os.Rename(tmpPath, dst); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("rename to final dest: %w", err)
	}

	// Remove source only after successful rename
	if err := os.Remove(src); err != nil {
		return fmt.Errorf("remove source after move: %w", err)
	}
	return nil
}

// CopyWorkspace copies all media files from srcDir to destinationDir.
// It is used during session fork operations so the forked session has
// its own independent copy of the media.
//
// Attachment metadata is NOT modified — the caller updates session
// documents separately. Attachment references (FileName) remain
// valid because the directory layout is preserved.
func CopyWorkspace(ctx context.Context, srcDir, destinationDir string) error {
	if srcDir == "" {
		return fmt.Errorf("source directory is empty")
	}
	if destinationDir == "" {
		return fmt.Errorf("destination directory is empty")
	}

	srcMedia := filepath.Join(srcDir, "media")
	dstMedia := filepath.Join(destinationDir, "media")

	// If source media dir doesn't exist, nothing to copy.
	entries, err := os.ReadDir(srcMedia)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read source media dir %s: %w", srcMedia, err)
	}
	if len(entries) == 0 {
		return nil
	}

	// Create destination media directory
	if err := os.MkdirAll(dstMedia, 0755); err != nil {
		return fmt.Errorf("create destination media dir: %w", err)
	}

	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		srcPath := filepath.Join(srcMedia, e.Name())
		dstPath := filepath.Join(dstMedia, e.Name())

		if err := copyFile(srcPath, dstPath); err != nil {
			return fmt.Errorf("copy %s: %w", e.Name(), err)
		}
	}

	return nil
}

// copyFile copies the content of src to dst, preserving permissions.
func copyFile(src, dst string) error {
	srcFile, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("open source: %w", err)
	}
	defer srcFile.Close()

	// Preserve permissions
	info, err := srcFile.Stat()
	if err != nil {
		return fmt.Errorf("stat source: %w", err)
	}

	tmpPath := dst + ".tmp"
	dstFile, err := os.OpenFile(tmpPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, info.Mode())
	if err != nil {
		return fmt.Errorf("create dest: %w", err)
	}

	_, err = io.Copy(dstFile, srcFile)
	closeErr := dstFile.Close()
	if err != nil {
		_ = dstFile.Close()
		_ = os.Remove(tmpPath)
		return fmt.Errorf("copy content: %w", err)
	}
	if closeErr != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("close dest: %w", closeErr)
	}

	if err := os.Rename(tmpPath, dst); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("finalize copy: %w", err)
	}
	return nil
}

// MigrateTempWorkspace moves a temporary workspace's media files into the
// destination session directory and returns a new PersistentWorkspace.
// It is called during the first explicit save of an unsaved session.
//
// The migration is atomic: if any file fails to move, all previously
// moved files are cleaned up and the caller receives an error. This
// ensures the session document never references media that doesn't exist.
//
// After a successful migration, the caller should replace its workspace
// reference with the returned PersistentWorkspace and clear any temp
// directory tracking.
func MigrateTempWorkspace(ctx context.Context, src Workspace, destination string) (MigrationResult, error) {
	if src == nil {
		return MigrationResult{}, fmt.Errorf("source workspace is nil")
	}
	if destination == "" {
		return MigrationResult{}, fmt.Errorf("destination is empty")
	}

	return PersistWorkspace(ctx, src, destination)
}
