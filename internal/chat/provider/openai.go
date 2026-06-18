package provider

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"squid-os/internal/config"
)

const (
	openaiClientID        = "app_EMoamEEZ73f0CkXaXp7hrann"
	openaiIssuer          = "https://auth.openai.com"
	openaiAuthURL         = "https://auth.openai.com/oauth/authorize"
	openaiTokenURL        = "https://auth.openai.com/oauth/token"
	openaiDeviceAuth      = "https://auth.openai.com/api/accounts/deviceauth/usercode"
	openaiDeviceCode      = "https://auth.openai.com/api/accounts/deviceauth/token"
	openaiDeviceUser      = "https://auth.openai.com/codex/device"
	openaiDeviceCallback  = "https://auth.openai.com/deviceauth/callback"
)

func init() {
	Register(config.ProviderOpenAI, func(settings *config.ProviderSettings) Provider {
		return NewOpenAIProvider(settings)
	})
}

// OpenAIProvider implements Provider for OpenAI.
type OpenAIProvider struct {
	settings     *config.ProviderSettings
	codeVerifier string
	state        string
	deviceAuthID string
	userCode     string
	pollInterval int
}

func NewOpenAIProvider(settings *config.ProviderSettings) *OpenAIProvider {
	if settings == nil {
		settings = &config.ProviderSettings{}
	}
	return &OpenAIProvider{settings: settings}
}

// --- Provider interface ---

func (o *OpenAIProvider) Name() string                         { return config.ProviderOpenAI }
func (o *OpenAIProvider) Dialect() config.Dialect              { return config.DialectOpenAICompatible }
func (o *OpenAIProvider) SupportedAuth() []config.AuthMethod   { return []config.AuthMethod{config.AuthAPIKey, config.AuthOAuth} }
func (o *OpenAIProvider) StaticModels() []string               { return nil }
func (o *OpenAIProvider) DefaultBaseURL() string               { return "https://api.openai.com" }
func (o *OpenAIProvider) RequiresBaseURL() bool                { return false }

func (o *OpenAIProvider) GetChatURL() string {
	if o.creds().ActiveAuthMethod == config.AuthOAuth {
		return "https://chatgpt.com/backend-api/codex/responses"
	}
	return "https://api.openai.com/v1/chat/completions"
}

func (o *OpenAIProvider) GetModelsURL() string {
	return "https://api.openai.com/v1/models"
}

func (o *OpenAIProvider) PrepareRequest(req *http.Request) error {
	token := o.getCurrentToken()
	if token == "" {
		return fmt.Errorf("openai: no credentials configured")
	}
	req.Header.Set("Authorization", "Bearer "+token)
	if o.creds().ActiveAuthMethod == config.AuthOAuth {
		req.Header.Set("Originator", "opencode")
		req.Header.Set("User-Agent", "squid-os")
		if o.creds().OAuth != nil && o.creds().OAuth.AccountID != "" {
			req.Header.Set("ChatGPT-Account-Id", o.creds().OAuth.AccountID)
		}
	}
	return nil
}

func (o *OpenAIProvider) IsExpired() bool {
	if o.creds().OAuth == nil {
		return false
	}
	return time.Now().After(o.creds().OAuth.ExpiresAt.Add(-60 * time.Second))
}

