package gnu

import (
	"fmt"
	"io"
)

const (
	ansiReset  = "\x1b[0m"
	ansiBold   = "\x1b[1m"
	ansiDim    = "\x1b[2m"
	ansiBlue   = "\x1b[34m"
	ansiGreen  = "\x1b[32m"
	ansiYellow = "\x1b[33m"
	ansiOrange = "\x1b[38;5;208m"
)

type Presenter struct {
	Writer io.Writer
	Color  bool
}

func (p Presenter) Waiting(model string) {
	fmt.Fprintln(p.Writer)
	if model == "" {
		p.line("  Asking Squid-OS…", ansiYellow)
		return
	}
	p.line(fmt.Sprintf("  Asking Squid-OS  %s%s%s", p.code(ansiDim), model, p.code(ansiReset)), ansiYellow)
}

func (p Presenter) Suggestion(suggestion Suggestion) {
	fmt.Fprintln(p.Writer)
	if suggestion.InstallHint != "" {
		fmt.Fprintf(p.Writer, "  %sinstall%s  %s\n", p.code(ansiBlue), p.code(ansiReset), suggestion.InstallHint)
	}
	fmt.Fprintf(p.Writer, "  %s%s%s\n\n", p.code(ansiOrange+ansiBold), suggestion.Command, p.code(ansiReset))
}

func (p Presenter) Aborted()   { p.line("  Aborted.", ansiYellow) }
func (p Presenter) Executing() { p.line("  Executing…", ansiGreen) }

func (p Presenter) line(text, color string) {
	fmt.Fprintf(p.Writer, "%s%s%s\n", p.code(color), text, p.code(ansiReset))
}
func (p Presenter) code(value string) string {
	if p.Color {
		return value
	}
	return ""
}
