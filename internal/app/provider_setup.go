package app

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

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
	// Return blink cmd if the current step is a Question
	if step, ok := seq.Steps[seq.Current]; ok {
		if q, ok := step.Component.(*component.Question); ok {
			return q.BlinkCmd()
		}
	}
	return nil
}

// ensureProviderConfigured checks if the active provider is configured and
// credentials are valid. Returns (blocked, cmd).
func (m *Model) ensureProviderConfigured() (bool, tea.Cmd) {
	if m.settings.Model == "" {
		return true, m.showModelPicker()
	}

	s := config.ResolveProviderSettings(m.endpoints, m.settings.Provider)
	if s == nil || !provider.IsConfigured(s) {
		return true, m.showModelPicker()
	}

	p := provider.Lookup(s.Name, s)
	if p != nil {
		// Refresh OAuth tokens that are near expiry — done via the
		// provider's CachedTokenSource in BuildGoAIModel. For now, check
		// the credential expiry directly.
		if s.Credentials != nil && s.Credentials.ActiveAuthMethod == config.AuthOAuth && s.Credentials.OAuth != nil {
			if time.Now().After(s.Credentials.OAuth.ExpiresAt.Add(-60 * time.Second)) {
				// Token near expiry — will be refreshed lazily by CachedTokenSource
				// in BuildGoAIModel. No need to block here.
			}
		}
	}

	return false, nil
}

