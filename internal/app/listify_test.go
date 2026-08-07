package app

import (
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/textarea"
)

func TestListifyStartsBelowPlainText(t *testing.T) {
	ta := newListifyTextarea("intro paragraph", 80, len("intro paragraph"))
	got := listify(ta)
	if got.Value() != "intro paragraph\n1> " {
		t.Fatalf("value = %q", got.Value())
	}
	assertListifyCursor(t, got, 1, len("1> "))
}

func TestListifyStartsAtEmptyCurrentLine(t *testing.T) {
	ta := newListifyTextarea("intro paragraph\n", 80, 0)
	ta.CursorDown()
	got := listify(ta)
	if got.Value() != "intro paragraph\n1> " {
		t.Fatalf("value = %q", got.Value())
	}
	assertListifyCursor(t, got, 1, len("1> "))
}

func TestListifyContinuesBlockFromBlankLineAfterNumberedItem(t *testing.T) {
	ta := newListifyTextarea("1> first\n", 80, 0)
	got := listify(ta)
	if got.Value() != "1> first\n2> " {
		t.Fatalf("value = %q", got.Value())
	}
	assertListifyCursor(t, got, 1, len("2> "))
}

func TestListifyCreatesIndependentLocalBlocks(t *testing.T) {
	value := "intro\n1> first\n2> second\noutro\nafter"
	ta := newListifyTextarea(value, 80, len("after"))
	for ta.Line() < 4 {
		ta.CursorDown()
	}
	got := listify(ta)
	want := value + "\n1> "
	if got.Value() != want {
		t.Fatalf("value = %q, want %q", got.Value(), want)
	}
}

func TestListifyContinuesOnlyContiguousNumberedBlock(t *testing.T) {
	value := "intro\n1> first\n2> second\noutro\n1> another"
	ta := newListifyTextarea(value, 80, len("1> another"))
	for ta.Line() < 4 {
		ta.CursorDown()
	}
	got := listify(ta)
	want := value + "\n2> "
	if got.Value() != want {
		t.Fatalf("value = %q, want %q", got.Value(), want)
	}
}

func TestListifySplitsNumberedLineAtLogicalCursorAcrossSoftWrap(t *testing.T) {
	const text = "asdf sadfsadf sdf sadf sadf asdf dsf dsaf eee sadf sadf sadf sadf sadf d asdf sadf asdf sadfsdf sadfdf zzzze adsf asdf sadfsdf ee111122eee"
	const marker = "zzzz"
	cursor := strings.Index(text, marker)
	if cursor < 0 {
		t.Fatal("test marker missing")
	}

	ta := newListifyTextarea("1> "+text, 32, len("1> ")+cursor)
	if info := ta.LineInfo(); info.StartColumn == 0 {
		t.Fatal("test cursor must be on a soft-wrapped visual row")
	}

	got := listify(ta)
	want := "1> " + text[:cursor] + "\n2> " + text[cursor:]
	if got.Value() != want {
		t.Fatalf("listify split at wrong position\ngot:  %q\nwant: %q", got.Value(), want)
	}
	assertListifyCursor(t, got, 1, len("2> "))
}

func TestListifyRenumbersOnlyCurrentBlock(t *testing.T) {
	value := "1> first\n8> second\nprose\n9> other"
	ta := newListifyTextarea(value, 80, 0)
	for ta.Line() > 1 {
		ta.CursorUp()
	}
	ta.SetCursor(len("8> second"))
	got := listify(ta)
	want := "1> first\n2> second\n3> \nprose\n9> other"
	if got.Value() != want {
		t.Fatalf("value = %q, want %q", got.Value(), want)
	}
}

func assertListifyCursor(t *testing.T, ta textarea.Model, line, col int) {
	t.Helper()
	if ta.Line() != line || logicalCursorColumn(ta) != col {
		t.Fatalf("cursor line=%d col=%d, want line=%d col=%d", ta.Line(), logicalCursorColumn(ta), line, col)
	}
}

func newListifyTextarea(value string, width, cursor int) textarea.Model {
	ta := textarea.New()
	ta.ShowLineNumbers = false
	ta.SetWidth(width)
	ta.SetValue(value)
	ta.SetCursor(cursor)
	return ta
}
