package agent

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRegistryScanAndLoad(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "review")
	if err := os.Mkdir(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "agent.yaml"), []byte("name: review\ndescription: Reviews code\nmodel: openai/test\n"), 0644); err != nil {
		t.Fatal(err)
	}
	r, err := InitRegistry(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.List()) != 1 {
		t.Fatal("missing entry")
	}
	d, err := r.Load("review")
	if err != nil || d.Model != "openai/test" {
		t.Fatalf("%+v %v", d, err)
	}
}
