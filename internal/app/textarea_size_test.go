package app

import (
	"strings"
	"testing"
)

func TestTextareaVisualRowsCountsHardAndSoftLines(t *testing.T) {
	if got := textareaVisualRows("short\nshort", 20); got != 2 {
		t.Fatalf("hard rows = %d, want 2", got)
	}
	long := strings.Repeat("word ", 12)
	got := textareaVisualRows(long+"\nshort", 20)
	if got <= 2 {
		t.Fatalf("visual rows = %d, want soft wraps plus hard line", got)
	}
}

func TestAutoSizeTextareaUsesVisualRows(t *testing.T) {
	m := newCompletionTestModel(t, nil, nil)
	m.width = 32
	m.recalcLayout()
	m.textarea.SetValue(strings.Repeat("wrapped words ", 12))
	m.autoSizeTextarea()

	want := textareaVisualRows(m.textarea.Value(), m.textarea.Width())
	if want < 3 {
		t.Fatalf("test input did not wrap: %d rows", want)
	}
	if m.textarea.Height() != want {
		t.Fatalf("textarea height = %d, want visual rows %d", m.textarea.Height(), want)
	}
}

func TestAutoSizeTextareaCapsVisualRows(t *testing.T) {
	m := newCompletionTestModel(t, nil, nil)
	m.width = 20
	m.recalcLayout()
	m.textarea.SetValue(strings.Repeat("verylongword ", 100))
	m.autoSizeTextarea()
	if m.textarea.Height() != m.textarea.MaxHeight {
		t.Fatalf("height = %d, want cap %d", m.textarea.Height(), m.textarea.MaxHeight)
	}
}
