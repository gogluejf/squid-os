package ui

import "github.com/charmbracelet/lipgloss"

// -------------------------------------------------------
// Palette — change these constants to reskin the whole UI
// -------------------------------------------------------

type Palette struct {
	// Backgrounds
	BgApp       string // main app / assistant message bg
	BgUser      string // user message bg
	BgFooter    string // footer / top header bg
	BgCode      string // code block bg
	BgIncognito string // incognito mode bg
	BgSelected  string // picker/command selected row bg

	// Foreground / Text
	TextPrimary    string // main text (white)
	TextSecondary  string // secondary text (light gray)
	TextDim        string // dim text (headers, labels)
	TextMuted      string // very dim (timestamps, separators)
	TextHeading    string // markdown headings
	TextAccent     string // links, keys, bullets (cyan)
	TextToolParam  string // tool display param value (lighter blue)
	TextCode       string // inline code / code block text
	TextSuccess    string // success indicators (green)
	TextError      string // error indicators (red)
	TextWarning    string // warning indicators (yellow/orange)
	TextInfo       string // info/notice (muted)
	TextSpinner    string // spinner / active indicator (pink)
	TextAttachment string // image attachment chip (orange)

	// Context bar
	CtxBarUsed  string // context bar: used portion bg (darker)
	CtxBarEmpty string // context bar: remaining portion bg (lighter)
}

// Current palette (defaults to the existing color scheme)
var P = Palette{
	BgApp:       "233",
	BgUser:      "236",
	BgFooter:    "235",
	BgCode:      "234",
	BgIncognito: "54",
	BgSelected:  "237",

	TextPrimary:    "252",
	TextSecondary:  "245",
	TextDim:        "240",
	TextMuted:      "243",
	TextHeading:    "255",
	TextAccent:     "110", // cyan
	TextToolParam:  "67",  // dark gray-blue for tool param display
	TextCode:       "228", // yellow
	TextSuccess:    "22",  // dark green
	TextError:      "124", // red
	TextWarning:    "214", // orange/yellow
	TextInfo:       "243",
	TextSpinner:    "205", // pink
	TextAttachment: "214", // orange

	CtxBarUsed:  "237",
	CtxBarEmpty: "233",
}

// -------------------------------------------------------
// Derived styles — each uses palette constants
// -------------------------------------------------------

// BoxMargin is the side gutter (cols) around UserBox and ToolBox.
const BoxMargin = 2

// BoxWidth computes the outer box width for a given viewport width.
// The box is inset by BoxMargin on each side, with a minimum of 20 cols.
func BoxWidth(viewportWidth int) int {
	w := viewportWidth - 2*BoxMargin
	if w < 20 {
		w = 20
	}
	return w
}

// ContentWidth computes the usable inner width for content inside a box
// (box width minus left+right padding of 2 each). Minimum 10 cols.
// Used for word-wrapping text/markdown inside any box.
func ContentWidth(viewportWidth int) int {
	w := BoxWidth(viewportWidth) - 4 // 2 left + 2 right padding
	if w < 10 {
		w = 10
	}
	return w
}

// CanvasContentWidth computes the usable inner width for content inside a
// canvas-span box (full-viewport width minus margins and padding).
// Canvas spans use the full viewport width as their box width.
func CanvasContentWidth(viewportWidth int) int {
	w := viewportWidth - 2*BoxMargin - 4
	if w < 10 {
		w = 10
	}
	return w
}

