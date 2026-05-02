package tools

import (
	"encoding/json"
	"fmt"
)

// Tool execution result
const (
	ResultStatusSuccess = "success"
	ResultStatusError   = "error"
)

// ToolResult is returned by Execute instead of (string, error).
type ToolResult struct {
	Status string // ResultStatusSuccess or ResultStatusError
	Result string // output on success
	Error  string // error message on failure
}

// Tool defines the contract for a callable tool.
// Each tool has a JSON Schema definition (for the LLM) and an Execute function.
// Schema is stored as pre-serialized JSON bytes to control key ordering.
type Tool struct {
	Name        string
	Description string
	Schema      []byte
	Execute     func(args map[string]interface{}) ToolResult
}

var allTools []Tool

func init() {
	allTools = []Tool{
		ReadFile,
		WriteFile,
		EditFile,
		Bash,
		Open,
	}
	if err := validateSchemas(allTools); err != nil {
		panic(err)
	}
}

func validateSchemas(tools []Tool) error {
	for _, t := range tools {
		if !json.Valid(t.Schema) {
			return fmt.Errorf("invalid JSON schema for tool %q", t.Name)
		}
	}
	return nil
}

// GetTools returns a copy of all available tools.
func GetTools() []Tool {
	cp := make([]Tool, len(allTools))
	copy(cp, allTools)
	return cp
}
