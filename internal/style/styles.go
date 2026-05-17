package style

import (
	"sync"

	"github.com/charmbracelet/lipgloss"
)

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
	TextPrimary       string // main text (white)
	TextSecondary     string // secondary text (light gray)
	TextDim           string // dim text (headers, labels)
	TextMuted         string // very dim (timestamps, separators)
	TextHeading       string // markdown headings
	TextAccent        string // links, keys, bullets (cyan)
	TextToolParam     string // tool display param value (lighter blue)
	TextSystemLabel   string // system message label (green)
	TextSystemParam   string // system message param value (darker green)
	TextInternalLabel string // internal message label (teal)
	TextInternalParam string // internal message param value (darker teal)
	TextCode          string // inline code / code block text
	TextSuccess       string // success indicators (green)
	TextError         string // error indicators (red)
	TextWarning       string // warning indicators (yellow/orange)
	TextInfo          string // info/notice (muted)
	TextSpinner       string // spinner / active indicator (pink)
	TextAttachment    string // image attachment chip (orange)

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

	TextPrimary:       "252",
	TextSecondary:     "245",
	TextDim:           "240",
	TextMuted:         "243",
	TextHeading:       "255",
	TextAccent:        "110", // cyan
	TextToolParam:     "67",  // dark gray-blue for tool param display
	TextSystemLabel:   "141", // system message label (green)
	TextSystemParam:   "140", // darker green than label
	TextInternalLabel: "39",  // internal message label (teal)
	TextInternalParam: "24",  // darker teal than label
	TextCode:          "228", // yellow
	TextSuccess:       "22",  // dark green
	TextError:         "124", // red
	TextWarning:       "214", // orange/yellow
	TextInfo:          "243",
	TextSpinner:       "205", // pink
	TextAttachment:    "214", // orange

	CtxBarUsed:  "255",
	CtxBarEmpty: "237",
}

// -------------------------------------------------------
// Width helpers
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

// -------------------------------------------------------
// Non-label styles — used directly (not inlined into StyleLabel)
// -------------------------------------------------------

