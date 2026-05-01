package tools

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
type Tool struct {
	Name        string
	Description string
	Schema      map[string]interface{}
	Execute     func(args map[string]interface{}) ToolResult
}

// GetTools returns all available tools.
func GetTools() []Tool {
	return []Tool{
		ReadFile,
		WriteFile,
		EditFile,
		Bash,
		Open,
	}
}
