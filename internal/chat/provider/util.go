package provider

import (
	"encoding/base64"
	"encoding/json"
	"strings"
)

// extractChatGPTAccountID decodes the JWT payload and extracts the ChatGPT account ID.
func extractChatGPTAccountID(token string) string {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return ""
	}
	payload, err := decodeBase64URL(parts[1])
	if err != nil {
		return ""
	}
	var jwts struct {
		Auth struct {
			ChatGPTAccountID string `json:"chatgpt_account_id"`
		} `json:"https://api.openai.com/auth"`
	}
	if err := json.Unmarshal(payload, &jwts); err != nil {
		return ""
	}
	return jwts.Auth.ChatGPTAccountID
}

func decodeBase64URL(s string) ([]byte, error) {
	switch len(s) % 4 {
	case 2:
		s += "=="
	case 3:
		s += "="
	}
	return base64.RawURLEncoding.DecodeString(s)
}

// normalizeOpenAICompatBaseURL ensures OpenAI-compatible providers receive the
// API root expected by GoAI's compat/openai-compatible transports.
//
// GoAI compat/vllm providers append "/chat/completions" internally, so the
// base URL must already point at the API root, typically ending in "/v1".
func normalizeOpenAICompatBaseURL(raw string, fallback string) string {
	base := strings.TrimSpace(raw)
	if base == "" {
		base = fallback
	}
	base = strings.TrimRight(base, "/")
	if before, ok := strings.CutSuffix(base, "/chat/completions"); ok {
		base = before
	}
	if before, ok := strings.CutSuffix(base, "/models"); ok {
		base = before
	}
	if !strings.HasSuffix(base, "/v1") {
		base += "/v1"
	}
	return base
}
