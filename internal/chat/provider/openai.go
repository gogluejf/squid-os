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
	RegisterMeta(ProviderMeta{
		Name:          config.ProviderOpenAI,
		ChatURL:       "https://api.openai.com/v1/chat/completions",
		ModelsURL:     "https://api.openai.com/v1/models",
		Dialect:       config.DialectOpenAICompatible,
		SupportedAuth: []config.AuthMethod{config.AuthAPIKey, config.AuthOAuth},
		New: func(creds *config.ProviderCreds) ProviderImpl {
			return NewOpenAIProvider(creds)
		},
	})
}

// OpenAIProvider implements ProviderImpl for OpenAI authentication.
type OpenAIProvider struct {
	codeVerifier string
	state        string
	creds        *config.ProviderCreds

	// Device auth fields
	deviceAuthID string
	userCode     string
	pollInterval int // seconds
}

// NewOpenAIProvider creates an OpenAIProvider from user settings.
func NewOpenAIProvider(creds *config.ProviderCreds) *OpenAIProvider {
	if creds == nil {
		creds = &config.ProviderCreds{}
	}
	return &OpenAIProvider{creds: creds}
}

// StartOAuth returns the authorization URL the user must visit.
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

// FinishOAuth exchanges the authorization code for tokens.
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

	o.creds.ActiveAuthMethod = config.AuthOAuth
	o.creds.OAuth = &config.OAuthCreds{
		AccessToken:  tokenResp.AccessToken,
		RefreshToken: tokenResp.RefreshToken,
		ExpiresAt:    time.Now().Add(time.Duration(tokenResp.ExpiresIn) * time.Second),
	}
	o.codeVerifier = ""
	return nil
}

// StartDeviceAuth initiates the device authorization flow. Returns the URL the
// user must visit and a short user_code they must enter.  This flow works on
// headless machines with no browser — the user visits the URL on any device.
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
		Interval     string `json:"interval"` // seconds
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

// PollDeviceAuth polls the device auth endpoint until the user has completed
// authorization or an error occurs.  The pollInterval was set by StartDeviceAuth.
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
			// User completed auth — we get authorization_code + code_verifier
			var deviceToken struct {
				AuthorizationCode string `json:"authorization_code"`
				CodeVerifier      string `json:"code_verifier"`
			}
			if err := json.NewDecoder(resp.Body).Decode(&deviceToken); err != nil {
				return fmt.Errorf("openai device token parse error: %w", err)
			}

			// Exchange the authorization code for real tokens
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
				return fmt.Errorf("openai token exchange returned %d: %s", tokenResp.StatusCode, string(body))
			}

			var tr struct {
				AccessToken  string `json:"access_token"`
				RefreshToken string `json:"refresh_token"`
				ExpiresIn    int    `json:"expires_in"`
			}
			if err := json.NewDecoder(tokenResp.Body).Decode(&tr); err != nil {
				return fmt.Errorf("openai token response parse error: %w", err)
			}

			o.creds.ActiveAuthMethod = config.AuthOAuth
			o.creds.OAuth = &config.OAuthCreds{
				AccessToken:  tr.AccessToken,
				RefreshToken: tr.RefreshToken,
				ExpiresAt:    time.Now().Add(time.Duration(tr.ExpiresIn) * time.Second),
			}
			return nil

		} else if resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusNotFound {
			// User hasn't completed yet — keep polling
			time.Sleep(time.Duration(o.pollInterval+3) * time.Second) // 3s safety margin
			continue
		} else {
			body, _ := io.ReadAll(resp.Body)
			return fmt.Errorf("openai device poll returned %d: %s", resp.StatusCode, string(body))
		}
	}
}

// PrepareRequest injects the Authorization header and Codex-specific headers.
func (o *OpenAIProvider) PrepareRequest(req *http.Request) error {
	token := o.getCurrentToken()
	if token == "" {
		return fmt.Errorf("openai: no credentials configured")
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Originator", "opencode")
	req.Header.Set("User-Agent", "squid-os")
	return nil
}

// IsExpired returns true if OAuth credentials have expired.
func (o *OpenAIProvider) IsExpired() bool {
	if o.creds == nil || o.creds.OAuth == nil {
		return false
	}
	return time.Now().After(o.creds.OAuth.ExpiresAt.Add(-60 * time.Second))
}

// Refresh attempts to refresh OAuth tokens.
func (o *OpenAIProvider) Refresh() error {
	if o.creds == nil || o.creds.OAuth == nil || o.creds.OAuth.RefreshToken == "" {
		return fmt.Errorf("openai: no refresh token available")
	}

	data := url.Values{
		"grant_type":    {"refresh_token"},
		"client_id":     {openaiClientID},
		"refresh_token": {o.creds.OAuth.RefreshToken},
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

	o.creds.OAuth.AccessToken = tokenResp.AccessToken
	if tokenResp.RefreshToken != "" {
		o.creds.OAuth.RefreshToken = tokenResp.RefreshToken
	}
	o.creds.OAuth.ExpiresAt = time.Now().Add(time.Duration(tokenResp.ExpiresIn) * time.Second)
	return nil
}

// GetCredentials returns a copy of the current credentials for persistence.
func (o *OpenAIProvider) GetCredentials() *config.ProviderCreds {
	if o.creds == nil {
		return nil
	}
	creds := *o.creds
	if o.creds.OAuth != nil {
		creds.OAuth = &config.OAuthCreds{
			AccessToken:  o.creds.OAuth.AccessToken,
			RefreshToken: o.creds.OAuth.RefreshToken,
			ExpiresAt:    o.creds.OAuth.ExpiresAt,
		}
	}
	return &creds
}

func (o *OpenAIProvider) getCurrentToken() string {
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

func generatePKCEVerifier() string {
	// RFC 7636: HIGH-ALPHA / LOW-ALPHA / DIGIT / "-" / "." / "_" / "~"
	// Allowed chars: A-Z a-z 0-9 - . _ ~
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

// CodeVerifier returns the PKCE code verifier generated during StartOAuth.
// It is needed by the caller to pass it to FinishOAuth on the same instance.
func (o *OpenAIProvider) CodeVerifier() string {
	return o.codeVerifier
}

// SetCodeVerifier sets the PKCE code verifier directly. Used when the verifier
// was generated in a previous step and needs to be restored on a new instance.
func (o *OpenAIProvider) SetCodeVerifier(v string) {
	o.codeVerifier = v
}

// SetDeviceState restores device auth state so a new instance can continue polling.
func (o *OpenAIProvider) SetDeviceState(deviceAuthID, userCode string) {
	o.deviceAuthID = deviceAuthID
	o.userCode = userCode
	o.pollInterval = 5
}

// GetDeviceAuthID returns the device auth ID for storing across wizard steps.
func (o *OpenAIProvider) GetDeviceAuthID() string {
	return o.deviceAuthID
}

// State returns the OAuth state parameter generated during StartOAuth.
func (o *OpenAIProvider) State() string {
	return o.state
}
