package chat

import (
	"fmt"

	"squid-os/internal/chat/provider"
	"squid-os/internal/config"
)

// ProviderImpl is an alias for the provider package's interface.
type ProviderImpl = provider.ProviderImpl

// LoadProviderImpl creates a ProviderImpl from a provider name and user settings.
// Uses the factory function registered in the provider's meta.
// Returns nil if the dialect is not supported or no factory is registered.
func LoadProviderImpl(settings config.ProviderSettings) ProviderImpl {
	meta := provider.GetMeta(settings.Name)

	// Unsupported dialect
	if meta.Dialect != config.DialectOpenAICompatible && meta.Dialect != config.DialectOpenAICodex {
		return nil
	}

	// Use the factory from meta
	if meta.New != nil {
		return meta.New(settings.Credentials)
	}
	return nil
}

// ResolveChatURL returns the chat completions URL for a provider.
// Uses hardcoded URL from meta for known providers, or BaseURL from settings for custom.
// Deprecated: Engine now uses the adapter to determine the URL directly.
// Kept for backward compatibility in places that don't use Engine.
func ResolveChatURL(settings config.ProviderSettings) string {
	meta := provider.GetMeta(settings.Name)
	if meta.ChatURL != "" {
		return meta.ChatURL
	}
	if meta.Dialect == config.DialectAnthropic {
		return settings.BaseURL + "/v1/messages"
	}
	return settings.BaseURL + "/v1/chat/completions"
}

// ResolveModelsURL returns the models listing URL for a provider.
func ResolveModelsURL(settings config.ProviderSettings) string {
	meta := provider.GetMeta(settings.Name)
	if meta.ModelsURL != "" {
		return meta.ModelsURL
	}
	if meta.Dialect == config.DialectOpenAICompatible {
		return settings.BaseURL + "/v1/models"
	}
	return ""
}

// NeedsURL returns true if the provider requires a user-provided base URL (not a known provider).
func NeedsURL(settings config.ProviderSettings) bool {
	meta := provider.GetMeta(settings.Name)
	return meta.ChatURL == ""
}

// DefaultBaseURL returns the default base URL from provider meta.
// Returns empty string if the provider is a known provider with a hardcoded ChatURL.
func DefaultBaseURL(name string) string {
	return provider.GetMeta(name).DefaultBaseURL
}

// IsConfigured checks if user settings have valid credentials.
// Looks up supported auth methods from provider meta.
func IsConfigured(settings config.ProviderSettings) bool {
	meta := provider.GetMeta(settings.Name)

	if settings.Credentials == nil {
		return false
	}

	if len(meta.SupportedAuth) == 0 {
		// No auth needed — but may still need a URL
		return NeedsURL(settings) == false || settings.BaseURL != ""
	}

	switch settings.Credentials.ActiveAuthMethod {
	case config.AuthNone:
		return NeedsURL(settings) == false || settings.BaseURL != ""
	case config.AuthAPIKey:
		return settings.Credentials.APIKey != ""
	case config.AuthOAuth:
		return settings.Credentials.OAuth != nil && settings.Credentials.OAuth.AccessToken != ""
	default:
		return false
	}
}

// GetSupportedAuth returns the supported auth methods for a provider.
func GetSupportedAuth(name string) []config.AuthMethod {
	return provider.GetMeta(name).SupportedAuth
}

// GetProviderMeta returns the metadata for a provider.
func GetProviderMeta(name string) provider.ProviderMeta {
	return provider.GetMeta(name)
}

// AllProviderMeta returns all registered provider metadata.
func AllProviderMeta() []provider.ProviderMeta {
	return provider.AllMeta()
}

// IsKnownProvider returns true if a provider with this name is registered.
func IsKnownProvider(name string) bool {
	return provider.IsKnownProvider(name)
}

// UnsupportedDialectError returns a clear error for unsupported provider dialects.
func UnsupportedDialectError(dialect config.Dialect) error {
	return fmt.Errorf("provider dialect %q not supported — only openai-compatible providers are currently available", dialect)
}
