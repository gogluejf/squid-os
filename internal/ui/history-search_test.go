package ui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"squid-os/internal/config"
)

var testHistoryTime = time.Date(2026, 7, 16, 12, 0, 0, 0, time.UTC)

func TestHistorySearchOrdersMatchesNewestFirst(t *testing.T) {
	hs := NewHistorySearchOverlay(entries(
		"oldest git status",
		"middle git diff",
		"newest git commit",
	))

	hs.Filter("git")

	assertItems(t, hs.FilteredItems(), []string{
		"newest git commit",
		"middle git diff",
		"oldest git status",
	})
	assertSelected(t, &hs, "newest git commit", 0)
}

func TestHistorySearchFindsThreeMatchesAmongTwentyNewestFirst(t *testing.T) {
	items := make([]config.HistoryEntry, 20)
	for i := range items {
		items[i] = entry(fmt.Sprintf("record %02d", i+1))
	}
	items[3] = entry("deploy old")
	items[10] = entry("deploy middle")
	items[18] = entry("deploy new")

	hs := NewHistorySearchOverlay(items)
	hs.Filter("deploy")

	assertItems(t, hs.FilteredItems(), []string{
		"deploy new",
		"deploy middle",
		"deploy old",
	})
	assertSelected(t, &hs, "deploy new", 0)
	assertRenderContains(t, &hs, "1/3")
}

func TestHistorySearchIsCaseInsensitive(t *testing.T) {
	hs := NewHistorySearchOverlay(entries(
		"Git Status",
		"git diff",
		"GIT commit",
	))

	hs.Filter("git")

	assertItems(t, hs.FilteredItems(), []string{
		"GIT commit",
		"git diff",
		"Git Status",
	})
}

func TestHistorySearchNoMatches(t *testing.T) {
	hs := NewHistorySearchOverlay(entries("git status", "go test"))

	hs.Filter("missing")
	hs.NextMatch()
	hs.PrevMatch()

	if got := len(hs.FilteredItems()); got != 0 {
		t.Fatalf("len(filtered) = %d, want 0", got)
	}
	assertSelected(t, &hs, "", 0)
	assertRenderContains(t, &hs, "no matches")
}

func TestHistorySearchNextMatchMovesToOlderResultAndUpdatesNumber(t *testing.T) {
	hs := NewHistorySearchOverlay(entries(
		"oldest git status",
		"middle git diff",
		"newest git commit",
	))

	hs.Filter("git")
	assertSelected(t, &hs, "newest git commit", 0)
	assertRenderContains(t, &hs, "1/3")

	hs.NextMatch()
	assertSelected(t, &hs, "middle git diff", 1)
	assertRenderContains(t, &hs, "2/3")

	hs.NextMatch()
	assertSelected(t, &hs, "oldest git status", 2)
	assertRenderContains(t, &hs, "3/3")
}

func TestHistorySearchNextMatchWrapsToNewest(t *testing.T) {
	hs := NewHistorySearchOverlay(entries("oldest git", "middle git", "newest git"))

	hs.Filter("git")
	hs.NextMatch()
	hs.NextMatch()
	hs.NextMatch()

	assertSelected(t, &hs, "newest git", 0)
	assertRenderContains(t, &hs, "1/3")
}

func TestHistorySearchPrevMatchWrapsToOldest(t *testing.T) {
	hs := NewHistorySearchOverlay(entries("oldest git", "middle git", "newest git"))

	hs.Filter("git")
	hs.PrevMatch()

	assertSelected(t, &hs, "oldest git", 2)
	assertRenderContains(t, &hs, "3/3")
}

func TestHistorySearchFilterResetStartsAtFirstMatch(t *testing.T) {
	hs := NewHistorySearchOverlay(entries(
		"oldest git",
		"middle git",
		"newest git",
		"newest commit",
	))

	hs.Filter("git")
	hs.NextMatch()
	hs.NextMatch()
	assertSelected(t, &hs, "oldest git", 2)

	hs.Filter("commit")
	assertSelected(t, &hs, "newest commit", 0)
}

func TestHistorySearchKeepsMostRecentDuplicate(t *testing.T) {
	hs := NewHistorySearchOverlay(entries(
		"git status",
		"git diff",
		"git status",
	))

	hs.Filter("git")

	assertItems(t, hs.FilteredItems(), []string{
		"git status",
		"git diff",
	})
}

func TestHistorySearchNilHistoryIsSafe(t *testing.T) {
	hs := NewHistorySearchOverlay(nil)

	hs.Filter("git")
	hs.NextMatch()
	hs.PrevMatch()

	if hs.FilteredItems() == nil {
		// nil is acceptable; this assertion documents that no match state is expected.
	} else if got := len(hs.FilteredItems()); got != 0 {
		t.Fatalf("len(filtered) = %d, want 0", got)
	}
	assertSelected(t, &hs, "", 0)
	assertRenderContains(t, &hs, "no matches")
}

func TestHistorySearchRenderWithEmptyFilterHidesCount(t *testing.T) {
	hs := NewHistorySearchOverlay(entries("git status", "git diff"))

	rendered := hs.Render(120)
	if strings.Contains(rendered, "/2") {
		t.Fatalf("render with empty filter should not show match count: %q", rendered)
	}
	if !strings.Contains(rendered, "esc to exit") {
		t.Fatalf("render = %q, want esc hint", rendered)
	}
}

func TestHistorySearchSelectedTextClampsOutOfRangeIndex(t *testing.T) {
	hs := NewHistorySearchOverlay(entries("oldest git", "middle git", "newest git"))

	hs.Filter("git")
	hs.MatchIdx = 99

	assertSelected(t, &hs, "oldest git", 2)
}

func TestHistorySearchRenderShowsSelectedDate(t *testing.T) {
	hs := NewHistorySearchOverlay([]config.HistoryEntry{{
		Text:      "git status",
		CreatedAt: time.Now().Add(-2 * time.Hour),
	}})

	hs.Filter("git")

	assertRenderContains(t, &hs, "2 hours ago")
}

func entries(texts ...string) []config.HistoryEntry {
	items := make([]config.HistoryEntry, 0, len(texts))
	for _, text := range texts {
		items = append(items, entry(text))
	}
	return items
}

func entry(text string) config.HistoryEntry {
	return config.HistoryEntry{Text: text, CreatedAt: testHistoryTime}
}

func assertItems(t *testing.T, got []config.HistoryEntry, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("len(filtered) = %d, want %d; got %#v", len(got), len(want), got)
	}
	for i := range want {
		if got[i].Text != want[i] {
			t.Fatalf("filtered[%d] = %q, want %q; got %#v", i, got[i].Text, want[i], got)
		}
	}
}

func assertSelected(t *testing.T, hs *HistorySearchOverlay, wantText string, wantIdx int) {
	t.Helper()
	if got := hs.SelectedText(); got != wantText {
		t.Fatalf("SelectedText() = %q, want %q", got, wantText)
	}
	if got := hs.MatchIdx; got != wantIdx {
		t.Fatalf("MatchIdx = %d, want %d", got, wantIdx)
	}
}

func assertRenderContains(t *testing.T, hs *HistorySearchOverlay, want string) {
	t.Helper()
	if got := hs.Render(120); !strings.Contains(got, want) {
		t.Fatalf("Render() = %q, want substring %q", got, want)
	}
}
