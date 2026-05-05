package ui

import (
	"strings"

	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/glamour/ansi"
	"github.com/charmbracelet/glamour/styles"
)

var mdRenderer *glamour.TermRenderer

func init() {
	var err error
	mdRenderer, err = glamour.NewTermRenderer(
		glamour.WithStyles(noIndentStyles()),
		glamour.WithWordWrap(0), // we control width ourselves
	)
	if err != nil {
		mdRenderer = nil
	}
}

// noIndentStyles returns the standard dark glamour style with all block
// indentation and document margin removed so markdown renders flush-left.
// Code block background is removed so it blends with the terminal.
func noIndentStyles() ansi.StyleConfig {
	cfg := styles.DarkStyleConfig
	cfg.Document.Indent = nil
	cfg.Document.Margin = nil
	cfg.Paragraph.Indent = nil
	cfg.Heading.Indent = nil
	cfg.BlockQuote.Indent = nil

	// Remove background from code blocks and inline code so they blend with the terminal
	cfg.CodeBlock.BackgroundColor = nil
	cfg.Code.BackgroundColor = nil

	// Change inline code color from red to orange
	orange := "209"
	cfg.Code.Color = &orange

	return cfg
}

// RenderMarkdown renders markdown text to styled terminal output.
// Falls back to plain text if renderer is unavailable.
// If width > 0, it reuses a renderer with that word-wrap width.
func RenderMarkdown(text string, width int) string {
	if text == "" {
		return text
	}

	// Use the default renderer for width=0
	if width == 0 {
		if mdRenderer == nil {
			return text
		}
		rendered, err := mdRenderer.Render(text)
		if err != nil {
			return text
		}
		return strings.TrimRight(rendered, "\n")
	}

	// For a specific width, create a one-off renderer with word wrapping
	r, err := glamour.NewTermRenderer(
		glamour.WithStyles(noIndentStyles()),
		glamour.WithWordWrap(width),
	)
	if err != nil {
		return text
	}
	rendered, err := r.Render(text)
	if err != nil {
		return text
	}
	return strings.TrimRight(rendered, "\n")
}

// RenderMarkdownOnBg renders markdown and, after every ANSI reset sequence,
// immediately restores the given 256-colour background.  This prevents glamour's
// reset codes from "punching holes" in the lipgloss block that wraps the output.
// bg256 is the xterm-256 colour number as a string (e.g. "233").
// wrapWidth is the content width to wrap at (0 = no wrap).
// Falls back to plain text if the renderer is unavailable.
func RenderMarkdownOnBg(text, bg256 string, wrapWidth int) string {
	rendered := RenderMarkdown(text, wrapWidth)
	if rendered == text {
		return text // renderer unavailable or empty — no post-processing needed
	}
	restoreSeq := "\x1b[48;5;" + bg256 + "m"
	return strings.ReplaceAll(rendered, "\x1b[0m", "\x1b[0m"+restoreSeq)
}
