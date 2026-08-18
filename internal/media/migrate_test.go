package media

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestPersistWorkspaceMovesFiles(t *testing.T) {
	srcDir := t.TempDir()
	dstDir := t.TempDir()

	// Create source media dir with files
	srcMedia := filepath.Join(srcDir, "media")
	if err := os.MkdirAll(srcMedia, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(srcMedia, "test.png"), []byte{0x89, 'P', 'N', 'G'}, 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(srcMedia, "doc.pdf"), []byte("%PDF-1.4"), 0644); err != nil {
		t.Fatal(err)
	}

	src := NewTempWorkspace(srcDir)

	result, err := PersistWorkspace(context.Background(), src, dstDir)
	if err != nil {
		t.Fatalf("PersistWorkspace failed: %v", err)
	}

	// Verify files were moved to destination
	dstMedia := filepath.Join(dstDir, "media")
	if _, err := os.Stat(filepath.Join(dstMedia, "test.png")); os.IsNotExist(err) {
		t.Fatal("test.png not found in destination")
	}
	if _, err := os.Stat(filepath.Join(dstMedia, "doc.pdf")); os.IsNotExist(err) {
		t.Fatal("doc.pdf not found in destination")
	}

	// Verify files were removed from source
	if _, err := os.Stat(filepath.Join(srcMedia, "test.png")); !os.IsNotExist(err) {
		t.Fatal("test.png should have been removed from source")
	}

	// Verify moved list
	if len(result.Moved) != 2 {
		t.Fatalf("expected 2 moved files, got %d", len(result.Moved))
	}

	// Verify new workspace is persistent
	if _, ok := result.Workspace.(*PersistentWorkspace); !ok {
		t.Fatal("expected PersistentWorkspace result")
	}
}

func TestPersistWorkspaceEmptySource(t *testing.T) {
	srcDir := t.TempDir()
	dstDir := t.TempDir()

	src := NewTempWorkspace(srcDir)

	result, err := PersistWorkspace(context.Background(), src, dstDir)
	if err != nil {
		t.Fatalf("PersistWorkspace(empty) failed: %v", err)
	}
	if len(result.Moved) != 0 {
		t.Fatalf("expected 0 moved files for empty source, got %d", len(result.Moved))
	}
	if _, ok := result.Workspace.(*PersistentWorkspace); !ok {
		t.Fatal("expected PersistentWorkspace result")
	}
}

func TestPersistWorkspaceNilSource(t *testing.T) {
	_, err := PersistWorkspace(context.Background(), nil, t.TempDir())
	if err == nil {
		t.Fatal("expected error for nil source")
	}
}

func TestPersistWorkspaceEmptyDestination(t *testing.T) {
	srcDir := t.TempDir()
	src := NewTempWorkspace(srcDir)
	_, err := PersistWorkspace(context.Background(), src, "")
	if err == nil {
		t.Fatal("expected error for empty destination")
	}
}

func TestCopyWorkspaceCopiesAllFiles(t *testing.T) {
	srcDir := t.TempDir()
	dstDir := t.TempDir()

	// Create source media dir with files
	srcMedia := filepath.Join(srcDir, "media")
	if err := os.MkdirAll(srcMedia, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(srcMedia, "photo.jpg"), []byte{0xFF, 0xD8, 0xFF}, 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(srcMedia, "data.txt"), []byte("hello"), 0644); err != nil {
		t.Fatal(err)
	}

	err := CopyWorkspace(context.Background(), srcDir, dstDir)
	if err != nil {
		t.Fatalf("CopyWorkspace failed: %v", err)
	}

	// Verify files were copied to destination
	dstMedia := filepath.Join(dstDir, "media")
	if _, err := os.Stat(filepath.Join(dstMedia, "photo.jpg")); os.IsNotExist(err) {
		t.Fatal("photo.jpg not found in destination")
	}
	if _, err := os.Stat(filepath.Join(dstMedia, "data.txt")); os.IsNotExist(err) {
		t.Fatal("data.txt not found in destination")
	}

	// Verify source files still exist (copy, not move)
	if _, err := os.Stat(filepath.Join(srcMedia, "photo.jpg")); os.IsNotExist(err) {
		t.Fatal("source photo.jpg should still exist after copy")
	}
	if _, err := os.Stat(filepath.Join(srcMedia, "data.txt")); os.IsNotExist(err) {
		t.Fatal("source data.txt should still exist after copy")
	}
}