var (
	// CanvasSpan — full viewport width, BgApp. Bare canvas paint for headers.
	// Callers prepend "\n" for vertical spacing.
	CanvasSpan = lipgloss.NewStyle().
			Background(lipgloss.Color(P.BgApp)).
			Foreground(lipgloss.Color(P.TextPrimary)).
			Padding(0, 2).
			Margin(0, BoxMargin, 1, BoxMargin).
			MarginBackground(lipgloss.Color(P.BgApp))

	UserHeaderAttStyle = lipgloss.NewStyle().
				Background(lipgloss.Color(P.BgUser)).
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

	// Generic status indicators — used on tool boxes for success/error badges
	CheckSuccess = lipgloss.NewStyle().
			Background(lipgloss.Color(P.BgCode)).
			Foreground(lipgloss.Color(P.TextSuccess))

	CheckError = lipgloss.NewStyle().
			Background(lipgloss.Color(P.BgCode)).
			Foreground(lipgloss.Color(P.TextError))

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

// -------------------------------------------------------
// StyleLabel — pre-built styles for message boxes
// -------------------------------------------------------

// StyleLabel holds pre-built lipgloss styles for a single message box type.
// Label/Param/Dim are used for the header line parts.
// Content/Error are used for expanded body text.
// Bg/Fg define the box background and default foreground for DrawCanvas.
type StyleLabel struct {
	Label   lipgloss.Style
	Param   lipgloss.Style
	Dim     lipgloss.Style
	Content lipgloss.Style
	Error   lipgloss.Style
	Bg      string
	Fg      string
}

// cachedBuilder lazily builds and caches a StyleLabel after the first call.
type cachedBuilder struct {
	once   sync.Once
	result StyleLabel
}

// build constructs the label and caches it.
func (c *cachedBuilder) build(fn func() StyleLabel) {
	c.once.Do(func() { c.result = fn() })
}

// Get returns the cached label, building on first access.
func (c *cachedBuilder) Get(fn func() StyleLabel) StyleLabel {
	c.build(fn)
	return c.result
}

// -------------------------------------------------------
// Canvas labels (BgApp background)
// -------------------------------------------------------

var _systemLabel cachedBuilder

// SystemStyleLabel returns the style for system prompt messages (green label).
func SystemStyleLabel() StyleLabel {
	return _systemLabel.Get(func() StyleLabel {
		bg := lipgloss.Color(P.BgApp)
		return StyleLabel{
			Label:   lipgloss.NewStyle().Background(bg).Foreground(lipgloss.Color(P.TextSystemLabel)),
			Param:   lipgloss.NewStyle().Background(bg).Foreground(lipgloss.Color(P.TextSystemParam)),
			Dim:     lipgloss.NewStyle().Background(bg).Foreground(lipgloss.Color(P.TextDim)),
			Content: lipgloss.NewStyle().Background(bg).Foreground(lipgloss.Color(P.TextMuted)),
			Error:   lipgloss.NewStyle().Background(bg).Foreground(lipgloss.Color(P.TextError)),
			Bg:      P.BgApp,
			Fg:      P.TextMuted,
		}
	})
}

var _internalLabel cachedBuilder

// InternalStyleLabel returns the style for internal metadata messages (teal label).
func InternalStyleLabel() StyleLabel {
	return _internalLabel.Get(func() StyleLabel {
		bg := lipgloss.Color(P.BgApp)
		return StyleLabel{
			Label:   lipgloss.NewStyle().Background(bg).Foreground(lipgloss.Color(P.TextInternalLabel)),
			Param:   lipgloss.NewStyle().Background(bg).Foreground(lipgloss.Color(P.TextInternalParam)),
			Dim:     lipgloss.NewStyle().Background(bg).Foreground(lipgloss.Color(P.TextDim)),
			Content: lipgloss.NewStyle().Background(bg).Foreground(lipgloss.Color(P.TextMuted)),
			Error:   lipgloss.NewStyle().Background(bg).Foreground(lipgloss.Color(P.TextError)),
			Bg:      P.BgApp,
			Fg:      P.TextMuted,
		}
	})
}

var _syntheticLabel cachedBuilder

// SyntheticStyleLabel returns the style for synthetic messages (warning label, e.g. "aborted").
func SyntheticStyleLabel() StyleLabel {
	return _syntheticLabel.Get(func() StyleLabel {
		bg := lipgloss.Color(P.BgApp)
		return StyleLabel{
			Label:   lipgloss.NewStyle().Background(bg).Foreground(lipgloss.Color(P.TextWarning)),
			Param:   lipgloss.NewStyle().Background(bg).Foreground(lipgloss.Color(P.TextInternalParam)),
			Dim:     lipgloss.NewStyle().Background(bg).Foreground(lipgloss.Color(P.TextDim)),
			Content: lipgloss.NewStyle().Background(bg).Foreground(lipgloss.Color(P.TextMuted)),
			Error:   lipgloss.NewStyle().Background(bg).Foreground(lipgloss.Color(P.TextError)),
			Bg:      P.BgApp,
			Fg:      P.TextMuted,
		}
	})
}

var _thinkingLabel cachedBuilder

// ThinkingStyleLabel returns the style for thinking blocks (muted label).
func ThinkingStyleLabel() StyleLabel {
	return _thinkingLabel.Get(func() StyleLabel {
		bg := lipgloss.Color(P.BgApp)
		return StyleLabel{
			Label:   lipgloss.NewStyle().Background(bg).Foreground(lipgloss.Color(P.TextMuted)),
			Dim:     lipgloss.NewStyle().Background(bg).Foreground(lipgloss.Color(P.TextDim)),
			Content: lipgloss.NewStyle().Background(bg).Foreground(lipgloss.Color(P.TextMuted)),
			Error:   lipgloss.NewStyle().Background(bg).Foreground(lipgloss.Color(P.TextError)),
			Bg:      P.BgApp,
			Fg:      P.TextMuted,
		}
	})
}

// -------------------------------------------------------
// User / assistant labels
// -------------------------------------------------------

var _userLabel cachedBuilder

// UserStyleLabel returns the style for user message boxes.
func UserStyleLabel() StyleLabel {
	return _userLabel.Get(func() StyleLabel {
		bg := lipgloss.Color(P.BgUser)
		return StyleLabel{
			Label:   lipgloss.NewStyle().Background(bg).Foreground(lipgloss.Color(P.TextSecondary)),
			Param:   lipgloss.NewStyle().Background(bg).Foreground(lipgloss.Color(P.TextSecondary)),
			Dim:     lipgloss.NewStyle().Background(bg).Foreground(lipgloss.Color(P.TextSecondary)),
			Content: lipgloss.NewStyle().Background(bg).Foreground(lipgloss.Color(P.TextPrimary)),
			Error:   lipgloss.NewStyle().Background(bg).Foreground(lipgloss.Color(P.TextError)),
			Bg:      P.BgUser,
			Fg:      P.TextPrimary,
		}
	})
}

var _assistantLabel cachedBuilder

// AssistantStyleLabel returns the style for assistant body blocks.
func AssistantStyleLabel() StyleLabel {
	return _assistantLabel.Get(func() StyleLabel {
		bg := lipgloss.Color(P.BgApp)
		return StyleLabel{
			Label:   lipgloss.NewStyle().Background(bg).Foreground(lipgloss.Color(P.TextPrimary)),
			Param:   lipgloss.NewStyle().Background(bg).Foreground(lipgloss.Color(P.TextPrimary)),
			Dim:     lipgloss.NewStyle().Background(bg).Foreground(lipgloss.Color(P.TextSecondary)),
			Content: lipgloss.NewStyle().Background(bg).Foreground(lipgloss.Color(P.TextPrimary)),
			Error:   lipgloss.NewStyle().Background(bg).Foreground(lipgloss.Color(P.TextError)),
			Bg:      P.BgApp,
			Fg:      P.TextPrimary,
		}
	})
}

// -------------------------------------------------------
// Tool / skill labels (BgCode background)
// -------------------------------------------------------

var _toolLabel cachedBuilder

// ToolStyle returns the style for core tools (cyan label, dark gray-blue param).
func ToolStyle() StyleLabel {
	return _toolLabel.Get(func() StyleLabel {
		bg := lipgloss.Color(P.BgCode)
		return StyleLabel{
			Label:   lipgloss.NewStyle().Background(bg).Foreground(lipgloss.Color(P.TextAccent)),
			Param:   lipgloss.NewStyle().Background(bg).Foreground(lipgloss.Color(P.TextToolParam)),
			Dim:     lipgloss.NewStyle().Background(bg).Foreground(lipgloss.Color(P.TextDim)),
			Content: lipgloss.NewStyle().Background(bg).Foreground(lipgloss.Color(P.TextDim)),
			Error:   lipgloss.NewStyle().Background(bg).Foreground(lipgloss.Color(P.TextError)),
			Bg:      P.BgCode,
			Fg:      P.TextDim,
		}
	})
}

var _skillLabel cachedBuilder

// SkillStyle returns the style for skill tools (yellow label, darker yellow param).
func SkillStyle() StyleLabel {
	return _skillLabel.Get(func() StyleLabel {
		bg := lipgloss.Color(P.BgCode)
		return StyleLabel{
			Label:   lipgloss.NewStyle().Background(bg).Foreground(lipgloss.Color("178")),
			Param:   lipgloss.NewStyle().Background(bg).Foreground(lipgloss.Color("180")),
			Dim:     lipgloss.NewStyle().Background(bg).Foreground(lipgloss.Color(P.TextDim)),
			Content: lipgloss.NewStyle().Background(bg).Foreground(lipgloss.Color(P.TextDim)),
			Error:   lipgloss.NewStyle().Background(bg).Foreground(lipgloss.Color(P.TextError)),
			Bg:      P.BgCode,
			Fg:      P.TextDim,
		}
	})
}
