// Command migrate-estimate-token rewrites persisted session documents under a
// sessions root. For each chat.json it:
//  1. sets every AttachmentRef.Tokens to 1 (stub value),
//  2. recomputes token_tally via CalculateTokenTally(),
//  3. saves through SaveSessionDoc (canonical current-schema form).
//
// It never modifies the source tree: it creates <root>.<UTC ts>.bck
// (content-identical backup) and <root>.<UTC ts>.new (migrated tree), then
// verifies the source is untouched and metadata matches before reporting.
//
// Usage: go run ./cmd/migrate-estimate-token <sessions-root> [-adopt]
//
// Without -adopt the .new tree is left for manual review. With -adopt the
// source directory is renamed to <root>.<ts>.old after verification and the
// .new tree is renamed to the original path. The .bck tree always remains.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"time"

	"squid-os/internal/chat"
	"squid-os/internal/config"
	"squid-os/internal/media"
)

func main() {
	adopt := flag.Bool("adopt", false, "replace the source tree with the migrated one (source kept as .old)")
	flag.Parse()
	if flag.NArg() < 1 {
		fmt.Fprintln(os.Stderr, "usage: migrate-estimate-token <sessions-root> [-adopt]")
		os.Exit(2)
	}
	root := filepath.Clean(flag.Arg(0))

	st, err := os.Stat(root)
	if err != nil || !st.IsDir() {
		fatalf("sessions root not a directory: %v", err)
	}
	ts := time.Now().UTC().Format("20060102T150405Z")
	bckRoot := fmt.Sprintf("%s.%s.bck", root, ts)
	newRoot := fmt.Sprintf("%s.%s.new", root, ts)
	for _, d := range []string{bckRoot, newRoot} {
		if _, err := os.Lstat(d); err == nil {
			fatalf("refusing to overwrite existing destination: %s", d)
		}
	}

	// Discover session files in the source tree.
	var relFiles []string
	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && d.Name() == "chat.json" {
			rel, err := filepath.Rel(root, path)
			if err != nil {
				return err
			}
			relFiles = append(relFiles, rel)
		}
		return nil
	})
	if err != nil {
		fatalf("discover: %v", err)
	}
	fmt.Printf("source:   %s\n", root)
	fmt.Printf("backup:   %s\n", bckRoot)
	fmt.Printf("migrated: %s\n", newRoot)
	fmt.Printf("files found: %d\n", len(relFiles))

	// Backup: content-identical copy of the whole tree.
	if err := copyTree(root, bckRoot); err != nil {
		fatalf("backup copy: %v", err)
	}

	// Migrate each session into the .new tree.
	var changed, unchanged, failed int
	var failures []string
	for _, rel := range relFiles {
		srcFile := filepath.Join(root, rel)
		dstFile := filepath.Join(newRoot, rel)
		sessionDir := filepath.Dir(srcFile)

		raw, err := os.ReadFile(srcFile)
		if err != nil {
			failed++
			failures = append(failures, fmt.Sprintf("%s: read: %v", rel, err))
			continue
		}

		var doc config.SessionDoc
		if err := json.Unmarshal(raw, &doc); err != nil {
			// Legacy shapes that no longer unmarshal. Copy verbatim.
			fmt.Printf("SKIP (legacy shape, copied verbatim): %s: %v\n", rel, err)
			if err := os.MkdirAll(filepath.Dir(dstFile), 0o755); err != nil || copyFile(srcFile, dstFile) != nil {
				failed++
				failures = append(failures, fmt.Sprintf("%s: verbatim copy: %v", rel, err))
			} else {
				unchanged++
			}
			continue
		}

		// Stub all attachment ref tokens to 1.
		stubAttachmentTokens(&doc)

		// Rebuild the tally with production semantics.
		s := &chat.Session{
			Doc:        doc,
			SessionDir: filepath.Dir(dstFile),
		}
		s.Workspace = media.NewPersistentWorkspace(s.SessionDir)
		oldCtx := doc.TokenTally
		s.RefreshTokenTally()

		if ctxFingerprint(oldCtx) == ctxFingerprint(s.Doc.TokenTally) {
			unchanged++
		} else {
			changed++
		}

		// SaveSessionDoc bumps meta.updated_at and the session dir mtime;
		// restore both so the migration only touches token_tally content.
		origCreatedAt, origUpdatedAt := doc.Meta.CreatedAt, doc.Meta.UpdatedAt
		srcDirInfo, _ := os.Stat(sessionDir)
		if err := config.SaveSessionDoc(s.SessionDir, s.Doc, s.Doc.TokenTally); err != nil {
			failed++
			failures = append(failures, fmt.Sprintf("%s: save: %v", rel, err))
			continue
		}
		restoreMeta(dstFile, origCreatedAt, origUpdatedAt)
		if fi, err := os.Stat(srcFile); err == nil {
			os.Chmod(dstFile, fi.Mode().Perm())
		}
		if srcDirInfo != nil {
			os.Chtimes(filepath.Dir(dstFile), srcDirInfo.ModTime(), srcDirInfo.ModTime())
		}
	}

	// Final metadata pass.
	if err := restoreTreeMetadata(root, newRoot); err != nil {
		fatalf("restore .new metadata: %v", err)
	}

	// Verify the source tree is byte-identical to the backup.
	if errs := diffTrees(root, bckRoot); len(errs) > 0 {
		for i, e := range errs {
			if i >= 10 {
				break
			}
			fmt.Fprintln(os.Stderr, "SOURCE CHANGED:", e)
		}
		fatalf("source tree differs from backup — aborting, nothing adopted")
	}

	// Verify metadata preservation.
	metaErrs := verifyMetadata(root, newRoot)
	metaErrs = append(metaErrs, verifyMetadata(root, bckRoot)...)
	if len(metaErrs) > 0 {
		for i, e := range metaErrs {
			if i >= 10 {
				break
			}
			fmt.Fprintln(os.Stderr, "METADATA MISMATCH:", e)
		}
		fatalf("metadata not preserved — .new/.bck trees unsafe to adopt")
	}
	fmt.Println("metadata verified: modes and mtimes match source in .new and .bck")

	fmt.Printf("tally changed: %d, unchanged: %d, failed: %d\n", changed, unchanged, failed)
	for _, f := range failures {
		fmt.Fprintln(os.Stderr, "FAIL:", f)
	}
	if failed > 0 {
		fmt.Println("result: NOT safe to adopt (failures present)")
		os.Exit(1)
	}
	fmt.Println("source verified untouched; .new tree ready for review")

	if !*adopt {
		fmt.Println("re-run with -adopt to replace the source tree (source will be kept as .old)")
		return
	}

	oldRoot := fmt.Sprintf("%s.%s.old", root, ts)
	if err := os.Rename(root, oldRoot); err != nil {
		fatalf("rename source aside: %v", err)
	}
	if err := os.Rename(newRoot, root); err != nil {
		if rb := os.Rename(oldRoot, root); rb != nil {
			fatalf("ADOPT FAILED AND ROLLBACK FAILED: source at %s, migrated at %s — resolve manually", oldRoot, newRoot)
		}
		fatalf("adopt rename failed (rolled back): %v", err)
	}
	fmt.Printf("adopted: %s (previous source kept at %s)\n", root, oldRoot)
}

