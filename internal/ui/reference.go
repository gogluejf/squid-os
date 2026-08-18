package ui

import (
	"regexp"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"squid-os/internal/media"
	"squid-os/internal/style"
)

// ReferenceKind identifies the category of a canonical reference.
type ReferenceKind string

const (
	ReferenceFile  ReferenceKind = "file"
	ReferenceSkill ReferenceKind = "skill"
	ReferenceAgent ReferenceKind = "agent"
	ReferenceTool  ReferenceKind = "tool"
)

// emojiMap returns the emoji prefix for each reference kind.
func (k ReferenceKind) emoji() string {
	switch k {
	case ReferenceFile:
		return "📄"
	case ReferenceSkill:
		return "⚡"
	case ReferenceAgent:
		return "🧠"
	case ReferenceTool:
		return "🔧"
	default:
		return ""
	}
}

// fileKindEmoji returns a specific emoji for a file attachment based on its Kind.
// Falls back to 📄 when the ref does not resolve in the registry.
func fileKindEmoji(ref referenceMatch, attachments []media.Attachment) string {
	a, ok := media.ResolveRef(attachments, ref.name)
	if !ok {
		return "❓"
	}
	switch a.Kind {
	case media.KindImage:
		return "🎨"
	case media.KindPDF:
		return "📕"
	case media.KindText:
		return "📝"
	case media.KindAudio:
		return "🎵"
	case media.KindVideo:
		return "🎬"
	default:
		return "📎"
	}
}

// colorMap returns the foreground color name for each reference kind.
func (k ReferenceKind) color() string {
	switch k {
	case ReferenceFile:
		return style.P.TextAttachment     // orange
	case ReferenceSkill:
		return style.P.TextSkill          // yellow
	case ReferenceAgent:
		return style.P.TextAgent          // pink-orange
	case ReferenceTool:
		return style.P.TextAccent         // cyan
	default:
		return style.P.TextPrimary
	}
}

// referenceRe matches canonical references:
//
//	@file:/path/to/file
//	@skill/name
//	@agent/name
//	@tool/name
var referenceRe = regexp.MustCompile(`@((?:file|skill|agent|tool)):([^\s]+)`)

// referenceMatch holds a parsed canonical reference.
type referenceMatch struct {
	kind  ReferenceKind
	name  string
	whole string // the matched text including the @ prefix
}

// parseReferences scans text for canonical references and returns matches in
// order of appearance.
func parseReferences(text string) []referenceMatch {
	var matches []referenceMatch
	for _, m := range referenceRe.FindAllStringSubmatchIndex(text, -1) {
		whole := text[m[0]:m[1]]
		kindStr := text[m[2]:m[3]]
		name := text[m[4]:m[5]]
		matches = append(matches, referenceMatch{
			kind:  ReferenceKind(kindStr),
			name:  name,
			whole: whole,
		})
	}
	return matches
}

// renderChip renders a single reference as a styled chip: [emoji name]
// with a foreground color per kind. The background matches the surrounding
// message bg so the ANSI reset after Render() doesn't punch a transparent
// hole in the rest of the line.
func renderChip(ref referenceMatch, bgColor string, attachments []media.Attachment) string {
	chipStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(ref.kind.color())).
		Background(lipgloss.Color(bgColor))

	emoji := ref.kind.emoji()
	label := ref.name
	if ref.kind == ReferenceFile {
		emoji = fileKindEmoji(ref, attachments)
		// Use DisplayName when the ref resolves in the registry —
		// it's more human-readable (e.g. "pasted-image.png" vs
		// "paste-f62258918ecfcb2f.png"). Falls back to the raw
		// name (path or file name) when unresolved.
		if a, ok := media.ResolveRef(attachments, ref.name); ok && a.DisplayName != "" {
			label = a.DisplayName
		}
	}
	return chipStyle.Render("[" + emoji + label + "]")
}

// RenderReferences replaces all canonical references in text with styled chips.
// The text is split into runs of reference and non-reference segments;
// references get rendered as chips, everything else passes through unchanged.
// This ensures malformed or unknown references remain readable plain text.
//
// bgColor is retained for API compatibility but no longer affects chip rendering
// (chips use foreground-only styling now).
func RenderReferences(text string, bgColor string, attachments []media.Attachment) string {
	matches := parseReferences(text)
	if len(matches) == 0 {
		return text
	}

	// Per the lipgloss-style-concatenation-reset pattern: every segment in a
	// concatenated string must carry the same background, otherwise the ANSI
	// reset after each Render() punches a transparent hole in the rest of the line.
	bgStyle := lipgloss.NewStyle().Background(lipgloss.Color(bgColor))

	var b strings.Builder
	lastEnd := 0
	for _, m := range matches {
		idx := strings.Index(text[lastEnd:], m.whole)
		if idx < 0 {
			continue
		}
		start := lastEnd + idx
		end := start + len(m.whole)

		// Plain-text segment before the chip — styled with bg so it doesn't
		// reset to terminal default.
		if seg := text[lastEnd:start]; seg != "" {
			b.WriteString(bgStyle.Render(seg))
		}
		b.WriteString(renderChip(m, bgColor, attachments))
		lastEnd = end
	}
	if tail := text[lastEnd:]; tail != "" {
		b.WriteString(bgStyle.Render(tail))
	}
	return b.String()
}
