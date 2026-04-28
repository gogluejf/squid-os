package tools

// Tool defines the contract for a callable tool.
// Each tool has a JSON Schema definition (for the LLM) and an Execute function.
type Tool struct {
	Name        string
	Description string
	Schema      map[string]interface{}
	Execute     func(args map[string]interface{}) (string, error)
}

// GetTools returns all available tools.
func GetTools() []Tool {
	return []Tool{
		ReadFile,
		WriteFile,
		EditFile,
		Bash,
	}
}
