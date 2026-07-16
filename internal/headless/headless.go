package headless

import (
	"context"
	"fmt"
	"os"
	"os/signal"

	"squid-os/internal/chat"
	"squid-os/internal/config"
	"squid-os/internal/tools"
)

// Run executes a single prompt and streams the response to stdout.
func Run(paths config.Paths, settings config.Settings, endpoints config.EndpointsConfig, prompt, imagePath string) error {
	if settings.Model == "" {
		return fmt.Errorf("no model configured. Run squid-os and use /model to select one, or set it in settings.json")
	}

	s := chat.NewSession(chat.SessionConfig{
		Provider:         settings.Provider,
		Model:            settings.Model,
		Thinking:         settings.Thinking,
		SystemPromptFile: settings.SystemPromptFile,
		Tools:            nil,
		Skills:           nil,
		WorkingDir:       "",
		DebugEnabled:     settings.DebugEnabled,
	}, paths)
	s.Append(chat.NewUserMessage(fmt.Sprintf("msg_%d", len(s.Doc.Messages)+1), prompt, imagePath))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt)
	defer signal.Stop(sig)
	go func() {
		<-sig
		s.Stream.Cancel("request cancelled")
	}()

	for event := range chat.RunLoop(ctx, s, endpoints, tools.GetRegistry()) {
		switch event.Type {
		case chat.LoopEventText:
			fmt.Print(event.Text)
		case chat.LoopEventError:
			if event.IsAuthError {
				_ = config.PersistProviderAuthState(paths, endpoints, s.CurrentInference().Provider)
			} else {
				_ = config.PersistRefreshedProvider(paths, endpoints, s.CurrentInference().Provider)
			}
			if event.Error != nil {
				return event.Error
			}
		case chat.LoopEventNeedAuth:
			_ = config.PersistRefreshedProvider(paths, endpoints, s.CurrentInference().Provider)
			return fmt.Errorf("tool authorization required for %s", event.AuthRequest.ToolName)
		case chat.LoopEventDone:
			_ = config.PersistRefreshedProvider(paths, endpoints, s.CurrentInference().Provider)
		}
	}

	fmt.Println()
	return nil
}
