package app

import (
	"time"

	"squid-os/internal/chat"
	"squid-os/internal/ui"
	"squid-os/internal/util"
)

// UIStreamState bundles app-only transient fields for an active inference stream.
type UIStreamState struct {
	ID               string
	Markdown         string
	MarkdownEnd      int
	Ch               <-chan chat.StreamEvent
	TokenCount       int
	Stopwatch        util.Stopwatch
	AuthorizationCtx *AuthorizationContext
	MsgIdx           int
}

func (ss *UIStreamState) reset() {
	*ss = UIStreamState{MarkdownEnd: -1, MsgIdx: -1}
}

// streamingToolCalls converts pure partial tool state into display-ready values while streaming.
func streamingToolCalls(partials []chat.PartialTool) []ui.StreamingToolCall {
	var out []ui.StreamingToolCall
	for i, p := range partials {
		if p.Name == "" {
			continue
		}
		dur := time.Duration(0)
		if !p.FirstAt.IsZero() {
			end := p.DoneAt
			if end.IsZero() || i == len(partials)-1 {
				end = time.Now()
			}
			dur = end.Sub(p.FirstAt)
		}
		out = append(out, ui.StreamingToolCall{
			Name:      p.Name,
			Arguments: p.Args,
			Tokens:    chat.CountTokensApproxInt(p.Chars),
			Duration:  dur,
		})
	}
	return out
}