var (
	// CanvasSpan — full viewport width, BgApp. Bare canvas paint for headers.
	// Callers prepend "\n" for vertical spacing.
	CanvasSpan = lipgloss.NewStyle().
			Background(lipgloss.Color(P.BgApp)).
			Foreground(lipgloss.Color(P.TextPrimary)).
			Padding(0, 2).
			Margin(0, BoxMargin, 1, BoxMargin).
			MarginBackground(lipgloss.Color(P.BgApp))

	// Stat chip on the canvas (e.g. "· ↓1.2k 250ms" after thinking/waiting)
	CanvasStatInline = lipgloss.NewStyle().
				Background(lipgloss.Color(P.BgApp)).
				Foreground(lipgloss.Color(P.TextDim))

	ThinkingLabel = lipgloss.NewStyle().
			Background(lipgloss.Color(P.BgApp)).
			Foreground(lipgloss.Color(P.TextMuted))

	// Internal label (e.g. "aborted") on canvas background
	InternalLabel = lipgloss.NewStyle().
			Background(lipgloss.Color(P.BgApp)).
			Foreground(lipgloss.Color(P.TextWarning))

	// System prompt label (color 141 - green) on canvas background
	SystemLabel = lipgloss.NewStyle().
			Background(lipgloss.Color(P.BgApp)).
			Foreground(lipgloss.Color("141"))

	// Internal message label (color 39 - teal) on canvas background
	InternalMsgLabel = lipgloss.NewStyle().
			Background(lipgloss.Color(P.BgApp)).
			Foreground(lipgloss.Color("39"))

	// Tool box inline sub-styles (BgCode — match the ToolBox background)
	ToolLabel = lipgloss.NewStyle().
			Background(lipgloss.Color(P.BgCode)).
			Foreground(lipgloss.Color(P.TextAccent))

	ToolParamOnTool = lipgloss.NewStyle().
			Background(lipgloss.Color(P.BgCode)).
			Foreground(lipgloss.Color(P.TextToolParam))

	ToolCheckOnTool = lipgloss.NewStyle().
			Background(lipgloss.Color(P.BgCode)).
			Foreground(lipgloss.Color(P.TextSuccess))

	ToolErrOnTool = lipgloss.NewStyle().
			Background(lipgloss.Color(P.BgCode)).
			Foreground(lipgloss.Color(P.TextError))

	ToolStatOnTool = lipgloss.NewStyle().
			Background(lipgloss.Color(P.BgCode)).
			Foreground(lipgloss.Color(P.TextDim))

	ToolResultOnTool = lipgloss.NewStyle().
				Background(lipgloss.Color(P.BgCode)).
				Foreground(lipgloss.Color(P.TextDim))

	ToolErrorOnTool = lipgloss.NewStyle().
			Background(lipgloss.Color(P.BgCode)).
			Foreground(lipgloss.Color(P.TextError))

	// Message header inline styles (painted on the parent box's bg)
	UserHeaderDimStyle = lipgloss.NewStyle().
				Background(lipgloss.Color(P.BgUser)).
				Foreground(lipgloss.Color(P.TextSecondary))

	UserHeaderAttStyle = lipgloss.NewStyle().
				Background(lipgloss.Color(P.BgUser)).
				Foreground(lipgloss.Color(P.TextAttachment))

	AssistantHeaderDimStyle = lipgloss.NewStyle().
				Background(lipgloss.Color(P.BgApp)).
				Foreground(lipgloss.Color(P.TextSecondary))

	// Top header
	TopHeaderStyle = lipgloss.NewStyle().
			Background(lipgloss.Color(P.BgFooter)).
			Foreground(lipgloss.Color(P.TextSecondary)).
			Bold(true).
			Padding(0, 1)

	// Footer bar
	FooterStyle = lipgloss.NewStyle().
			Background(lipgloss.Color(P.BgFooter)).
			Foreground(lipgloss.Color(P.TextSecondary)).
			Padding(0, 1)

	FooterKeyStyle = lipgloss.NewStyle().
			Background(lipgloss.Color(P.BgFooter)).
			Foreground(lipgloss.Color(P.TextAccent)).
			Bold(true)

	FooterDimStyle = lipgloss.NewStyle().
			Background(lipgloss.Color(P.BgFooter)).
			Foreground(lipgloss.Color(P.TextDim))

	FooterValueStyle = lipgloss.NewStyle().
				Background(lipgloss.Color(P.BgFooter)).
				Foreground(lipgloss.Color(P.TextPrimary))

	// Markdown elements
	HeadingStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color(P.TextHeading)).
			Bold(true)

	BulletStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color(P.TextAccent))

	// Error — carries BgApp so ANSI resets don't punch holes
	ErrorStyle = lipgloss.NewStyle().
			Background(lipgloss.Color(P.BgApp)).
			Foreground(lipgloss.Color(P.TextError)).
			Bold(true)

	// Warning — carries BgApp
	WarningStyle = lipgloss.NewStyle().
			Background(lipgloss.Color(P.BgApp)).
			Foreground(lipgloss.Color(P.TextWarning)).
			Bold(true)

	// Info — carries BgApp (so blue text stays on app bg, not terminal bg)
	InfoStyle = lipgloss.NewStyle().
			Background(lipgloss.Color(P.BgApp)).
			Foreground(lipgloss.Color(P.TextInfo))

	// TextMutedStyle — for rendering muted content inside canvas spans (system/internal)
	TextMutedStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color(P.TextMuted))

	// Image attachment chip — carries BgApp
	AttachmentStyle = lipgloss.NewStyle().
			Background(lipgloss.Color(P.BgApp)).
			Foreground(lipgloss.Color(P.TextAttachment)).
			Padding(0, 1)

	// Status line bg — wraps the full row so styled segments don't punch holes
	StatusLineStyle = lipgloss.NewStyle().
			Background(lipgloss.Color(P.BgApp))

	// Incognito indicator
	IncognitoStyle = lipgloss.NewStyle().
			Background(lipgloss.Color(P.BgIncognito)).
			Foreground(lipgloss.Color(P.TextPrimary)).
			Bold(true)

	IncognitoHeaderStyle = lipgloss.NewStyle().
				Background(lipgloss.Color(P.BgIncognito)).
				Foreground(lipgloss.Color(P.TextPrimary)).
				Bold(true).
				Padding(0, 1)

	// Command palette
	CommandStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color(P.TextAccent)).
			Bold(true)

	CommandDescStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color(P.TextMuted))

	CommandSelectedStyle = lipgloss.NewStyle().
				Background(lipgloss.Color(P.BgSelected)).
				Foreground(lipgloss.Color(P.TextAccent)).
				Bold(true)
)
