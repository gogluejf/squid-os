package diff

import (
	"strings"
	"testing"
)

func TestDiffIdentical(t *testing.T) {
	result := Diff("hello\nworld", "hello\nworld")
	if result != "" {
		t.Fatalf("expected empty diff, got: %q", result)
	}
}

func TestDiffAdded(t *testing.T) {
	result := Diff("a\nb", "a\nb\nc")
	if result == "" {
		t.Fatal("expected non-empty diff")
	}
	if !strings.Contains(result, "+ c") {
		t.Errorf("expected added line '+ c', got: %q", result)
	}
}

func TestDiffRemoved(t *testing.T) {
	result := Diff("a\nb\nc", "a\nb")
	if result == "" {
		t.Fatal("expected non-empty diff")
	}
	if !strings.Contains(result, "- c") {
		t.Errorf("expected removed line '- c', got: %q", result)
	}
}

func TestDiffMixed(t *testing.T) {
	result := Diff("a\nb\nc\nd", "a\nx\nc\ne")
	if result == "" {
		t.Fatal("expected non-empty diff")
	}
	if !strings.Contains(result, "- b") {
		t.Errorf("expected removed '- b', got: %q", result)
	}
	if !strings.Contains(result, "+ x") {
		t.Errorf("expected added '+ x', got: %q", result)
	}
}
