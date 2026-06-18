package provider

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"squid-os/internal/config"
)

const (
	codexClientID         = "app_EMoamEEZ73f0CkXaXp7hrann"
	codexIssuer           = "https://auth.openai.com"
	codexAuthURL          = "https://auth.openai.com/oauth/authorize"
	codexTokenURL         = "https://auth.openai.com/oauth/token"
	codexDeviceAuth       = "https://auth.openai.com/api/accounts/deviceauth/usercode"
	codexDeviceCode       = "https://auth.openai.com/api/accounts/deviceauth/token"
	codexDeviceUser       = "https://auth.openai.com/codex/device"
	codexDeviceCallback   = "https://auth.openai.com/deviceauth/callback"
	codexBackendAPI       = "https://chatgpt.com/backend-api/codex/responses"
	codexPlatformAPI      = "https://api.openai.com/v1/responses"
	codexPlatformModels   = "https://api.openai.com/v1/models"
)

func init() {
	RegisterMeta(ProviderMeta{
		Name:          config.ProviderOpenAICodex,
		Dialect:       config.DialectOpenAICodex,
		SupportedAuth: []config.AuthMethod{config.AuthOAuth, config.AuthAPIKey},
		StaticModels: []string{
			"gpt-5.1-codex",
			"gpt-5.1-codex-max",
			"gpt-5.1-codex-mini",
			"gpt-5.2",
			"gpt-5.2-codex",
			"gpt-5.3-codex",
			"gpt-5.4",
			"gpt-5.4-mini",
		},
		New: func(creds *config.ProviderCreds) ProviderImpl {
			return NewCodexProvider(creds)
		},
	})
}

// CodexProvider implements ProviderImpl for OpenAI Codex.
// Supports OAuth (device flow) and API key auth.
type CodexProvider struct {
	creds *config.ProviderCreds

	// Device auth fields
	codeVerifier string
	state        string
	deviceAuthID string
	userCode     string
	pollInterval int // seconds
}

func NewCodexProvider(creds *config.ProviderCreds) *CodexProvider {
	if creds == nil {
		creds = &config.ProviderCreds{}
	}
	return &CodexProvider{creds: creds}
}

// --- OAuth Device Flow ---

// StartDeviceAuth initiates the device authorization flow. Returns the URL the
// user must visit and a short user_code they must enter.
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

// PollDeviceAuth polls until the user completes authorization, then exchanges for tokens.
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

			// Exchange for real tokens
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

			// Extract AccountID from the JWT
			accountID := extractChatGPTAccountID(tr.AccessToken)

			o.creds.ActiveAuthMethod = config.AuthOAuth
			o.creds.OAuth = &config.OAuthCreds{
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

// --- Standard OAuth (browser redirect flow) ---

// StartOAuth returns the authorization URL for browser-based OAuth.
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

// FinishOAuth exchanges the authorization code for tokens.
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

	o.creds.ActiveAuthMethod = config.AuthOAuth
	o.creds.OAuth = &config.OAuthCreds{
		AccessToken:  tokenResp.AccessToken,
		RefreshToken: tokenResp.RefreshToken,
		AccountID:    accountID,
		ExpiresAt:    time.Now().Add(time.Duration(tokenResp.ExpiresIn) * time.Second),
	}
	o.codeVerifier = ""
	return nil
}

// --- ProviderImpl interface ---

// GetChatURL returns the inference endpoint based on auth method.
func (o *CodexProvider) GetChatURL(settings *config.ProviderSettings) string {
	if settings != nil && settings.Credentials != nil && settings.Credentials.ActiveAuthMethod == config.AuthOAuth {
		return codexBackendAPI
	}
	return codexPlatformAPI
}

// GetModelsURL returns the models listing URL for API key auth.
func (o *CodexProvider) GetModelsURL(settings *config.ProviderSettings) string {
	if settings != nil && settings.Credentials != nil && settings.Credentials.ActiveAuthMethod == config.AuthAPIKey {
		return codexPlatformModels
	}
	return "" // OAuth has no models endpoint — uses StaticModels
}

