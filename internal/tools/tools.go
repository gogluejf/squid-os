package tools

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"squid-os/internal/agent"
	"squid-os/internal/config"
	"squid-os/internal/media"
	"squid-os/internal/skills"
	"squid-os/internal/style"
)

// Tool execution result
const (
	ResultStatusSuccess = "success"
	ResultStatusError   = "error"
	ResultStatusPending = "pending" // awaiting user authorization or execution
	ResultStatusRunning = "running" // execution started; repaired as interrupted after restart
)

// ToolResult is returned by Execute instead of (string, error).
type ToolResult struct {
	Status string             // ResultStatusSuccess or ResultStatusError
	Result string             // output on success
	Error  string             // error message on failure
	Files  []config.FileEntry // files touched by this tool
}

type Catalog interface {
	ResolveSkill(string) (skills.SkillEntry, bool)
	ResolveAgent(string) (agent.Entry, bool)
	LoadSkill(config.CapabilityScope, string) (*skills.Skill, error)
	LoadAgent(config.CapabilityScope, string) (*agent.Definition, error)
}

type RuntimeContext struct {
	Config     config.SessionConfig
	Catalog    Catalog
	Identity   config.SessionIdentity
	SessionDir string
	ToolCallID string
	// ChildRef is the preallocated child session reference for agent delegation.
	// Empty for non-agent tools.
	ChildRef ChildSessionRef
	// IngestService handles media ingestion through the session's workspace.
	// May be nil for tools that don't need it.
	IngestService *media.IngestService
}

// ChildSessionRef holds the preallocated identity of a delegated child session.
type ChildSessionRef struct {
	ID   string
	Name string
}

// Tool defines the contract for a callable tool.
type Tool struct {
	Name          string
	Description   string
	DisplayParams []string
	Style         style.StyleLabel
	Schema        []byte
	Execute       func(args map[string]interface{}, ctx RuntimeContext) ToolResult
	Preview       func(args map[string]interface{}, ctx RuntimeContext) ToolResult
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
		InspectMediaTool,
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

// DisplayValue extracts display-friendly values from args JSON string using
// the tool's DisplayParams. Returns the values joined by " · ".
func (t *Tool) DisplayValue(argsJSON string) string {
	if t == nil || len(t.DisplayParams) == 0 || argsJSON == "" {
		return ""
	}
	var args map[string]interface{}
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return ""
	}
	var parts []string
	for _, key := range t.DisplayParams {
		val, ok := args[key]
		if !ok {
			continue
		}
		switch v := val.(type) {
		case string:
			if v != "" {
				parts = append(parts, v)
			}
		case float64:
			parts = append(parts, fmt.Sprintf("%g", v))
		case bool:
			parts = append(parts, fmt.Sprintf("%v", v))
		}
	}
	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, " · ")
}

// IsAgentTool returns true if the given tool name is an agent delegation tool.
func IsAgentTool(name string) bool {
	return name == "call_agent" || name == "inline_agent"
}

// GenerateChildSessionRef allocates a globally unique child identity and a
// human-readable directory name for an agent tool call.
func GenerateChildSessionRef(toolName, agentName, toolCallID string) ChildSessionRef {
	var childName string
	if toolName == "call_agent" {
		childName = fmt.Sprintf("%s-%s", agentName, toolCallID)
	} else {
		childName = fmt.Sprintf("inline-%s", toolCallID)
	}
	return ChildSessionRef{
		ID:   uuid.New().String(),
		Name: childName,
	}
}
