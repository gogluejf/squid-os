package app

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"squid-os/internal/chat"
	"squid-os/internal/chat/provider"
	"squid-os/internal/config"
	"squid-os/internal/ui"
	"squid-os/internal/ui/component"
)

// showProviderConfig builds and displays an auth wizard for the given provider settings.
func (m *Model) showProviderConfig(s *config.ProviderSettings) tea.Cmd {
	seq := m.buildAuthWizard(s)
	seq.Init(m)
	m.setComponent(seq)
	// Return blink cmd for Question components
	if q, ok := seq.Steps[seq.Current].Component.(*component.Question); ok {
		return q.BlinkCmd()
	}
	return nil
}

// ensureProviderConfigured checks if the active provider is configured and
// credentials are valid. Returns (blocked, cmd).
func (m *Model) ensureProviderConfigured() (bool, tea.Cmd) {
	s := config.ResolveProviderSettings(m.endpoints, m.settings.Provider)
	if s == nil {
		// No settings entry — create one
		s = &config.ProviderSettings{Name: m.settings.Provider}
	}

	if !chat.IsConfigured(*s) {
		return true, m.showProviderConfig(s)
	}

	// Check if OAuth creds are expired and attempt auto-refresh
	impl := chat.LoadProviderImpl(*s)
	if impl != nil && impl.IsExpired() {
		if err := impl.Refresh(); err != nil {
			return true, m.showProviderConfig(s)
		}
		// Refresh succeeded — save updated creds
		if openai, ok := impl.(*provider.OpenAIProvider); ok {
			s.Credentials = openai.GetCredentials()
			m.saveSettings(*s)
		}
	}

	return false, nil
}

// buildAuthWizard creates a Sequence for provider configuration.
func (m *Model) buildAuthWizard(s *config.ProviderSettings) *component.Sequence {
	meta := chat.GetProviderMeta(s.Name)
	var steps []component.SequenceStep

	// Step 1: BaseURL for providers that need it
	if chat.NeedsURL(*s) {
		steps = append(steps, component.SequenceStep{
			Key: "baseURL",
			Component: &component.Prompt{
				Title: fmt.Sprintf("Configure %s", s.Name),
				Label: "Base URL: ",
			},
		})
	}

	authMethods := meta.SupportedAuth

	// Step 2: Auth method picker (if multiple methods available)
	if len(authMethods) > 1 {
		methodLabels := make([]string, len(authMethods))
		for i, a := range authMethods {
			methodLabels[i] = string(a)
		}
		steps = append(steps, component.SequenceStep{
			Key: "authMethod",
			Component: &component.Picker{
				Title: fmt.Sprintf("Auth method for %s", s.Name),
				Items: func() []component.PickerItem {
					items := make([]component.PickerItem, len(methodLabels))
					for i, label := range methodLabels {
						items[i] = component.PickerItem{Label: label, Value: label}
					}
					return items
				}(),
			},
		})

		// Credential entry steps for both methods
		steps = append(steps, component.SequenceStep{
			Key: "apiKey",
			Component: &component.Prompt{
				Title: fmt.Sprintf("API key for %s", s.Name),
				Label: "Key: ",
			},
		})
		steps = append(steps, component.SequenceStep{
			Key: "oauthURL",
			Component: &component.Prompt{
				Title: fmt.Sprintf("OAuth for %s", s.Name),
				Label: "Auth code: ",
			},
		})
	} else if len(authMethods) == 1 {
		method := authMethods[0]
		if method == config.AuthAPIKey {
			steps = append(steps, component.SequenceStep{
				Key: "apiKey",
				Component: &component.Prompt{
					Title: fmt.Sprintf("API key for %s", s.Name),
					Label: "Key: ",
				},
			})
		} else if method == config.AuthOAuth {
			steps = append(steps, component.SequenceStep{
				Key: "oauthURL",
				Component: &component.Question{
					Title:       fmt.Sprintf("OAuth for %s", s.Name),
					Description: "Open this URL in your browser, log in, then paste the authorization code:\n\n" + getOpenAIAuthURL("http://localhost:8080/callback"),
					Options:     []string{"I've pasted the code below"},
				},
			})
			steps = append(steps, component.SequenceStep{
				Key: "oauthCode",
				Component: &component.Prompt{
					Title: "Paste authorization code",
					Label: "Code: ",
				},
			})
		} else if method == config.AuthNone {
			steps = append(steps, component.SequenceStep{
				Key: "confirm",
				Component: &component.Question{
					Title:   fmt.Sprintf("%s configured", s.Name),
					Options: []string{"Done"},
				},
			})
		}
	} else {
		steps = append(steps, component.SequenceStep{
			Key: "confirm",
			Component: &component.Question{
				Title:   fmt.Sprintf("%s configured", s.Name),
				Options: []string{"Done"},
			},
		})
	}

	seq := &component.Sequence{
		Steps:  steps,
		Result: make(map[string]any),
		OnDone: func(ctx any, results map[string]any) tea.Cmd {
			model := ctx.(*Model)
			return model.onProviderConfigComplete(s, results)
		},
		OnCancel: func(ctx any) tea.Cmd {
			model := ctx.(*Model)
			return model.setChatMode()
		},
	}

	return seq
}