func (o *OpenAIProvider) Refresh() error {
	if o.creds().OAuth == nil || o.creds().OAuth.RefreshToken == "" {
		return fmt.Errorf("openai: no refresh token available")
	}

	data := url.Values{
		"grant_type":    {"refresh_token"},
		"client_id":     {openaiClientID},
		"refresh_token": {o.creds().OAuth.RefreshToken},
	}

	payload := bytes.NewBufferString(data.Encode())
	req, err := http.NewRequest("POST", openaiTokenURL, payload)
	if err != nil {
		return fmt.Errorf("openai refresh request failed: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("openai refresh request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("openai refresh returned %d: %s", resp.StatusCode, string(body))
	}

	var tokenResp struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int    `json:"expires_in"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return fmt.Errorf("openai refresh response parse error: %w", err)
	}

	o.creds().OAuth.AccessToken = tokenResp.AccessToken
	if tokenResp.RefreshToken != "" {
		o.creds().OAuth.RefreshToken = tokenResp.RefreshToken
	}
	o.creds().OAuth.ExpiresAt = time.Now().Add(time.Duration(tokenResp.ExpiresIn) * time.Second)
	return nil
}

// --- Device auth ---

func (o *OpenAIProvider) StartDeviceAuth() (visitURL string, code string, err error) {
	client := &http.Client{Timeout: 15 * time.Second}
	payload := []byte(`{"client_id":"` + openaiClientID + `"}`)
	req, err := http.NewRequest("POST", openaiDeviceAuth, bytes.NewReader(payload))
	if err != nil {
		return "", "", fmt.Errorf("openai device auth request failed: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "squid-os")

	resp, err := client.Do(req)
	if err != nil {
		return "", "", fmt.Errorf("openai device auth request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", "", fmt.Errorf("openai device auth returned %d: %s", resp.StatusCode, string(body))
	}

	var deviceData struct {
		DeviceAuthID string `json:"device_auth_id"`
		UserCode     string `json:"user_code"`
		Interval     string `json:"interval"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&deviceData); err != nil {
		return "", "", fmt.Errorf("openai device auth response parse error: %w", err)
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

	return openaiDeviceUser, deviceData.UserCode, nil
}

func (o *OpenAIProvider) PollDeviceAuth() error {
	client := &http.Client{Timeout: 15 * time.Second}

	for {
		payload := []byte(`{"device_auth_id":"` + o.deviceAuthID + `","user_code":"` + o.userCode + `"}`)
		req, err := http.NewRequest("POST", openaiDeviceCode, bytes.NewReader(payload))
		if err != nil {
			return fmt.Errorf("openai device poll request failed: %w", err)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("User-Agent", "squid-os")

		resp, err := client.Do(req)
		if err != nil {
			return fmt.Errorf("openai device poll request failed: %w", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode == http.StatusOK {
			var deviceToken struct {
				AuthorizationCode string `json:"authorization_code"`
				CodeVerifier      string `json:"code_verifier"`
			}
			if err := json.NewDecoder(resp.Body).Decode(&deviceToken); err != nil {
				return fmt.Errorf("openai device token parse error: %w", err)
			}

			data := url.Values{
				"grant_type":    {"authorization_code"},
				"code":          {deviceToken.AuthorizationCode},
				"redirect_uri":  {openaiDeviceCallback},
				"client_id":     {openaiClientID},
				"code_verifier": {deviceToken.CodeVerifier},
			}

			tokenResp, err := client.PostForm(openaiTokenURL, data)
			if err != nil {
				return fmt.Errorf("openai token exchange failed: %w", err)
			}
			defer tokenResp.Body.Close()

			if tokenResp.StatusCode != http.StatusOK {
				body, _ := io.ReadAll(tokenResp.Body)
				return fmt.Errorf("openai token exchange returned %d: %s", resp.StatusCode, string(body))
			}

			var tr struct {
				AccessToken  string `json:"access_token"`
				RefreshToken string `json:"refresh_token"`
				ExpiresIn    int    `json:"expires_in"`
			}
			if err := json.NewDecoder(tokenResp.Body).Decode(&tr); err != nil {
				return fmt.Errorf("openai token response parse error: %w", err)
			}

			o.creds().ActiveAuthMethod = config.AuthOAuth
			o.creds().OAuth = &config.OAuthCreds{
				AccessToken:  tr.AccessToken,
				RefreshToken: tr.RefreshToken,
				AccountID:    extractChatGPTAccountID(tr.AccessToken),
				ExpiresAt:    time.Now().Add(time.Duration(tr.ExpiresIn) * time.Second),
			}
			return nil

		} else if resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusNotFound {
			time.Sleep(time.Duration(o.pollInterval+3) * time.Second)
			continue
		} else {
			body, _ := io.ReadAll(resp.Body)
			return fmt.Errorf("openai device poll returned %d: %s", resp.StatusCode, string(body))
		}
	}
}

// --- Standard OAuth ---

func (o *OpenAIProvider) StartOAuth(redirectURI string) (string, error) {
	o.codeVerifier = generatePKCEVerifier()
	challenge := generateCodeChallenge(o.codeVerifier)
	state := generateState()
	o.state = state

	params := url.Values{
		"client_id":                 {openaiClientID},
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

	return openaiAuthURL + "?" + params.Encode(), nil
}

func (o *OpenAIProvider) FinishOAuth(code, redirectURI string) error {
	if o.codeVerifier == "" {
		return fmt.Errorf("openai: no PKCE code verifier — call StartOAuth first")
	}

	data := url.Values{
		"grant_type":    {"authorization_code"},
		"client_id":     {openaiClientID},
		"code":          {code},
		"code_verifier": {o.codeVerifier},
		"redirect_uri":  {redirectURI},
	}

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.PostForm(openaiTokenURL, data)
	if err != nil {
		return fmt.Errorf("openai token exchange failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("openai token exchange returned %d: %s", resp.StatusCode, string(body))
	}

	var tokenResp struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int    `json:"expires_in"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return fmt.Errorf("openai token response parse error: %w", err)
	}

	o.creds().ActiveAuthMethod = config.AuthOAuth
	o.creds().OAuth = &config.OAuthCreds{
		AccessToken:  tokenResp.AccessToken,
		RefreshToken: tokenResp.RefreshToken,
		AccountID:    extractChatGPTAccountID(tokenResp.AccessToken),
		ExpiresAt:    time.Now().Add(time.Duration(tokenResp.ExpiresIn) * time.Second),
	}
	o.codeVerifier = ""
	return nil
}

// --- Accessors ---

func (o *OpenAIProvider) GetCredentials() *config.ProviderCreds {
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

func (o *OpenAIProvider) CodeVerifier() string   { return o.codeVerifier }
func (o *OpenAIProvider) SetCodeVerifier(v string) { o.codeVerifier = v }
func (o *OpenAIProvider) State() string           { return o.state }
func (o *OpenAIProvider) GetDeviceAuthID() string { return o.deviceAuthID }
func (o *OpenAIProvider) SetDeviceState(id, code string) {
	o.deviceAuthID = id
	o.userCode = code
	o.pollInterval = 5
}

func (o *OpenAIProvider) creds() *config.ProviderCreds {
	if o.settings == nil || o.settings.Credentials == nil {
		o.settings = &config.ProviderSettings{Credentials: &config.ProviderCreds{}}
	}
	return o.settings.Credentials
}

func (o *OpenAIProvider) getCurrentToken() string {
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

func generatePKCEVerifier() string {
	const chars = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-._~"
	b := make([]byte, 43)
	rand.Read(b)
	for i := range b {
		b[i] = chars[b[i]%byte(len(chars))]
	}
	return string(b)
}

func generateCodeChallenge(verifier string) string {
	h := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(h[:])
}

func generateState() string {
	b := make([]byte, 16)
	rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}