// stubAttachmentTokens sets every AttachmentRef.Tokens to 1 across all
// messages and tool call execution attachments.
func stubAttachmentTokens(doc *config.SessionDoc) {
	for i := range doc.Messages {
		for j := range doc.Messages[i].Attachments {
			doc.Messages[i].Attachments[j].Tokens = 1
		}
		for k := range doc.Messages[i].ToolCalls {
			exec := &doc.Messages[i].ToolCalls[k].Execution
			for j := range exec.Attachments {
				exec.Attachments[j].Tokens = 1
			}
		}
	}
}

// ctxFingerprint summarizes the context tally for change detection.
func ctxFingerprint(tt *config.TokenTally) string {
	if tt == nil {
		return "nil"
	}
	c := tt.Context
	return fmt.Sprintf("%d|%d|%d|%d|%d|%d|%d|%d|%d|%d",
		c.Raw, c.RawInput, c.RawOutput, c.Compacted, c.CompactedInput,
		c.CompactedOutput, c.Saved, c.SavedInstruction, c.SavedExecution, c.ToolDefinitions)
}

// restoreMeta rewrites meta.created_at/updated_at back to their pre-save values.
func restoreMeta(file, createdAt, updatedAt string) {
	data, err := os.ReadFile(file)
	if err != nil {
		fatalf("restoreMeta read %s: %v", file, err)
	}
	var doc config.SessionDoc
	if err := json.Unmarshal(data, &doc); err != nil {
		fatalf("restoreMeta parse %s: %v", file, err)
	}
	doc.Meta.CreatedAt = createdAt
	doc.Meta.UpdatedAt = updatedAt
	out, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		fatalf("restoreMeta marshal %s: %v", file, err)
	}
	if err := os.WriteFile(file, out, 0644); err != nil {
		fatalf("restoreMeta write %s: %v", file, err)
	}
}

