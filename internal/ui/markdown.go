package ui

import (
	"strings"

	"github.com/charmbracelet/glamour"
)

var mdRenderer *glamour.TermRenderer

func init() {
	var err error
	mdRenderer, err = glamour.NewTermRenderer(
		glamour.WithAutoStyle(),
		glamour.WithWordWrap(0), // we control width ourselves
	)
	if err != nil {
		mdRenderer = nil
	}
}

// RenderMarkdown renders markdown text to styled terminal output.
// Falls back to plain text if renderer is unavailable.
func RenderMarkdown(text string, width int) string {
	if mdRenderer == nil || text == "" {
		return text
	}

	rendered, err := mdRenderer.Render(text)
	if err != nil {
		return text
	}

	// glamour adds trailing newlines, trim them
	return strings.TrimRight(rendered, "\n")
}

// RenderMarkdownOnBg renders markdown and, after every ANSI reset sequence,
// immediately restores the given 256-colour background.  This prevents glamour's
// reset codes from "punching holes" in the lipgloss block that wraps the output.
// bg256 is the xterm-256 colour number as a string (e.g. "233").
// Falls back to plain text if the renderer is unavailable.
func RenderMarkdownOnBg(text, bg256 string) string {
	rendered := RenderMarkdown(text, 0)
	if rendered == text {
		return text // renderer unavailable or empty — no post-processing needed
	}
	restoreSeq := "\x1b[48;5;" + bg256 + "m"
	return strings.ReplaceAll(rendered, "\x1b[0m", "\x1b[0m"+restoreSeq)
}
