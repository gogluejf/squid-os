package run

import (
	"context"
	"fmt"
	"time"

	"squid-os/internal/chat"
	"squid-os/internal/config"
	runtimeconfig "squid-os/internal/runtime"
)

type Request struct {
	Session runtimeconfig.SessionRequest
	OnEvent func(chat.LoopEvent)
}

type Result struct {
	FinalText, SavedSessionName string
	Session                     *chat.Session
}

func checkpointSave(paths config.Paths, name string, doc *config.SessionDoc) error {
	if name == "" {
		return nil
	}
	return config.SaveSessionDoc(paths, name, *doc)
}

func Execute(ctx context.Context, request Request) (Result, error) {
	sessionRequest := request.Session
	cfg := sessionRequest.Config
	var session *chat.Session
	if sessionRequest.ExistingSession != nil {
		session = chat.LoadSession(*sessionRequest.ExistingSession, sessionRequest.SessionName)
		cfg = session.Doc.Config
	} else {
		cfg.Memory = config.SessionMemory{Namespace: string(cfg.Memory.Namespace), Path: cfg.Memory.Path, Instructions: cfg.Memory.Instructions}
		session = chat.NewSession(cfg, sessionRequest.Paths)
	}
	session.Append(chat.NewUserMessage(fmt.Sprintf("msg_%d", len(session.Doc.Messages)+1), sessionRequest.Prompt, ""))
	if err := chat.PrepareTurn(session); err != nil {
		return Result{}, err
	}
	if cfg.Limits.MaxTime != "" {
		if dur, err := time.ParseDuration(cfg.Limits.MaxTime); err == nil {
			var cancel context.CancelFunc
			ctx, cancel = context.WithTimeout(ctx, dur)
			defer cancel()
		}
	}
	final := ""
	finalPass := ""
	paths := sessionRequest.Paths
	for event := range chat.RunLoop(ctx, session, sessionRequest.Endpoints) {
		if request.OnEvent != nil {
			request.OnEvent(event)
		}
		if event.Type == chat.LoopEventText {
			finalPass += event.Text
		}
		if event.Type == chat.LoopEventDone {
			final = finalPass
			finalPass = ""
			if cfg.Autosave.Enabled {
				_ = checkpointSave(paths, cfg.Autosave.Name, &session.Doc)
			}
		}
		if event.Type == chat.LoopEventToolFlushed {
			finalPass = ""
			if cfg.Autosave.Enabled {
				_ = checkpointSave(paths, cfg.Autosave.Name, &session.Doc)
			}
		}
		if event.Type == chat.LoopEventError {
			if cfg.Autosave.Enabled {
				_ = checkpointSave(paths, cfg.Autosave.Name, &session.Doc)
			}
			if event.Error != nil {
				return Result{FinalText: final, Session: session}, event.Error
			}
			return Result{FinalText: final, Session: session}, fmt.Errorf("run failed")
		}
		if event.Type == chat.LoopEventNeedAuth {
			if cfg.Autosave.Enabled {
				_ = checkpointSave(paths, cfg.Autosave.Name, &session.Doc)
			}
			return Result{FinalText: final, Session: session}, fmt.Errorf("tool authorization required for %s", event.AuthRequest.ToolName)
		}
	}
	// If no turn completed (e.g., error before done), use whatever was accumulated
	if final == "" {
		final = finalPass
	}
	result := Result{FinalText: final, Session: session}
	if cfg.Autosave.Enabled {
		if err := checkpointSave(paths, cfg.Autosave.Name, &session.Doc); err != nil {
			return result, fmt.Errorf("autosave: %w", err)
		}
		result.SavedSessionName = cfg.Autosave.Name
	}
	return result, nil
}
