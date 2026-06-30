package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"squid-os/internal/config"
	"github.com/zendev-sh/goai/provider"
	goai_openai "github.com/zendev-sh/goai/provider/openai"
)

const (
	codexClientID         = "app_EMoamEEZ73f0CkXaXp7hrann"
	codexAuthURL          = "https://auth.openai.com/oauth/authorize"
	codexTokenURL         = "https://auth.openai.com/oauth/token"
	codexDeviceAuth       = "https://auth.openai.com/api/accounts/deviceauth/usercode"
	codexDeviceCode       = "https://auth.openai.com/api/accounts/deviceauth/token"
	codexDeviceUser       = "https://auth.openai.com/codex/device"
	codexDeviceCallback   = "https://auth.openai.com/deviceauth/callback"
)

func init() {
	Register(config.ProviderOpenAICodex, func(settings *config.ProviderSettings) Provider {
		return NewCodexProvider(settings)
	})
}

// CodexProvider implements Provider for OpenAI Codex.
type CodexProvider struct {
	settings     *config.ProviderSettings
	codeVerifier string
	state        string
	deviceAuthID string
	userCode     string
	pollInterval int
}

func NewCodexProvider(settings *config.ProviderSettings) *CodexProvider {
	if settings == nil {
		settings = &config.ProviderSettings{}
	}
	return &CodexProvider{settings: settings}
}

// --- Provider interface ---

func (o *CodexProvider) Name() string { return config.ProviderOpenAICodex }
func (o *CodexProvider) Dialect() config.Dialect { return config.DialectOpenAICodex }
func (o *CodexProvider) SupportedAuth() []config.AuthMethod {
	return []config.AuthMethod{config.AuthOAuth, config.AuthAPIKey}
}
func (o *CodexProvider) StaticModels() []string {
	return []string{
		"gpt-5.1-codex",
		"gpt-5.1-codex-max",
		"gpt-5.1-codex-mini",
		"gpt-5.2",
		"gpt-5.2-codex",
		"gpt-5.3-codex",
		"gpt-5.4",
		"gpt-5.4-mini",
	}
}
func (o *CodexProvider) DefaultBaseURL() string { return "https://chatgpt.com" }
func (o *CodexProvider) RequiresBaseURL() bool  { return false }
func (o *CodexProvider) RequestProviderOptions(model string, thinking bool) map[string]any {
	opts := map[string]any{}
	if thinking {
		opts["reasoning_effort"] = "medium"
	}
	if o.creds().ActiveAuthMethod == config.AuthOAuth {
		opts["store"] = false
	}
	if len(opts) == 0 {
		return nil
	}
	return opts
}

func (o *CodexProvider) BuildGoAIModel(model string) (provider.LanguageModel, bool, error) {
	if model == "" {
		model = "gpt-5.4"
	}
	var opts []goai_openai.Option
	switch o.creds().ActiveAuthMethod {
	case config.AuthAPIKey:
		opts = append(opts, goai_openai.WithAPIKey(o.creds().APIKey))
		baseURL := o.settings.BaseURL
		if baseURL == "" {
			baseURL = "https://api.openai.com/v1"
		}
		opts = append(opts, goai_openai.WithBaseURL(strings.TrimRight(baseURL, "/")))
	case config.AuthOAuth:
		if o.creds().OAuth == nil || o.creds().OAuth.AccessToken == "" {
			return nil, false, fmt.Errorf("codex: no OAuth token configured")
		}
		ts := provider.CachedTokenSource(func(ctx context.Context) (*provider.Token, error) {
			accessToken := o.creds().OAuth.AccessToken
			if accessToken == "" {
				if err := o.refreshOAuth(); err != nil {
					return nil, err
				}
				accessToken = o.creds().OAuth.AccessToken
			}
			return &provider.Token{
				Value:     accessToken,
				ExpiresAt: o.creds().OAuth.ExpiresAt,
			}, nil
		})
		opts = append(opts, goai_openai.WithTokenSource(ts))
		headers := map[string]string{
			"Originator": "opencode",
			"User-Agent": "squid-os",
		}
		if o.creds().OAuth != nil && o.creds().OAuth.AccountID != "" {
			headers["ChatGPT-Account-Id"] = o.creds().OAuth.AccountID
		}
		opts = append(opts, goai_openai.WithHeaders(headers))
		baseURL := o.settings.BaseURL
		if baseURL == "" {
			baseURL = "https://chatgpt.com/backend-api/codex"
		}
		opts = append(opts, goai_openai.WithBaseURL(strings.TrimRight(baseURL, "/")))
	default:
		return nil, false, fmt.Errorf("codex: unsupported auth method: %s", o.creds().ActiveAuthMethod)
	}
	// Codex/Qwen-like reasoning path can embed thinking in text content.
	return goai_openai.Chat(model, opts...), true, nil
}

