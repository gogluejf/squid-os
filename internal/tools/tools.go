package tools

import (
	"encoding/json"
	"fmt"

	"squid-os/internal/agent"
	"squid-os/internal/config"
	"squid-os/internal/skills"
	"squid-os/internal/style"
)

// Tool execution result
const (
	ResultStatusSuccess = "success"
	ResultStatusError   = "error"
	ResultStatusPending = "pending" // awaiting user authorization
)

// ToolResult is returned by Execute instead of (string, error).
type ToolResult struct {
	Status string             // ResultStatusSuccess or ResultStatusError
	Result string             // output on success
	Error  string             // error message on failure
	Files  []config.FileEntry // files touched by this tool
}

type RuntimeContext struct {
	Config config.SessionConfig
	Skills *skills.Registry
	Agents *agent.Registry
}

// Tool defines the contract for a callable tool.
type Tool struct {
	Name         string
	Description  string
	DisplayParam string
	Style        style.StyleLabel
	Schema       []byte
	Execute      func(args map[string]interface{}, ctx RuntimeContext) ToolResult
	Preview      func(args map[string]interface{}, ctx RuntimeContext) ToolResult
	// IsDestructive is optional. If present, it returns true if the tool call modifies
	// disk state, makes network calls, or otherwise has security implications.
	// nil means the tool is never destructive. Used by the authorization
	// gate to determine whether user confirmation is needed.
	IsDestructive func(args map[string]interface{}) bool
}

// Registry holds tools by name for O(1) lookup.
type Registry struct {
	tools []Tool
	index map[string]*Tool
}

var registry *Registry

func init() {
	list := []Tool{
		ReadFile,
		WriteFile,
		EditFile,
		Bash,
		Open,
		SkillLoad,
		SkillList,
		ListAgents,
		CallAgent,
		InlineAgent,
		SetWorkingDirTool,
	}
	for i := range list {
		if err := validateSchema(list[i]); err != nil {
			panic(err)
		}
	}
	registry = newRegistry(list)
}

func newRegistry(tools []Tool) *Registry {
	r := &Registry{
		tools: make([]Tool, len(tools)),
		index: make(map[string]*Tool, len(tools)),
	}
	copy(r.tools, tools)
	for i := range r.tools {
		r.index[r.tools[i].Name] = &r.tools[i]
	}
	return r
}

func validateSchema(t Tool) error {
	if !json.Valid(t.Schema) {
		return fmt.Errorf("invalid JSON schema for tool %q", t.Name)
	}
	return nil
}

// GetRegistry returns the global tool registry.
func GetRegistry() *Registry {
	return registry
}

// GetTools returns a copy of all available tools.
func GetTools() []Tool {
	if registry == nil {
		return nil
	}
	cp := make([]Tool, len(registry.tools))
	copy(cp, registry.tools)
	return cp
}

// Get looks up a tool by name. Returns nil if not found.
func (r *Registry) Get(name string) *Tool {
	if r == nil {
		return nil
	}
	return r.index[name]
}

// List returns a copy of all tools.
func (r *Registry) List() []Tool {
	if r == nil {
		return nil
	}
	cp := make([]Tool, len(r.tools))
	copy(cp, r.tools)
	return cp
}

// DisplayValue extracts the display-friendly value from args JSON string using
// the tool's DisplayParam. Returns "" if the param isn't set or not found in args.
func (t *Tool) DisplayValue(argsJSON string) string {
	if t == nil || t.DisplayParam == "" || argsJSON == "" {
		return ""
	}
	var args map[string]interface{}
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return ""
	}
	val, ok := args[t.DisplayParam]
	if !ok {
		return ""
	}
	switch v := val.(type) {
	case string:
		return v
	case float64:
		return fmt.Sprintf("%g", v)
	case bool:
		return fmt.Sprintf("%v", v)
	default:
		return ""
	}
}
