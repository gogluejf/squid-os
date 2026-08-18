package tools

import (
	"context"
	"fmt"
	"strings"

	"squid-os/internal/media"
	"squid-os/internal/style"
)

// InspectMediaTool loads a media file (local path, URL, or session attachment)
// through the shared attachment workspace and returns the canonical @file:<id>
// reference. The context builder generates a synthetic user multimodal message
// at API-build time — the tool itself does not call any model.
var InspectMediaTool = Tool{
	Name:        "inspect_media",
	Description: "Load a media file (image, PDF, document) so the model can inspect it. Accepts local file paths or HTTP/HTTPS URLs. Use only when the media is not already available in context — do not call this for files the user has just attached.",
	DisplayParams: []string{"path_or_url", "query"},
	Style:         style.ToolStyle(),
	Schema: []byte(`{
	"type": "object",
	"properties": {
		"path_or_url": {
			"type": "string",
			"description": "Path to a local file or HTTP/HTTPS URL"
		},
		"query": {
			"type": "string",
			"description": "Natural-language query describing what to extract or analyze from the media (e.g., 'Describe the contents', 'What text is visible?')"
		}
	},
	"required": ["path_or_url", "query"]
}`),
	Execute: func(args map[string]interface{}, rt RuntimeContext) ToolResult {
		pathOrURL, ok := args["path_or_url"].(string)
		if !ok || pathOrURL == "" {
			return ToolResult{Status: ResultStatusError, Error: "path_or_url is required and must be a string"}
		}

		query, ok := args["query"].(string)
		if !ok || query == "" {
			return ToolResult{Status: ResultStatusError, Error: "query is required and must be a string"}
		}

		// Case 1: Already a session attachment reference — just validate it exists.
		if strings.HasPrefix(pathOrURL, "@file:") {
			return ToolResult{
				Status: ResultStatusSuccess,
				Result: pathOrURL,
			}
		}

		// Case 2: HTTP/HTTPS URL — ingest through the workspace.
		if strings.HasPrefix(pathOrURL, "http://") || strings.HasPrefix(pathOrURL, "https://") {
			return ingestMedia(rt, media.IngestSource{
				Kind: media.IngestSourceKindURL,
				URL:  pathOrURL,
			})
		}

		// Case 3: Local file path — resolve and ingest.
		resolvedPath := ResolvePath(pathOrURL, rt.Config.WorkingDir)
		return ingestMedia(rt, media.IngestSource{
			Kind: media.IngestSourceKindFile,
			Path: resolvedPath,
		})
	},
	IsDestructive: func(args map[string]interface{}) bool {
		pathOrURL, ok := args["path_or_url"].(string)
		if !ok {
			return false
		}
		return strings.HasPrefix(pathOrURL, "http://") || strings.HasPrefix(pathOrURL, "https://")
	},
}

func ingestMedia(rt RuntimeContext, source media.IngestSource) ToolResult {
	if rt.IngestService == nil {
		return ToolResult{Status: ResultStatusError, Error: "workspace not available for media ingestion"}
	}

	attach, err := rt.IngestService.Ingest(context.Background(), source)
	if err != nil {
		return ToolResult{Status: ResultStatusError, Error: fmt.Sprintf("ingest media: %v", err)}
	}

	return ToolResult{
		Status: ResultStatusSuccess,
		Result: attach.CanonicalRef(),
	}
}