func (o *CodexProvider) refreshOAuth() error {
	if o.creds().OAuth == nil || o.creds().OAuth.RefreshToken == "" {
		return fmt.Errorf("codex: no refresh token available")
	}

	data := url.Values{
		"grant_type":    {"refresh_token"},
		"client_id":     {codexClientID},
		"refresh_token": {o.creds().OAuth.RefreshToken},
	}

	payload := bytes.NewBufferString(data.Encode())
	req, err := http.NewRequest("POST", codexTokenURL, payload)
	if err != nil {
		return fmt.Errorf("codex refresh request failed: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("codex refresh request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("codex refresh returned %d: %s", resp.StatusCode, string(body))
	}

	var tokenResp struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int    `json:"expires_in"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return fmt.Errorf("codex refresh response parse error: %w", err)
	}

	o.creds().OAuth.AccessToken = tokenResp.AccessToken
	if tokenResp.RefreshToken != "" {
		o.creds().OAuth.RefreshToken = tokenResp.RefreshToken
	}
	o.creds().OAuth.ExpiresAt = time.Now().Add(time.Duration(tokenResp.ExpiresIn) * time.Second)
	if tokenResp.AccessToken != "" {
		o.creds().OAuth.AccountID = extractChatGPTAccountID(tokenResp.AccessToken)
	}
	return nil
}

func (o *CodexProvider) ListModels(ctx context.Context) ([]string, error) {
	token := o.getCurrentToken()
	if token == "" {
		return nil, fmt.Errorf("codex: no credentials configured for model listing")
	}

	switch o.creds().ActiveAuthMethod {
	case config.AuthOAuth:
		// Subscription-backed Codex/ChatGPT backend does not expose a clean API-models
		// endpoint we can rely on here. Use curated static models that reflect the
		// subscription-facing provider behavior instead of API-billed OpenAI models.
		return o.StaticModels(), nil
	case config.AuthAPIKey:
		baseURL := o.settings.BaseURL
		if baseURL == "" {
			baseURL = "https://api.openai.com"
		}
		req, err := http.NewRequestWithContext(ctx, "GET", strings.TrimRight(baseURL, "/")+"/v1/models", nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+token)

		client := &http.Client{Timeout: 10 * time.Second}
		resp, err := client.Do(req)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			return nil, fmt.Errorf("codex models endpoint returned %d: %s", resp.StatusCode, string(body))
		}

		var result struct {
			Data []struct {
				ID string `json:"id"`
			} `json:"data"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			return nil, err
		}

		models := make([]string, 0, len(result.Data))
		for _, m := range result.Data {
			models = append(models, m.ID)
		}
		return models, nil
	default:
		return nil, fmt.Errorf("codex: unsupported auth method: %s", o.creds().ActiveAuthMethod)
	}
}

// --- Device auth ---

