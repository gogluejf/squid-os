package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
)

// NewSessionFile creates a new empty session
func NewSessionFile(provider, model string, thinking bool, systemPrompt string) SessionFile {
	now := time.Now().UTC().Format(time.RFC3339)
	return SessionFile{
		Version: 1,
		Session: Session{
			ID:               uuid.New().String(),
			CreatedAt:        now,
			UpdatedAt:        now,
			Provider:         provider,
			Model:            model,
			Thinking:         thinking,
			SystemPromptFile: systemPrompt,
		},
	}
}

// SessionPath returns the file path for a session by name.
func SessionPath(p Paths, name string) string {
	return filepath.Join(p.Sessions, name+".chat.json")
}

// SaveSession writes a session to sessions/<name>.chat.json
func SaveSession(p Paths, name string, sf SessionFile) error {
	sf.Session.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	data, err := json.MarshalIndent(sf, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(SessionPath(p, name), data, 0644)
}

// LoadSession reads a session from sessions/<name>.chat.json
func LoadSession(p Paths, name string) (SessionFile, error) {
	file := SessionPath(p, name)
	data, err := os.ReadFile(file)
	if err != nil {
		return SessionFile{}, err
	}
	var sf SessionFile
	if err := json.Unmarshal(data, &sf); err != nil {
		return SessionFile{}, err
	}
	return sf, nil
}

// ListSessions returns available session names (without .chat.json), sorted by most recently modified.
func ListSessions(p Paths) []string {
	entries, err := os.ReadDir(p.Sessions)
	if err != nil {
		return nil
	}

	type entry struct {
		name    string
		modTime time.Time
	}
	var sessions []entry
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".chat.json") {
			info, err := e.Info()
			if err != nil {
				continue
			}
			sessions = append(sessions, entry{
				name:    strings.TrimSuffix(e.Name(), ".chat.json"),
				modTime: info.ModTime(),
			})
		}
	}

	sort.Slice(sessions, func(i, j int) bool {
		return sessions[i].modTime.After(sessions[j].modTime)
	})

	names := make([]string, len(sessions))
	for i, s := range sessions {
		names[i] = s.name
	}
	return names
}