func getOpenAIAuthURL(redirectURI string) string {
	o := provider.NewOpenAIProvider(nil)
	url, err := o.StartOAuth(redirectURI)
	if err != nil {
		return fmt.Sprintf("Could not generate auth URL: %v", err)
	}
	return url
}

// onProviderConfigComplete applies the wizard results to provider settings and saves.
func (m *Model) onProviderConfigComplete(s *config.ProviderSettings, ctx map[string]any) tea.Cmd {
	// Apply BaseURL
	if baseURL, ok := ctx["baseURL"].(string); ok && baseURL != "" {
		s.BaseURL = strings.TrimSpace(baseURL)
		if !strings.HasPrefix(s.BaseURL, "http") {
			s.BaseURL = "https://" + s.BaseURL
		}
	}

	meta := chat.GetProviderMeta(s.Name)
	authMethods := meta.SupportedAuth

	// Apply auth method
	if method, ok := ctx["authMethod"].(component.PickerItem); ok {
		s.Credentials = &config.ProviderCreds{
			ActiveAuthMethod: config.AuthMethod(method.Value),
		}
	} else if methodStr, ok := ctx["authMethod"].(string); ok {
		s.Credentials = &config.ProviderCreds{
			ActiveAuthMethod: config.AuthMethod(methodStr),
		}
	}

	// If no credentials set yet but we have auth methods
	if s.Credentials == nil && len(authMethods) == 1 {
		s.Credentials = &config.ProviderCreds{
			ActiveAuthMethod: authMethods[0],
		}
	}

	if s.Credentials == nil {
		s.Credentials = &config.ProviderCreds{
			ActiveAuthMethod: config.AuthNone,
		}
	}

	switch s.Credentials.ActiveAuthMethod {
	case config.AuthAPIKey:
		if key, ok := ctx["apiKey"].(string); ok {
			s.Credentials.APIKey = strings.TrimSpace(key)
		}
	case config.AuthOAuth:
		var code string
		if c, ok := ctx["oauthCode"].(string); ok && c != "" {
			code = c
		} else if c, ok := ctx["oauthURL"].(string); ok && c != "" {
			code = c
		}
		if code != "" {
			o := provider.NewOpenAIProvider(s.Credentials)
			if _, err := o.StartOAuth("http://localhost:8080/callback"); err != nil {
				m.setNotification(ui.NotificationError, fmt.Sprintf("Failed to start OAuth: %v", err))
				return m.setChatMode()
			}
			if err := o.FinishOAuth(strings.TrimSpace(code)); err != nil {
				m.setNotification(ui.NotificationError, fmt.Sprintf("OAuth token exchange failed: %v", err))
				return m.setChatMode()
			}
			s.Credentials = o.GetCredentials()
		}
	}

	// Save
	m.saveSettings(*s)
	m.setNotification(ui.NotificationInfo, fmt.Sprintf("Configured provider: %s", s.Name))

	return tea.Batch(
		m.scanModelsCmd(),
		m.setChatMode(),
	)
}

// saveSettings updates the provider settings in endpoints and persists to disk.
func (m *Model) saveSettings(s config.ProviderSettings) {
	for i, existing := range m.endpoints.Providers {
		if existing.Name == s.Name {
			m.endpoints.Providers[i] = s
			break
		}
	}
	// If not found, append
	found := false
	for _, existing := range m.endpoints.Providers {
		if existing.Name == s.Name {
			found = true
			break
		}
	}
	if !found {
		m.endpoints.Providers = append(m.endpoints.Providers, s)
	}
	_ = config.SaveEndpoints(m.paths, m.endpoints)
}
