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
	if !strings.Contains(result, "+ -|3|c") {
		t.Errorf("expected added line '+ -|3|c', got: %q", result)
	}
}

func TestDiffRemoved(t *testing.T) {
	result := Diff("a\nb\nc", "a\nb")
	if result == "" {
		t.Fatal("expected non-empty diff")
	}
	if !strings.Contains(result, "- 3|-|c") {
		t.Errorf("expected removed line '- 3|-|c', got: %q", result)
	}
}

func TestDiffMixed(t *testing.T) {
	result := Diff("a\nb\nc\nd", "a\nx\nc\ne")
	if result == "" {
		t.Fatal("expected non-empty diff")
	}
	if !strings.Contains(result, "- 2|-|b") {
		t.Errorf("expected removed '- 2|-|b', got: %q", result)
	}
	if !strings.Contains(result, "+ -|2|x") {
		t.Errorf("expected added '+ -|2|x', got: %q", result)
	}
}