// copyTree copies src to dst preserving file modes and timestamps.
func copyTree(src, dst string) error {
	var dirs []string
	err := filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			dirs = append(dirs, target)
			return os.MkdirAll(target, 0o775)
		}
		if err := copyFile(path, target); err != nil {
			return err
		}
		info, err := os.Stat(path)
		if err != nil {
			return err
		}
		if err := os.Chtimes(target, info.ModTime(), info.ModTime()); err != nil {
			return err
		}
		return os.Chmod(target, info.Mode().Perm())
	})
	if err != nil {
		return err
	}
	sort.Slice(dirs, func(i, j int) bool { return len(dirs[i]) > len(dirs[j]) })
	for _, target := range dirs {
		if target == dst {
			continue
		}
		rel, err := filepath.Rel(dst, target)
		if err != nil {
			continue
		}
		info, err := os.Stat(filepath.Join(src, rel))
		if err != nil {
			continue
		}
		if err := os.Chmod(target, info.Mode().Perm()); err != nil {
			return err
		}
		if err := os.Chtimes(target, info.ModTime(), info.ModTime()); err != nil {
			return err
		}
	}
	return nil
}

// restoreTreeMetadata re-applies source modes and mtimes to an entire dst tree.
func restoreTreeMetadata(src, dst string) error {
	var entries []string
	err := filepath.WalkDir(dst, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		entries = append(entries, path)
		return nil
	})
	if err != nil {
		return err
	}
	sort.Slice(entries, func(i, j int) bool { return len(entries[i]) > len(entries[j]) })
	for _, target := range entries {
		if target == dst {
			continue
		}
		rel, err := filepath.Rel(dst, target)
		if err != nil {
			continue
		}
		info, err := os.Stat(filepath.Join(src, rel))
		if err != nil {
			continue
		}
		if err := os.Chmod(target, info.Mode().Perm()); err != nil {
			return err
		}
		if err := os.Chtimes(target, info.ModTime(), info.ModTime()); err != nil {
			return err
		}
	}
	return nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	si, err := in.Stat()
	if err != nil {
		return err
	}
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, si.Mode().Perm())
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, in)
	return err
}

// diffTrees reports content differences between two trees.
func diffTrees(a, b string) []string {
	var errs []string
	filepath.WalkDir(b, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", path, err))
			return nil
		}
		rel, _ := filepath.Rel(b, path)
		other := filepath.Join(a, rel)
		if d.IsDir() {
			if oi, err := os.Stat(other); err != nil || !oi.IsDir() {
				errs = append(errs, "missing dir in "+a+": "+rel)
			}
			return nil
		}
		da, err1 := os.ReadFile(path)
		db, err2 := os.ReadFile(other)
		if err1 != nil || err2 != nil {
			errs = append(errs, fmt.Sprintf("read %s: %v / %v", rel, err1, err2))
			return nil
		}
		if string(da) != string(db) {
			errs = append(errs, "content differs: "+rel)
		}
		return nil
	})
	return errs
}

// verifyMetadata compares mode and mtime of every entry in dst against src.
func verifyMetadata(src, dst string) []string {
	var errs []string
	filepath.WalkDir(dst, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", path, err))
			return nil
		}
		if path == dst {
			return nil
		}
		rel, _ := filepath.Rel(dst, path)
		orig := filepath.Join(src, rel)
		di, derr := os.Stat(path)
		oi, oerr := os.Stat(orig)
		if derr != nil || oerr != nil {
			errs = append(errs, fmt.Sprintf("stat %s: %v / %v", rel, derr, oerr))
			return nil
		}
		if di.Mode().Perm() != oi.Mode().Perm() {
			errs = append(errs, fmt.Sprintf("mode differs: %s (%o vs %o)", rel, di.Mode().Perm(), oi.Mode().Perm()))
		}
		if !di.ModTime().Equal(oi.ModTime()) {
			errs = append(errs, fmt.Sprintf("mtime differs: %s (%s vs %s)", rel, di.ModTime().Format(time.RFC3339), oi.ModTime().Format(time.RFC3339)))
		}
		return nil
	})
	return errs
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "error: "+format+"\n", args...)
	os.Exit(1)
}
