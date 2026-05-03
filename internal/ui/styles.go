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
	TextError:      "196", // red
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

var (
	// Message backgrounds
	UserMsgStyle = lipgloss.NewStyle().
			Background(lipgloss.Color(P.BgUser)).
			Foreground(lipgloss.Color(P.TextPrimary)).
			Padding(0, 1)

	AssistantMsgStyle = lipgloss.NewStyle().
				Background(lipgloss.Color(P.BgApp)).
				Foreground(lipgloss.Color(P.TextPrimary)).
				Padding(0, 1)

	// Thinking block
	ThinkingStyle = lipgloss.NewStyle().
			Background(lipgloss.Color(P.BgApp)).
			Foreground(lipgloss.Color(P.TextMuted))

	// Message headers
	MsgHeaderStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color(P.TextDim))

	UserHeaderStyle = lipgloss.NewStyle().
			Background(lipgloss.Color(P.BgUser)).
			Foreground(lipgloss.Color(P.TextSecondary)).
			Padding(0, 1)

	UserHeaderDimStyle = lipgloss.NewStyle().
				Background(lipgloss.Color(P.BgUser)).
				Foreground(lipgloss.Color(P.TextSecondary))

	UserHeaderAttStyle = lipgloss.NewStyle().
				Background(lipgloss.Color(P.BgUser)).
				Foreground(lipgloss.Color(P.TextAttachment))

	AssistantHeaderStyle = lipgloss.NewStyle().
				Background(lipgloss.Color(P.BgApp)).
				Foreground(lipgloss.Color(P.TextSecondary)).
				Padding(0, 1)

	AssistantHeaderDimStyle = lipgloss.NewStyle().
				Background(lipgloss.Color(P.BgApp)).
				Foreground(lipgloss.Color(P.TextSecondary))

	AssistantHeaderAttStyle = lipgloss.NewStyle().
				Background(lipgloss.Color(P.BgApp)).
				Foreground(lipgloss.Color(P.TextAttachment))

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

	// Code blocks
	CodeBlockStyle = lipgloss.NewStyle().
			Background(lipgloss.Color(P.BgCode)).
			Foreground(lipgloss.Color(P.TextCode)).
			Padding(0, 1)

	// Markdown elements
	HeadingStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color(P.TextHeading)).
			Bold(true)

	BulletStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color(P.TextAccent))

	// Spinner / status
	SpinnerStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color(P.TextSpinner))

	// Error
	ErrorStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color(P.TextError)).
			Bold(true)

	// Warning
	WarningStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color(P.TextWarning)).
			Bold(true)

	// Info
	InfoStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color(P.TextInfo))

	// Image attachment chip
	AttachmentStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color(P.TextAttachment)).
			Padding(0, 1)

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
				Foreground(lipgloss.Color(P.TextSuccess)).
				Bold(true)

	// Tool call line
	ToolCallStyle = lipgloss.NewStyle().
			Background(lipgloss.Color(P.BgApp)).
			Foreground(lipgloss.Color(P.TextAccent)).
			Padding(0, 1)

	ToolCallInline = lipgloss.NewStyle().
			Background(lipgloss.Color(P.BgApp)).
			Foreground(lipgloss.Color(P.TextAccent))

	ToolParamInline = lipgloss.NewStyle().
			Background(lipgloss.Color(P.BgApp)).
			Foreground(lipgloss.Color(P.TextToolParam))

	ToolCheckInline = lipgloss.NewStyle().
			Background(lipgloss.Color(P.BgApp)).
			Foreground(lipgloss.Color(P.TextSuccess))

	ToolErrInline = lipgloss.NewStyle().
			Background(lipgloss.Color(P.BgApp)).
			Foreground(lipgloss.Color(P.TextError))

	ToolStatInline = lipgloss.NewStyle().
			Background(lipgloss.Color(P.BgApp)).
			Foreground(lipgloss.Color(P.TextDim))

	ToolCallResultStyle = lipgloss.NewStyle().
				Background(lipgloss.Color(P.BgApp)).
				Foreground(lipgloss.Color(P.TextDim)).
				Padding(0, 1)

	ToolCallErrorStyle = lipgloss.NewStyle().
				Background(lipgloss.Color(P.BgApp)).
				Foreground(lipgloss.Color(P.TextError)).
				Padding(0, 1)
)
