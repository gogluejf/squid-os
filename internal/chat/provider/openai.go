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
	openaiClientID   = "app_EMoamEEZ73f0CkXaXp7hrann"
	openaiAuthURL    = "https://auth.openai.com/oauth/authorize"
	openaiTokenURL   = "https://auth.openai.com/oauth/token"
)

func init() {
	RegisterMeta(ProviderMeta{
		Name:          config.ProviderOpenAI,
		ChatURL:       "https://api.openai.com/v1/chat/completions",
		ModelsURL:     "https://api.openai.com/v1/models",
		Dialect:       config.DialectOpenAICompatible,
		SupportedAuth: []config.AuthMethod{config.AuthAPIKey, config.AuthOAuth},
	})
	RegisterMeta(ProviderMeta{
		Name:          config.ProviderOpenAICodex,
		ChatURL:       "https://api.openai.com/v1/chat/completions",
		ModelsURL:     "https://api.openai.com/v1/models",
		Dialect:       config.DialectOpenAICompatible,
		SupportedAuth: []config.AuthMethod{config.AuthAPIKey, config.AuthOAuth},
	})
}

// OpenAIProvider implements ProviderImpl for OpenAI authentication.
type OpenAIProvider struct {
	codeVerifier string
	creds        *config.ProviderCreds
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

	params := url.Values{
		"client_id":             {openaiClientID},
		"response_type":         {"code"},
		"code_challenge":        {challenge},
		"code_challenge_method": {"S256"},
		"redirect_uri":          {redirectURI},
	}

	return openaiAuthURL + "?" + params.Encode(), nil
}

// FinishOAuth exchanges the authorization code for tokens.
func (o *OpenAIProvider) FinishOAuth(code string) error {
	if o.codeVerifier == "" {
		return fmt.Errorf("openai: no PKCE code verifier — call StartOAuth first")
	}

	data := url.Values{
		"grant_type":    {"authorization_code"},
		"client_id":     {openaiClientID},
		"code":          {code},
		"code_verifier": {o.codeVerifier},
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

// PrepareRequest injects the Authorization header.
func (o *OpenAIProvider) PrepareRequest(req *http.Request) error {
	token := o.getCurrentToken()
	if token == "" {
		return fmt.Errorf("openai: no credentials configured")
	}
	req.Header.Set("Authorization", "Bearer "+token)
	return nil
}

// GetAccessToken returns the current access token for logging/debugging.
func (o *OpenAIProvider) GetAccessToken() string {
	return o.getCurrentToken()
}

// NeedsAuth returns true if this provider requires authentication.
func (o *OpenAIProvider) NeedsAuth() bool {
	return true
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
	b := make([]byte, 32)
	rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

func generateCodeChallenge(verifier string) string {
	h := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(h[:])
}
