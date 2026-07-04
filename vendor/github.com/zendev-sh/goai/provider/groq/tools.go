package groq

import "github.com/zendev-sh/goai/provider"

// Tools provides factory functions for Groq provider-defined tools.
// Matches Vercel AI SDK's groq.tools.
var Tools = struct {
	// BrowserSearch creates a browser search tool definition.
	// Provides interactive browser search capabilities that go beyond traditional
	// web search by navigating websites interactively and providing detailed results.
	// Supported on: openai/gpt-oss-20b, openai/gpt-oss-120b.
	BrowserSearch func() provider.ToolDefinition
}{
	BrowserSearch: browserSearchTool,
}

func browserSearchTool() provider.ToolDefinition {
	return provider.ToolDefinition{
		Name:                "browser_search",
		ProviderDefinedType: "browser_search",
	}
}
