package cli

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"squid-os/internal/localinput"
)

// ── voice CLI ───────────────────────────────────────────────────────────────
//
// squid-os voice <stop|on|off> talks to a live TUI instance over its private
// Unix socket (same protocol as input). 'stop' triggers the same CancelGroup
// path as alt+x: the whole active speech group is silenced in one frame.
// on/off are scaffolded for future runtime toggling.

type voiceOptions struct {
	Instance string
	Session  string
	JSON     bool
}

type voiceCmd struct{}

var _ Command[voiceOptions] = voiceCmd{}

func (voiceCmd) Spec() CommandSpec {
	return CommandSpec{Use: "voice", Short: "Control live TUI voice output", Runnable: false}
}

func (voiceCmd) Flags(f *pflag.FlagSet) *voiceOptions {
	o := &voiceOptions{}
	f.StringVar(&o.Instance, "instance", "", "target instance ID (default: auto-discover)")
	f.StringVar(&o.Session, "session", "", "target session ID (default: the instance's live session)")
	f.BoolVar(&o.JSON, "json", true, "emit JSON result")
	return o
}

func (voiceCmd) Prepare(c *cobra.Command, o *voiceOptions, args []string) error { return nil }
func (voiceCmd) Run(o *voiceOptions, io CommandIO) error                        { return nil }
func (voiceCmd) Completions() []Completion                                      { return nil }

type voiceSubCmd struct {
	name    string
	short   string
	execute func(*voiceOptions, CommandIO) error
}

func (c voiceSubCmd) Spec() CommandSpec {
	return CommandSpec{Use: c.name, Short: c.short, Args: cobra.NoArgs, Runnable: true}
}
func (c voiceSubCmd) Flags(f *pflag.FlagSet) *voiceOptions { return voiceCmd{}.Flags(f) }
func (c voiceSubCmd) Prepare(cmd *cobra.Command, o *voiceOptions, args []string) error {
	return nil
}
func (c voiceSubCmd) Run(o *voiceOptions, io CommandIO) error { return c.execute(o, io) }
func (c voiceSubCmd) Completions() []Completion               { return nil }

// registerVoiceCommands attaches the voice command tree to root.
func registerVoiceCommands(root *cobra.Command) {
	parent := makeCobra(voiceCmd{})
	root.AddCommand(parent)
	parent.AddCommand(
		makeCobra(voiceSubCmd{name: "stop", short: "Silence the current speech group (same as alt+x)", execute: voiceStop}),
		makeCobra(voiceSubCmd{name: "on", short: "Enable voice output (scaffold)", execute: voiceScaffold}),
		makeCobra(voiceSubCmd{name: "off", short: "Disable voice output (scaffold)", execute: voiceScaffold}),
	)
}

// ── subcommands ─────────────────────────────────────────────────────────────

// voiceStop sends the voice_stop op: the app-side handler runs the identical
// CancelGroup path as alt+x. Reply status is "cancelled" when an active group
// was killed or "nothing_playing" for the explicit no-op; both exit 0. A
// rejected reply (receiver_not_ready from a wedged UI loop) exits non-zero.
func voiceStop(o *voiceOptions, io CommandIO) error {
	path, err := discoverInstance(o.Instance)
	if err != nil {
		return err
	}
	r, err := roundTrip(path, localinput.Request{Version: localinput.ProtocolVersion, Op: "voice_stop", SessionID: o.Session})
	if err != nil {
		return err
	}
	printReply(io.Out, r)
	switch r.Status {
	case "cancelled", "nothing_playing":
		return nil
	default:
		return exitErr(exitCodeFor(r))
	}
}

// voiceScaffold is the placeholder for the future on/off runtime toggle ops.
func voiceScaffold(o *voiceOptions, io CommandIO) error {
	return fmt.Errorf("not implemented yet: use /voice inside the TUI")
}
