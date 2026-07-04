package xai

import "github.com/zendev-sh/goai/provider"

// Tools provides factory functions for xAI (Grok) provider-defined tools.
// Matches Vercel AI SDK's xai.tools.
var Tools = struct {
	// WebSearch creates a web search tool definition.
	// Allows models to search the web for up-to-date information.
	WebSearch func(opts ...WebSearchOption) provider.ToolDefinition

	// XSearch creates an X (Twitter) search tool definition.
	// Allows models to search posts on X for real-time social content.
	XSearch func(opts ...XSearchOption) provider.ToolDefinition
}{
	WebSearch: webSearchTool,
	XSearch:   xSearchTool,
}

// ---------------------------------------------------------------------------
// WebSearch
// ---------------------------------------------------------------------------

// WebSearchOption configures the xAI web search tool.
type WebSearchOption func(*webSearchConfig)

type webSearchConfig struct {
	AllowedDomains           []string // max 5
	ExcludedDomains          []string // max 5
	EnableImageUnderstanding bool
}

// WithAllowedDomains restricts web search to these domains (max 5).
func WithAllowedDomains(domains ...string) WebSearchOption {
	return func(c *webSearchConfig) { c.AllowedDomains = domains }
}

// WithExcludedDomains excludes these domains from web search results (max 5).
func WithExcludedDomains(domains ...string) WebSearchOption {
	return func(c *webSearchConfig) { c.ExcludedDomains = domains }
}

// WithWebSearchImageUnderstanding enables image understanding in web search results.
func WithWebSearchImageUnderstanding(enabled bool) WebSearchOption {
	return func(c *webSearchConfig) { c.EnableImageUnderstanding = enabled }
}

func webSearchTool(opts ...WebSearchOption) provider.ToolDefinition {
	cfg := &webSearchConfig{}
	for _, o := range opts {
		o(cfg)
	}

	providerOpts := map[string]any{}
	if len(cfg.AllowedDomains) > 0 {
		providerOpts["allowed_domains"] = cfg.AllowedDomains
	}
	if len(cfg.ExcludedDomains) > 0 {
		providerOpts["excluded_domains"] = cfg.ExcludedDomains
	}
	if cfg.EnableImageUnderstanding {
		providerOpts["enable_image_understanding"] = true
	}

	return provider.ToolDefinition{
		Name:                   "web_search",
		ProviderDefinedType:    "web_search",
		ProviderDefinedOptions: providerOpts,
	}
}

// ---------------------------------------------------------------------------
// XSearch
// ---------------------------------------------------------------------------

// XSearchOption configures the xAI X (Twitter) search tool.
type XSearchOption func(*xSearchConfig)

type xSearchConfig struct {
	AllowedXHandles          []string // max 10
	ExcludedXHandles         []string // max 10
	FromDate                 string   // ISO 8601 date
	ToDate                   string   // ISO 8601 date
	EnableImageUnderstanding bool
	EnableVideoUnderstanding bool
}

// WithAllowedXHandles restricts X search to posts from these handles (max 10).
func WithAllowedXHandles(handles ...string) XSearchOption {
	return func(c *xSearchConfig) { c.AllowedXHandles = handles }
}

// WithExcludedXHandles excludes posts from these handles (max 10).
func WithExcludedXHandles(handles ...string) XSearchOption {
	return func(c *xSearchConfig) { c.ExcludedXHandles = handles }
}

// WithXSearchDateRange restricts X search to posts within a date range (ISO 8601).
func WithXSearchDateRange(from, to string) XSearchOption {
	return func(c *xSearchConfig) {
		c.FromDate = from
		c.ToDate = to
	}
}

// WithXSearchImageUnderstanding enables image understanding in X search results.
func WithXSearchImageUnderstanding(enabled bool) XSearchOption {
	return func(c *xSearchConfig) { c.EnableImageUnderstanding = enabled }
}

// WithXSearchVideoUnderstanding enables video understanding in X search results.
func WithXSearchVideoUnderstanding(enabled bool) XSearchOption {
	return func(c *xSearchConfig) { c.EnableVideoUnderstanding = enabled }
}

func xSearchTool(opts ...XSearchOption) provider.ToolDefinition {
	cfg := &xSearchConfig{}
	for _, o := range opts {
		o(cfg)
	}

	providerOpts := map[string]any{}
	if len(cfg.AllowedXHandles) > 0 {
		providerOpts["allowed_x_handles"] = cfg.AllowedXHandles
	}
	if len(cfg.ExcludedXHandles) > 0 {
		providerOpts["excluded_x_handles"] = cfg.ExcludedXHandles
	}
	if cfg.FromDate != "" {
		providerOpts["from_date"] = cfg.FromDate
	}
	if cfg.ToDate != "" {
		providerOpts["to_date"] = cfg.ToDate
	}
	if cfg.EnableImageUnderstanding {
		providerOpts["enable_image_understanding"] = true
	}
	if cfg.EnableVideoUnderstanding {
		providerOpts["enable_video_understanding"] = true
	}

	return provider.ToolDefinition{
		Name:                   "x_search",
		ProviderDefinedType:    "x_search",
		ProviderDefinedOptions: providerOpts,
	}
}
