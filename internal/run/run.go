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
	Session      runtimeconfig.SessionRequest
	ChildSession *config.ChildSessionOptions
	OnEvent      func(chat.LoopEvent)
}

type Result struct {
	FinalText, SavedSessionName string
	Session                     *chat.Session
}

func checkpointSave(session *chat.Session) error {
	return session.Save()
}

func bootstrapSession(request Request) (*chat.Session, config.SessionConfig, error) {
	sessionRequest := request.Session
	cfg := sessionRequest.Config

	switch {
	case sessionRequest.ExistingSession != nil:
		session := chat.LoadRootSession(*sessionRequest.ExistingSession, sessionRequest.SessionName, sessionRequest.Paths, sessionRequest.Catalog)
		return session, session.Doc.Config, nil
	case request.ChildSession != nil:
		cs := request.ChildSession
		identity := config.SessionIdentity{
			ID:               cs.ID,
			ParentID:         cs.ParentID,
			RootID:           cs.RootID,
			ParentToolCallID: cs.ParentToolCallID,
			Depth:            cs.Depth,
		}
		session, err := chat.NewChildSession(cfg, identity, cs.ParentSessionDir, sessionRequest.Paths, sessionRequest.Catalog)
		return session, cfg, err
	default:
		cfg.Memory = config.SessionMemory{Namespace: string(cfg.Memory.Namespace), Path: cfg.Memory.Path, Instructions: cfg.Memory.Instructions}
		return chat.NewRootSession(cfg, sessionRequest.Paths, sessionRequest.Catalog), cfg, nil
	}
}

func Execute(ctx context.Context, request Request) (Result, error) {
	sessionRequest := request.Session
	session, cfg, err := bootstrapSession(request)
	if err != nil {
		return Result{}, err
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
	for event := range chat.RunLoop(ctx, session, paths, sessionRequest.Endpoints) {
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
				_ = checkpointSave(session)
			}
		}
		if event.Type == chat.LoopEventToolFlushed {
			finalPass = ""
			if cfg.Autosave.Enabled {
				_ = checkpointSave(session)
			}
		}
		if event.Type == chat.LoopEventError {
			if cfg.Autosave.Enabled {
				_ = checkpointSave(session)
			}
			if event.Error != nil {
				return Result{FinalText: final, Session: session}, event.Error
			}
			return Result{FinalText: final, Session: session}, fmt.Errorf("run failed")
		}
		if event.Type == chat.LoopEventNeedAuth {
			if cfg.Autosave.Enabled {
				_ = checkpointSave(session)
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
		if err := checkpointSave(session); err != nil {
			return result, fmt.Errorf("autosave: %w", err)
		}
		result.SavedSessionName = cfg.Autosave.Name
	}
	return result, nil
}
