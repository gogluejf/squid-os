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