func (o *CodexProvider) StartDeviceAuth() (visitURL string, code string, err error) {
	client := &http.Client{Timeout: 15 * time.Second}
	payload := []byte(`{"client_id":"` + codexClientID + `"}`)
	req, err := http.NewRequest("POST", codexDeviceAuth, bytes.NewReader(payload))
	if err != nil {
		return "", "", fmt.Errorf("codex device auth request failed: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "squid-os")

	resp, err := client.Do(req)
	if err != nil {
		return "", "", fmt.Errorf("codex device auth request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", "", fmt.Errorf("codex device auth returned %d: %s", resp.StatusCode, string(body))
	}

	var deviceData struct {
		DeviceAuthID string `json:"device_auth_id"`
		UserCode     string `json:"user_code"`
		Interval     string `json:"interval"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&deviceData); err != nil {
		return "", "", fmt.Errorf("codex device auth response parse error: %w", err)
	}

	o.deviceAuthID = deviceData.DeviceAuthID
	o.userCode = deviceData.UserCode
	if deviceData.Interval == "" {
		o.pollInterval = 5
	} else {
		o.pollInterval, _ = fmt.Sscanf(deviceData.Interval, "%d", &o.pollInterval)
		if o.pollInterval < 1 {
			o.pollInterval = 5
		}
	}

	return codexDeviceUser, deviceData.UserCode, nil
}

func (o *CodexProvider) PollDeviceAuth() error {
	client := &http.Client{Timeout: 15 * time.Second}

	for {
		payload := []byte(`{"device_auth_id":"` + o.deviceAuthID + `","user_code":"` + o.userCode + `"}`)
		req, err := http.NewRequest("POST", codexDeviceCode, bytes.NewReader(payload))
		if err != nil {
			return fmt.Errorf("codex device poll request failed: %w", err)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("User-Agent", "squid-os")

		resp, err := client.Do(req)
		if err != nil {
			return fmt.Errorf("codex device poll request failed: %w", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode == http.StatusOK {
			var deviceToken struct {
				AuthorizationCode string `json:"authorization_code"`
				CodeVerifier      string `json:"code_verifier"`
			}
			if err := json.NewDecoder(resp.Body).Decode(&deviceToken); err != nil {
				return fmt.Errorf("codex device token parse error: %w", err)
			}

			data := url.Values{
				"grant_type":    {"authorization_code"},
				"code":          {deviceToken.AuthorizationCode},
				"redirect_uri":  {codexDeviceCallback},
				"client_id":     {codexClientID},
				"code_verifier": {deviceToken.CodeVerifier},
			}

			tokenResp, err := client.PostForm(codexTokenURL, data)
			if err != nil {
				return fmt.Errorf("codex token exchange failed: %w", err)
			}
			defer tokenResp.Body.Close()

			if tokenResp.StatusCode != http.StatusOK {
				body, _ := io.ReadAll(tokenResp.Body)
				return fmt.Errorf("codex token exchange returned %d: %s", tokenResp.StatusCode, string(body))
			}

			var tr struct {
				AccessToken  string `json:"access_token"`
				RefreshToken string `json:"refresh_token"`
				ExpiresIn    int    `json:"expires_in"`
			}
			if err := json.NewDecoder(tokenResp.Body).Decode(&tr); err != nil {
				return fmt.Errorf("codex token response parse error: %w", err)
			}

			accountID := extractChatGPTAccountID(tr.AccessToken)

			o.creds().ActiveAuthMethod = config.AuthOAuth
			o.creds().OAuth = &config.OAuthCreds{
				AccessToken:  tr.AccessToken,
				RefreshToken: tr.RefreshToken,
				AccountID:    accountID,
				ExpiresAt:    time.Now().Add(time.Duration(tr.ExpiresIn) * time.Second),
			}
			return nil

		} else if resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusNotFound {
			time.Sleep(time.Duration(o.pollInterval+3) * time.Second)
			continue
		} else {
			body, _ := io.ReadAll(resp.Body)
			return fmt.Errorf("codex device poll returned %d: %s", resp.StatusCode, string(body))
		}
	}
}

// --- Standard OAuth ---

func (o *CodexProvider) StartOAuth(redirectURI string) (string, error) {
	o.codeVerifier = generatePKCEVerifier()
	challenge := generateCodeChallenge(o.codeVerifier)
	state := generateState()
	o.state = state

	params := url.Values{
		"client_id":                 {codexClientID},
		"response_type":             {"code"},
		"code_challenge":            {challenge},
		"code_challenge_method":     {"S256"},
		"redirect_uri":              {redirectURI},
		"state":                     {state},
		"scope":                     {"openid profile email offline_access"},
		"codex_cli_simplified_flow": {"true"},
		"id_token_add_organizations": {"true"},
		"originator":                {"opencode"},
	}

	return codexAuthURL + "?" + params.Encode(), nil
}

func (o *CodexProvider) FinishOAuth(code, redirectURI string) error {
	if o.codeVerifier == "" {
		return fmt.Errorf("codex: no PKCE code verifier — call StartOAuth first")
	}

	data := url.Values{
		"grant_type":    {"authorization_code"},
		"client_id":     {codexClientID},
		"code":          {code},
		"code_verifier": {o.codeVerifier},
		"redirect_uri":  {redirectURI},
	}

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.PostForm(codexTokenURL, data)
	if err != nil {
		return fmt.Errorf("codex token exchange failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("codex token exchange returned %d: %s", resp.StatusCode, string(body))
	}

	var tokenResp struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int    `json:"expires_in"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return fmt.Errorf("codex token response parse error: %w", err)
	}

	accountID := extractChatGPTAccountID(tokenResp.AccessToken)

	o.creds().ActiveAuthMethod = config.AuthOAuth
	o.creds().OAuth = &config.OAuthCreds{
		AccessToken:  tokenResp.AccessToken,
		RefreshToken: tokenResp.RefreshToken,
		AccountID:    accountID,
		ExpiresAt:    time.Now().Add(time.Duration(tokenResp.ExpiresIn) * time.Second),
	}
	o.codeVerifier = ""
	return nil
}

// --- Accessors ---

func (o *CodexProvider) GetCredentials() *config.ProviderCreds {
	creds := *o.creds()
	if o.creds().OAuth != nil {
		creds.OAuth = &config.OAuthCreds{
			AccessToken:  o.creds().OAuth.AccessToken,
			RefreshToken: o.creds().OAuth.RefreshToken,
			AccountID:    o.creds().OAuth.AccountID,
			ExpiresAt:    o.creds().OAuth.ExpiresAt,
		}
	}
	return &creds
}

func (o *CodexProvider) CodeVerifier() string           { return o.codeVerifier }
func (o *CodexProvider) SetCodeVerifier(v string)       { o.codeVerifier = v }
func (o *CodexProvider) State() string                  { return o.state }
func (o *CodexProvider) GetDeviceAuthID() string        { return o.deviceAuthID }
func (o *CodexProvider) SetDeviceState(id, code string) {
	o.deviceAuthID = id
	o.userCode = code
	o.pollInterval = 5
}

func (o *CodexProvider) creds() *config.ProviderCreds {
	if o.settings == nil || o.settings.Credentials == nil {
		o.settings = &config.ProviderSettings{Credentials: &config.ProviderCreds{}}
	}
	return o.settings.Credentials
}

func (o *CodexProvider) getCurrentToken() string {
	c := o.creds()
	switch c.ActiveAuthMethod {
	case config.AuthAPIKey:
		return c.APIKey
	case config.AuthOAuth:
		if c.OAuth != nil {
			return c.OAuth.AccessToken
		}
		return ""
	default:
		return ""
	}
}
