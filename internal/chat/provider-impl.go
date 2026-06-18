package chat

import (
	"fmt"

	"squid-os/internal/chat/provider"
	"squid-os/internal/config"
)

// ProviderImpl is an alias for the provider package's interface.
type ProviderImpl = provider.Provider

// LoadProviderImpl creates a ProviderImpl from a provider name and user settings.
// Returns nil if the provider is not registered or has an unsupported dialect.
func LoadProviderImpl(settings config.ProviderSettings) ProviderImpl {
	p := provider.Lookup(settings.Name, settings.Credentials)
	if p == nil {
		return nil
	}

	// Unsupported dialect
	d := p.Dialect()
	if d != config.DialectOpenAICompatible && d != config.DialectOpenAICodex {
		return nil
	}

	return p
}

// NeedsURL returns true if the provider requires a user-provided base URL (not a known provider).
func NeedsURL(settings config.ProviderSettings) bool {
	p := provider.Lookup(settings.Name, nil)
	if p == nil {
		return true
	}
	return p.DefaultBaseURL() == ""
}

// DefaultBaseURL returns the default base URL from provider meta.
func DefaultBaseURL(name string) string {
	p := provider.Lookup(name, nil)
	if p == nil {
		return ""
	}
	return p.DefaultBaseURL()
}

// IsConfigured checks if user settings have valid credentials.
func IsConfigured(settings config.ProviderSettings) bool {
	p := provider.Lookup(settings.Name, nil)
	if p == nil || len(p.SupportedAuth()) == 0 {
		return NeedsURL(settings) == false || settings.BaseURL != ""
	}

	if settings.Credentials == nil {
		return false
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

// GetProviderMeta returns a provider instance for reading metadata.
func GetProviderMeta(name string) provider.Provider {
	return provider.Lookup(name, nil)
}

// UnsupportedDialectError returns a clear error for unsupported provider dialects.
func UnsupportedDialectError(dialect config.Dialect) error {
	return fmt.Errorf("provider dialect %q not supported — only openai-compatible providers are currently available", dialect)
}
