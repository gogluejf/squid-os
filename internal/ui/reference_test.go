package ui

import (
	"strings"
	"testing"

	"squid-os/internal/media"
)

func TestParseReferences(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  int // number of matches
	}{
		{"file ref", "@file:/home/user/main.go", 1},
		{"skill ref", "@skill:browser-use", 1},
		{"agent ref", "@agent:trader", 1},
		{"tool ref", "@tool:bash", 1},
		{"multiple refs", "read @file:/x.go and @skill:plan-generator", 2},
		{"no refs", "hello world", 0},
		{"malformed", "@unknown/name", 0},
		{"bare at", "@name", 0},
		{"file with spaces after", "@file:/path/to/file.txt rest of text", 1},
		{"all four kinds", "@file:/a @skill:s @agent:a @tool:t", 4},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseReferences(tt.input)
			if len(got) != tt.want {
				t.Errorf("parseReferences() = %d matches, want %d", len(got), tt.want)
			}
		})
	}
}

func TestParseReferencesKinds(t *testing.T) {
	tests := []struct {
		input string
		kind  ReferenceKind
		name  string
	}{
		{"@file:/path/to/file", ReferenceFile, "/path/to/file"},
		{"@skill:my-skill", ReferenceSkill, "my-skill"},
		{"@agent:my-agent", ReferenceAgent, "my-agent"},
		{"@tool:my-tool", ReferenceTool, "my-tool"},
	}
	for _, tt := range tests {
		matches := parseReferences(tt.input)
		if len(matches) != 1 {
			t.Fatalf("expected 1 match for %q, got %d", tt.input, len(matches))
		}
		if matches[0].kind != tt.kind {
			t.Errorf("kind = %q, want %q", matches[0].kind, tt.kind)
		}
		if matches[0].name != tt.name {
			t.Errorf("name = %q, want %q", matches[0].name, tt.name)
		}
	}
}

func TestRenderReferences_NoRefs(t *testing.T) {
	input := "Hello world, no references here."
	got := RenderReferences(input, "236", nil)
	if got != input {
		t.Errorf("RenderReferences() = %q, want %q", got, input)
	}
}

func TestRenderReferences_ChipPresent(t *testing.T) {
	tests := []struct {
		input     string
		wantEmoji string
		wantName  string
	}{
		{"@file:/x.go", "❓", "/x.go"},
		{"@skill:skill-name", "⚡", "skill-name"},
		{"@agent:agent-name", "🧠", "agent-name"},
		{"@tool:tool-name", "🔧", "tool-name"},
	}
	for _, tt := range tests {
		got := RenderReferences(tt.input, "236", nil)
		if !strings.Contains(got, tt.wantEmoji) {
			t.Errorf("RenderReferences(%q) = %q, missing emoji %q", tt.input, got, tt.wantEmoji)
		}
		if !strings.Contains(got, tt.wantName) {
			t.Errorf("RenderReferences(%q) = %q, missing name %q", tt.input, got, tt.wantName)
		}
	}
}

func TestRenderReferences_PreservesSurroundingText(t *testing.T) {
	input := "Read @file:/main.go then run tests."
	got := RenderReferences(input, "236", nil)
	if !strings.Contains(got, "Read ") {
		t.Errorf("leading text lost: got %q", got)
	}
	if !strings.Contains(got, "then run tests.") {
		t.Errorf("trailing text lost: got %q", got)
	}
}

func TestRenderReferences_MalformedRemainsPlainText(t *testing.T) {
	input := "@unknown/name and @bare"
	got := RenderReferences(input, "236", nil)
	if got != input {
		t.Errorf("malformed refs should pass through unchanged: got %q, want %q", got, input)
	}
}

func TestRenderReferences_Multiple(t *testing.T) {
	input := "@file:/a.go and @skill:x and @tool:y"
	got := RenderReferences(input, "236", nil)
	if !strings.Contains(got, "❓") {
		t.Error("missing file emoji")
	}
	if !strings.Contains(got, "⚡") {
		t.Error("missing skill emoji")
	}
	if !strings.Contains(got, "🔧") {
		t.Error("missing tool emoji")
	}
	if !strings.Contains(got, "and") {
		t.Error("surrounding text lost")
	}
}

func TestRenderReferences_FileKindIcons(t *testing.T) {
	registry := []media.Attachment{
		{ID: "img-id", FileName: "img.png", MIME: "image/png", Kind: media.KindImage},
		{ID: "pdf-id", FileName: "doc.pdf", MIME: "application/pdf", Kind: media.KindPDF},
		{ID: "txt-id", FileName: "note.txt", MIME: "text/plain", Kind: media.KindText},
		{ID: "aud-id", FileName: "song.mp3", MIME: "audio/mpeg", Kind: media.KindAudio},
		{ID: "vid-id", FileName: "clip.mp4", MIME: "video/mp4", Kind: media.KindVideo},
		{ID: "gen-id", FileName: "data.bin", MIME: "application/octet-stream", Kind: media.KindFile},
	}

	tests := []struct {
		input     string
		wantEmoji string
	}{
		{"@file:img-id", "🎨"},
		{"@file:pdf-id", "📕"},
		{"@file:txt-id", "📝"},
		{"@file:aud-id", "🎵"},
		{"@file:vid-id", "🎬"},
		{"@file:gen-id", "📎"},
		{"@file:unknown-id", "❓"}, // unresolved → fallback
	}
	for _, tt := range tests {
		got := RenderReferences(tt.input, "236", registry)
		if !strings.Contains(got, tt.wantEmoji) {
			t.Errorf("RenderReferences(%q) = %q, missing emoji %q", tt.input, got, tt.wantEmoji)
		}
	}
}