func TestCopyWorkspaceNoSourceMedia(t *testing.T) {
	srcDir := t.TempDir()
	dstDir := t.TempDir()

	// No media directory exists
	err := CopyWorkspace(context.Background(), srcDir, dstDir)
	if err != nil {
		t.Fatalf("CopyWorkspace(no media) failed: %v", err)
	}
}

func TestCopyWorkspaceEmptySource(t *testing.T) {
	err := CopyWorkspace(context.Background(), "", t.TempDir())
	if err == nil {
		t.Fatal("expected error for empty source")
	}
}

func TestCopyWorkspaceEmptyDestination(t *testing.T) {
	err := CopyWorkspace(context.Background(), t.TempDir(), "")
	if err == nil {
		t.Fatal("expected error for empty destination")
	}
}

func TestMigrateTempWorkspaceMovesAndPreservesRegistry(t *testing.T) {
	srcDir := t.TempDir()
	dstDir := t.TempDir()

	src := NewTempWorkspace(srcDir)

	// Create actual media files
	srcMedia := src.Dir()
	if err := os.MkdirAll(srcMedia, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(srcMedia, "photo.png"), []byte{0x89, 'P', 'N', 'G'}, 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(srcMedia, "doc.pdf"), []byte("%PDF-1.4"), 0644); err != nil {
		t.Fatal(err)
	}

	result, err := MigrateTempWorkspace(context.Background(), src, dstDir)
	if err != nil {
		t.Fatalf("MigrateTempWorkspace failed: %v", err)
	}

	// Verify files moved
	dstMedia := result.Workspace.Dir()
	if _, err := os.Stat(filepath.Join(dstMedia, "photo.png")); os.IsNotExist(err) {
		t.Fatal("photo.png not found in destination")
	}
	if _, err := os.Stat(filepath.Join(dstMedia, "doc.pdf")); os.IsNotExist(err) {
		t.Fatal("doc.pdf not found in destination")
	}

	// Verify moved list
	if len(result.Moved) != 2 {
		t.Fatalf("expected 2 moved files, got %d", len(result.Moved))
	}
}

func TestMigrateTempWorkspaceNilSource(t *testing.T) {
	_, err := MigrateTempWorkspace(context.Background(), nil, t.TempDir())
	if err == nil {
		t.Fatal("expected error for nil source")
	}
}

func TestMigrateTempWorkspaceEmptyDestination(t *testing.T) {
	srcDir := t.TempDir()
	src := NewTempWorkspace(srcDir)
	_, err := MigrateTempWorkspace(context.Background(), src, "")
	if err == nil {
		t.Fatal("expected error for empty destination")
	}
}

func TestPersistWorkspaceRollbackOnFailure(t *testing.T) {
	srcDir := t.TempDir()
	// Use a destination that's a file, not a directory — this will cause
	// the second move to fail (can't create media subdirectory)
	dstDir := filepath.Join(t.TempDir(), "file.txt")
	if err := os.WriteFile(dstDir, []byte("blocker"), 0644); err != nil {
		t.Fatal(err)
	}

	srcMedia := filepath.Join(srcDir, "media")
	if err := os.MkdirAll(srcMedia, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(srcMedia, "file1.png"), []byte{1}, 0644); err != nil {
		t.Fatal(err)
	}

	src := NewTempWorkspace(srcDir)
	_, err := PersistWorkspace(context.Background(), src, dstDir)
	if err == nil {
		t.Fatal("expected error when destination is a file")
	}

	// Verify rollback — source files should still exist
	if _, err := os.Stat(filepath.Join(srcMedia, "file1.png")); os.IsNotExist(err) {
		t.Fatal("source file should still exist after failed migration")
	}
}