// buildAuthWizard creates a Sequence (state machine) for provider configuration.
//
// Steps are declared by key and linked via OnAdvance callbacks:
//
//	baseURL  → Prompt to enter the provider base URL.
//	authPick → Picker to choose the auth method (only when >1 method).
//	apiKey   → Prompt to enter an API key.
//	oauthURL → Question that shows the OAuth URL the user must open.
//	oauthCode → Prompt to paste the authorization code.
//	done     → Question with Confirm / Cancel and a summary description.
//
// The start key and OnAdvance routers make the flow work for every
// combination of providers: known vs custom, single vs multiple auth
// methods, and the "none" method.
func (m *Model) buildAuthWizard(s *config.ProviderSettings) *component.Sequence {
	p := provider.GetByName(s.Name)
	authMethods := p.SupportedAuth()

	steps := make(map[string]component.SequenceStep)

	// --- Determine the start key ---
	var startKey string
	if p.RequiresBaseURL() {
		startKey = "baseURL"
	} else if len(authMethods) > 1 {
		startKey = "authPick"
	} else if len(authMethods) == 1 {
		startKey = methodKey(authMethods[0])
	} else {
		// No auth methods at all — just done
		startKey = "done"
	}

	// --- Gather saved values for pre-filling ---
	savedAuthMethod := ""
	savedAPIKey := ""
	if s.Credentials != nil && s.Credentials.ActiveAuthMethod != "" {
		savedAuthMethod = string(s.Credentials.ActiveAuthMethod)
	}
	if s.Credentials != nil && s.Credentials.APIKey != "" {
		savedAPIKey = s.Credentials.APIKey
	}

	// --- Helper: build description for the done step ---
	buildDoneDesc := func(r map[string]any) string {
		var parts []string
		parts = append(parts, fmt.Sprintf("Provider: %s", s.Name))
		if url, ok := r["baseURL"].(string); ok && url != "" {
			parts = append(parts, fmt.Sprintf("Base URL: %s", url))
		}
		if s.Credentials != nil {
			parts = append(parts, fmt.Sprintf("Auth: %s", s.Credentials.ActiveAuthMethod))
		} else if method, ok := r["authPick"].(component.PickerItem); ok {
			parts = append(parts, fmt.Sprintf("Auth: %s", method.Value))
		}
		if key, ok := r["apiKey"].(string); ok && key != "" {
			parts = append(parts, fmt.Sprintf("Key: %s", maskKey(key)))
		}
		if code, ok := r["oauthCode"].(string); ok && code != "" {
			parts = append(parts, "Auth: OAuth (token received)")
		}
		return strings.Join(parts, "\n")
	}

	// --- Step: baseURL ---
	if p.RequiresBaseURL() {
		defaultURL := p.DefaultBaseURL()
		// Use existing BaseURL if already set, otherwise use the default
		if s.BaseURL != "" {
			defaultURL = s.BaseURL
		}
		steps["baseURL"] = component.SequenceStep{
			Key: "baseURL",
			Component: &component.Prompt{
				Title:        fmt.Sprintf("Configure %s", s.Name),
				Description:  "Enter the provider base URL. For OpenAI-compatible servers like vLLM/LiteLLM, use the API root (usually ending in /v1), not the full /chat/completions URL.",
				Label:        "Base URL: ",
				DefaultValue: defaultURL,
			},
			OnAdvance: func(ctx any, r map[string]any) string {
				// Determine next step based on auth methods
				if len(authMethods) > 1 {
					return "authPick"
				} else if len(authMethods) == 1 {
					return methodKey(authMethods[0])
				}
				return "done"
			},
		}
	}

	// --- Step: authPick (only when >1 method) ---
	if len(authMethods) > 1 {
		methodLabels := make([]string, len(authMethods))
		for i, a := range authMethods {
			methodLabels[i] = string(a)
		}
		steps["authPick"] = component.SequenceStep{
			Key: "authPick",
			Component: &component.Picker{
				Title:       fmt.Sprintf("Auth method for %s", s.Name),
				Description: "Choose how to authenticate",
				Items: func() []component.PickerItem {
					items := make([]component.PickerItem, len(methodLabels))
					for i, label := range methodLabels {
						items[i] = component.PickerItem{Label: label, Value: label}
					}
					return items
				}(),
				DefaultValue: savedAuthMethod,
			},
			OnAdvance: func(ctx any, r map[string]any) string {
				// Route to the chosen method's step
				if item, ok := r["authPick"].(component.PickerItem); ok {
					return methodKey(config.AuthMethod(item.Value))
				}
				return "done"
			},
		}
	}

	// --- Step: apiKey ---
	steps["apiKey"] = component.SequenceStep{
		Key: "apiKey",
		Component: &component.Prompt{
			Title:        fmt.Sprintf("API Key for %s", s.Name),
			Description:  "Enter your API key for this provider",
			Label:        "Key: ",
			DefaultValue: savedAPIKey,
		},
		OnAdvance: func(ctx any, r map[string]any) string {
			return "done"
		},
	}

	// --- Step: oauthURL (now device auth for OpenAI) ---
	steps["oauthURL"] = component.SequenceStep{
		Key: "oauthURL",
		Component: &component.Question{
			Title:       fmt.Sprintf("OAuth for %s", s.Name),
			Description: "Loading...",
			Options:     []string{"I've entered the code"},
		},
		OnEnter: func(ctx any, r map[string]any) {
			step := steps["oauthURL"]
			if q, ok := step.Component.(*component.Question); ok {
				devAuth := provider.Lookup(s.Name, &config.ProviderSettings{Name: s.Name, Credentials: &config.ProviderCreds{}})
				if devAuth == nil {
					q.Description = fmt.Sprintf("Could not initialize auth flow for provider: %s", s.Name)
					return
				}
				visitURL, userCode, err := devAuth.StartDeviceAuth()
				if err != nil {
					q.Description = fmt.Sprintf("Could not start device auth: %v", err)
					return
				}
				q.Description = fmt.Sprintf("Visit this URL on any device (phone, laptop) and enter the code:\n\n  Code: %s\n  URL:  %s\n\nThen click below when you've entered it.", userCode, visitURL)
				r["oauthProvider"] = devAuth
				r["oauthDeviceAuthID"] = devAuth.GetDeviceAuthID()
				r["oauthUserCode"] = userCode
			}
			steps["oauthURL"] = step
		},
		OnAdvance: func(ctx any, r map[string]any) string {
			return "oauthCode"
		},
	}

	// --- Step: oauthCode (poll for device auth completion) ---
	steps["oauthCode"] = component.SequenceStep{
		Key: "oauthCode",
		Component: &component.Question{
			Title:       "Waiting for authorization...",
			Description: "Waiting for you to enter the code on your device...",
			Options:     []string{"Done"},
		},
		OnEnter: func(ctx any, r map[string]any) {
			step := steps["oauthCode"]
			if q, ok := step.Component.(*component.Question); ok {
				devAuth, ok := r["oauthProvider"].(provider.Provider)
				if !ok || devAuth == nil {
					q.Description = "Error: device auth state lost"
					return
				}
				deviceAuthID, hasID := r["oauthDeviceAuthID"].(string)
				userCode, hasCode := r["oauthUserCode"].(string)
				if !hasID || !hasCode {
					q.Description = "Error: device auth state lost"
					return
				}
				devAuth.SetDeviceState(deviceAuthID, userCode)
				if err := devAuth.PollDeviceAuth(); err != nil {
					q.Description = fmt.Sprintf("Authorization failed: %v", err)
					r["oauthError"] = err.Error()
					return
				}
				r["oauthCreds"] = devAuth.GetCredentials()
				q.Description = "Authorization successful!"
			}
			steps["oauthCode"] = step
		},
		OnAdvance: func(ctx any, r map[string]any) string {
			return "done"
		},
	}

	// --- Step: done (summary + confirm/cancel) ---
	steps["done"] = component.SequenceStep{
		Key: "done",
		Component: &component.Question{
			Title:   "Confirm Configuration",
			Options: []string{"Confirm", "Cancel"},
		},
		OnEnter: func(ctx any, r map[string]any) {
			// Build the summary description dynamically before rendering
			step := steps["done"]
			if q, ok := step.Component.(*component.Question); ok {
				q.Description = buildDoneDesc(r)
			}
			steps["done"] = step
		},
		OnAdvance: func(ctx any, r map[string]any) string {
			// Selection 0 = Confirm → finish ("")
			// Selection 1 = Cancel → signal via result
			if qr, ok := r["done"].(component.QuestionResult); ok {
				if qr.Selection == 1 {
					r["cancelled"] = true
				}
			}
			return "" // end sequence
		},
	}

	// --- Wire up the Sequence ---
	seq := &component.Sequence{
		Steps:    steps,
		StartKey: startKey,
		Result:   make(map[string]any),
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

// methodKey returns the step key for a given auth method.
func methodKey(m config.AuthMethod) string {
	switch m {
	case config.AuthNone:
		return "done"
	case config.AuthAPIKey:
		return "apiKey"
	case config.AuthOAuth:
		return "oauthURL"
	default:
		return "done"
	}
}

// maskKey masks an API key, showing only the first and last 4 characters.
func maskKey(key string) string {
	if len(key) <= 8 {
		return "****"
	}
	runes := []rune(key)
	return string(runes[:4]) + "..." + string(runes[len(runes)-4:])
}

// onProviderConfigComplete applies the wizard results to provider settings and saves.
func (m *Model) onProviderConfigComplete(s *config.ProviderSettings, results map[string]any) tea.Cmd {
	// Check if user cancelled
	if cancelled, ok := results["cancelled"].(bool); ok && cancelled {
		return m.setChatMode()
	}

	// Apply BaseURL
	if baseURL, ok := results["baseURL"].(string); ok && baseURL != "" {
		s.BaseURL = strings.TrimSpace(baseURL)
		if !strings.HasPrefix(s.BaseURL, "http") {
			s.BaseURL = "https://" + s.BaseURL
		}
	}

	meta := provider.GetByName(s.Name)
	authMethods := meta.SupportedAuth()

	// Determine active auth method
	if item, ok := results["authPick"].(component.PickerItem); ok {
		s.Credentials = &config.ProviderCreds{
			ActiveAuthMethod: config.AuthMethod(item.Value),
		}
	} else if len(authMethods) == 1 {
		s.Credentials = &config.ProviderCreds{
			ActiveAuthMethod: authMethods[0],
		}
	} else if s.Credentials == nil {
		s.Credentials = &config.ProviderCreds{
			ActiveAuthMethod: config.AuthNone,
		}
	}

	switch s.Credentials.ActiveAuthMethod {
	case config.AuthAPIKey:
		if key, ok := results["apiKey"].(string); ok {
			s.Credentials.APIKey = strings.TrimSpace(key)
		}
	case config.AuthOAuth:
		// Check if device auth already completed and stored credentials
		if creds, ok := results["oauthCreds"].(*config.ProviderCreds); ok && creds != nil {
			s.Credentials = creds
		} else if errMsg, ok := results["oauthError"].(string); ok && errMsg != "" {
			m.setNotification(ui.NotificationError, fmt.Sprintf("OAuth failed: %s", errMsg))
			return m.setChatMode()
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
