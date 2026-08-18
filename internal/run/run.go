package run

import (
	"context"
	"fmt"
	"time"

	"squid-os/internal/chat"
	"squid-os/internal/config"
	"squid-os/internal/media"
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
	if !session.Doc.Config.Autosave.Enabled {
		return nil
	}
	return session.Save()
}

func bootstrapSession(request Request) (*chat.Session, config.SessionConfig, error) {
	sessionRequest := request.Session
	cfg := sessionRequest.Config

	switch {
	case sessionRequest.ExistingSession != nil:
		session, err := chat.LoadRootSession(*sessionRequest.ExistingSession, sessionRequest.SessionName, sessionRequest.Paths, sessionRequest.Catalog)
		if err != nil {
			return nil, config.SessionConfig{}, err
		}
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
		session := chat.NewChildSession(cfg, identity, cs.ParentSessionDir, sessionRequest.Paths, sessionRequest.Catalog)
		return session, cfg, nil
	default:
		cfg.Memory = config.SessionMemory{Namespace: string(cfg.Memory.Namespace), Path: cfg.Memory.Path, Instructions: cfg.Memory.Instructions}
		return chat.NewRootSession(cfg, sessionRequest.Paths, sessionRequest.Catalog), cfg, nil
	}
}

func Execute(ctx context.Context, request Request) (Result, error) {
	sessionRequest := request.Session

	// Clean up stale temporary workspaces from crashed or abandoned sessions.
	// This is bounded: only removes Squid-owned temp dirs older than 24h,
	// capped at 100 entries per startup.
	_ = media.CleanupStale(media.CleanupPolicy{
		Root:      sessionRequest.Paths.TempFolder,
		OlderThan: 24 * time.Hour,
		MaxEntries: 100,
	})

	session, cfg, err := bootstrapSession(request)
	if err != nil {
		return Result{}, err
	}

	session.Append(chat.NewUserMessage(fmt.Sprintf("msg_%d", len(session.Doc.Messages)+1), sessionRequest.Prompt))
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
	checkpoint := func() error {
		return checkpointSave(session)
	}
	for event := range chat.RunLoop(ctx, session, paths, sessionRequest.Endpoints, checkpoint) {
		if request.OnEvent != nil {
			request.OnEvent(event)
		}
		if event.Type == chat.LoopEventText {
			finalPass += event.Text
		}
		if event.Type == chat.LoopEventDone {
			final = finalPass
			finalPass = ""
			_ = checkpointSave(session)
		}
		if event.Type == chat.LoopEventToolFlushed {
			finalPass = ""
		}
		if event.Type == chat.LoopEventError {
			_ = checkpointSave(session)
			if event.Error != nil {
				return Result{FinalText: final, Session: session}, event.Error
			}
			return Result{FinalText: final, Session: session}, fmt.Errorf("run failed")
		}
		if event.Type == chat.LoopEventNeedAuth {
			return Result{FinalText: final, Session: session}, fmt.Errorf("tool authorization required for %s", event.AuthRequest.ToolName)
		}
	}
	// If no turn completed (e.g., error before done), use whatever was accumulated
	if final == "" {
		final = finalPass
	}
	result := Result{FinalText: final, Session: session}
	if err := checkpointSave(session); err != nil {
		return result, fmt.Errorf("autosave: %w", err)
	}
	if cfg.Autosave.Enabled {
		result.SavedSessionName = session.Info.Name
	}
	return result, nil
}
