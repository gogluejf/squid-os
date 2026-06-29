package provider

import "strings"

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
