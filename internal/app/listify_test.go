package app

import (
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/textarea"
)

func TestListifySplitsAtLogicalCursorAcrossSoftWrap(t *testing.T) {
	const text = "asdf sadfsadf sdf sadf sadf asdf dsf dsaf eee sadf sadf sadf sadf sadf d asdf sadf asdf sadfsdf sadfdf zzzze adsf asdf sadfsdf ee111122eee"
	const marker = "zzzz"
	cursor := strings.Index(text, marker)
	if cursor < 0 {
		t.Fatal("test marker missing")
	}

	ta := newListifyTextarea(text, 32, cursor)
	if info := ta.LineInfo(); info.StartColumn == 0 {
		t.Fatal("test cursor must be on a soft-wrapped visual row")
	}

	got := listify(ta)
	want := "1> " + text[:cursor] + "\n2> " + text[cursor:]
	if got.Value() != want {
		t.Fatalf("listify split at wrong position\ngot:  %q\nwant: %q", got.Value(), want)
	}
	if got.Line() != 1 || logicalCursorColumn(got) != len([]rune("2> ")) {
		t.Fatalf("cursor after split: line=%d col=%d", got.Line(), logicalCursorColumn(got))
	}
}

func TestRenumberInPlacePreservesLogicalCursorAcrossSoftWrap(t *testing.T) {
	const content = "this is a long broken numbered line that wraps several times before its cursor marker"
	value := "9> " + content
	cursor := len([]rune("9> this is a long broken numbered line that wraps"))

	ta := newListifyTextarea(value, 24, cursor)
	if !isNumberingBroken(ta) {
		t.Fatal("test input should have broken numbering")
	}
	got := renumberInPlace(ta)
	if got.Value() != "1> "+content {
		t.Fatalf("renumbered value = %q", got.Value())
	}
	// Prefix width is unchanged ("9> " -> "1> "), so logical cursor is exact.
	if logicalCursorColumn(got) != cursor {
		t.Fatalf("cursor moved from %d to %d", cursor, logicalCursorColumn(got))
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