// PrepareRequest adds the required Codex headers.
func (o *CodexProvider) PrepareRequest(req *http.Request) error {
	token := o.getCurrentToken()
	if token == "" {
		return fmt.Errorf("codex: no credentials configured")
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Originator", "opencode")
	req.Header.Set("User-Agent", "squid-os")

	// Add ChatGPT-Account-Id if we have it
	if o.creds != nil && o.creds.OAuth != nil && o.creds.OAuth.AccountID != "" {
		req.Header.Set("ChatGPT-Account-Id", o.creds.OAuth.AccountID)
	}
	return nil
}

func (o *CodexProvider) IsExpired() bool {
	if o.creds == nil || o.creds.OAuth == nil {
		return false
	}
	return time.Now().After(o.creds.OAuth.ExpiresAt.Add(-60 * time.Second))
}

func (o *CodexProvider) Refresh() error {
	if o.creds == nil || o.creds.OAuth == nil || o.creds.OAuth.RefreshToken == "" {
		return fmt.Errorf("codex: no refresh token available")
	}

	data := url.Values{
		"grant_type":    {"refresh_token"},
		"client_id":     {codexClientID},
		"refresh_token": {o.creds.OAuth.RefreshToken},
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

	o.creds.OAuth.AccessToken = tokenResp.AccessToken
	if tokenResp.RefreshToken != "" {
		o.creds.OAuth.RefreshToken = tokenResp.RefreshToken
	}
	o.creds.OAuth.ExpiresAt = time.Now().Add(time.Duration(tokenResp.ExpiresIn) * time.Second)

	// Update AccountID from new token in case it changed
	if tokenResp.AccessToken != "" {
		o.creds.OAuth.AccountID = extractChatGPTAccountID(tokenResp.AccessToken)
	}
	return nil
}

// --- Helpers ---

func (o *CodexProvider) getCurrentToken() string {
	if o.creds == nil {
		return ""
	}
	switch o.creds.ActiveAuthMethod {
	case config.AuthAPIKey:
		return o.creds.APIKey
	case config.AuthOAuth:
		if o.creds.OAuth != nil {
			return o.creds.OAuth.AccessToken
		}
		return ""
	default:
		return ""
	}
}

// GetCredentials returns a copy of the current credentials for persistence.
func (o *CodexProvider) GetCredentials() *config.ProviderCreds {
	if o.creds == nil {
		return nil
	}
	creds := *o.creds
	if o.creds.OAuth != nil {
		creds.OAuth = &config.OAuthCreds{
			AccessToken:  o.creds.OAuth.AccessToken,
			RefreshToken: o.creds.OAuth.RefreshToken,
			AccountID:    o.creds.OAuth.AccountID,
			ExpiresAt:    o.creds.OAuth.ExpiresAt,
		}
	}
	return &creds
}

// SetDeviceState restores device auth state so a new instance can continue polling.
func (o *CodexProvider) SetDeviceState(deviceAuthID, userCode string) {
	o.deviceAuthID = deviceAuthID
	o.userCode = userCode
	o.pollInterval = 5
}

// GetDeviceAuthID returns the device auth ID for storing across wizard steps.
func (o *CodexProvider) GetDeviceAuthID() string {
	return o.deviceAuthID
}

// CodeVerifier returns the PKCE code verifier generated during StartOAuth.
func (o *CodexProvider) CodeVerifier() string {
	return o.codeVerifier
}

// SetCodeVerifier sets the PKCE code verifier directly.
func (o *CodexProvider) SetCodeVerifier(v string) {
	o.codeVerifier = v
}

// State returns the OAuth state parameter generated during StartOAuth.
func (o *CodexProvider) State() string {
	return o.state
}