package headless

import (
	"context"
	"fmt"
	"os"
	"os/signal"

	"rig-chat/internal/chat"
	"rig-chat/internal/config"
)

// Run executes a single prompt and streams the response to stdout
func Run(paths config.Paths, settings config.Settings, endpoints config.EndpointsConfig, prompt, imagePath string) error {
	// Find the active provider
	chatURL := config.ResolveChatURL(endpoints, settings.Provider)

	if settings.Model == "" {
		return fmt.Errorf("no model configured. Run rig-chat and use /model to select one, or set it in settings.json")
	}

	engine := chat.NewEngine(chatURL, settings.Model, settings.Thinking)

	// Build messages
	var msgs []chat.ChatMessage

	sysPrompt := config.LoadSystemPrompt(paths, settings.SystemPromptFile)
	msgs = append(msgs, chat.ChatMessage{Role: "system", Content: sysPrompt})

	if imagePath != "" {
		parts, err := chat.BuildMultimodalContent(prompt, imagePath)
		if err != nil {
			return fmt.Errorf("image: %w", err)
		}
		msgs = append(msgs, chat.ChatMessage{Role: "user", Content: parts})
	} else {
		msgs = append(msgs, chat.ChatMessage{Role: "user", Content: prompt})
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Handle Ctrl+C
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt)
	go func() {
		<-sig
		cancel()
	}()

	parser := &chat.ThinkParser{}
	if settings.Thinking {
		parser.InThink = true
	}

	ch := engine.Stream(ctx, msgs)

	for event := range ch {
		if event.Error != nil {
			return event.Error
		}
		if event.Done {
			break
		}
		if event.Text != "" {
			fmt.Print(event.Text)
		}
		// In headless mode, thinking text is suppressed by default
	}

	fmt.Println()
	return nil
}
