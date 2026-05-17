package tools

import (
	"encoding/json"
	"fmt"
	"strings"

	"squid-os/internal/skills"
	"squid-os/internal/style"
)

// SkillLoad loads the full SKILL.md content for the named skill and injects it into context.
var SkillLoad = Tool{
	Name:         "skill_load",
	Description:  "Load a skill by name and inject its instructions into context. Returns the skill's full instructions.",
	DisplayParam: "name",
	Style:        style.SkillStyle(),
	Schema: []byte(`{
	"type": "object",
	"properties": {
		"name": {
			"type": "string",
			"description": "Skill name (must exist in the registry)"
		}
	},
	"required": ["name"]
}`),
	Execute: func(args map[string]interface{}) ToolResult {
		name, ok := args["name"].(string)
		if !ok || name == "" {
			return ToolResult{Status: ResultStatusError, Error: "name is required and must be a string"}
		}
		reg := skills.GetRegistry()
		if reg == nil {
			return ToolResult{Status: ResultStatusError, Error: "skill registry not initialized"}
		}
		sk, err := reg.Load(name)
		if err != nil {
			return ToolResult{Status: ResultStatusError, Error: fmt.Sprintf("skill %q not found", name)}
		}
		var b strings.Builder
		b.WriteString(fmt.Sprintf("═══ SKILL: %s ═══\n", sk.Name))
		if sk.Body != "" {
			b.WriteString(sk.Body)
		} else {
			b.WriteString("(No instructions in this skill)\n")
		}
		if sk.ScriptsDir != "" {
			b.WriteString(fmt.Sprintf("\n[Scripts: %s]\n", sk.ScriptsDir))
		}
		if sk.AssetsDir != "" {
			b.WriteString(fmt.Sprintf("[Assets: %s]\n", sk.AssetsDir))
		}
		if sk.RefsDir != "" {
			b.WriteString(fmt.Sprintf("[References: %s]\n", sk.RefsDir))
		}
		b.WriteString("═══════════════════\n")
		return ToolResult{Status: ResultStatusSuccess, Result: b.String()}
	},
}

// SkillList returns a list of all available skills with name and description.
var SkillList = Tool{
	Name:         "skill_list",
	Description:  "Return a list of all available skills with name and description. Lightweight, always available.",
	DisplayParam: "",
	Style:        style.SkillStyle(),
	Schema:       []byte(`{"type": "object", "properties": {}}`),
	Execute: func(args map[string]interface{}) ToolResult {
		reg := skills.GetRegistry()
		if reg == nil {
			return ToolResult{Status: ResultStatusError, Error: "skill registry not initialized"}
		}
		entries := reg.List()
		if len(entries) == 0 {
			return ToolResult{Status: ResultStatusSuccess, Result: "No skills installed."}
		}
		var b strings.Builder
		b.WriteString(fmt.Sprintf("Available skills (%d):\n", len(entries)))
		for _, e := range entries {
			b.WriteString(fmt.Sprintf("  - %s: %s\n", e.Name, e.Description))
		}
		return ToolResult{Status: ResultStatusSuccess, Result: b.String()}
	},
}

// skillBuildArgs holds the parsed JSON args for skill_build.
type skillBuildArgs struct {
	Name         string            `json:"name"`
	Description  string            `json:"description"`
	Version      string            `json:"version,omitempty"`
	License      string            `json:"license,omitempty"`
	AllowedTools string            `json:"allowed_tools,omitempty"`
	Metadata     map[string]string `json:"metadata,omitempty"`
	Overview     string            `json:"overview"`
	Instructions string            `json:"instructions"`
	Rules        string            `json:"rules,omitempty"`
	OutputFormat string            `json:"output_format,omitempty"`
	Examples     string            `json:"examples,omitempty"`
	References   map[string]string `json:"references,omitempty"`
	Assets       map[string]string `json:"assets,omitempty"`
	Scripts      map[string]string `json:"scripts,omitempty"`
}

// SkillBuild generates a new skill with the proper folder structure.
var SkillBuild = Tool{
	Name:         "skill_build",
	Description:  "Generate a new skill with the proper folder structure. Validates the contract and creates files.",
	DisplayParam: "name",
	Style:        style.SkillStyle(),
	Schema: []byte(`{
	"type": "object",
	"properties": {
		"name": {
			"type": "string",
			"description": "Skill name (lowercase, hyphens only, 1-64 chars)"
		},
		"description": {
			"type": "string",
			"description": "What it does + when to invoke (max 1024 chars)"
		},
		"version": {
			"type": "string",
			"description": "Optional semantic version"
		},
		"license": {
			"type": "string",
			"description": "Optional license string"
		},
		"allowed_tools": {
			"type": "string",
			"description": "Space-separated tool names the skill is authorized to call (e.g., 'bash read_file write_file'). Stored as-is in frontmatter. The agent can only use these tools while the skill is active."
		},
		"overview": {
			"type": "string",
			"description": "One-paragraph summary of what the skill does and why it exists."
		},
		"instructions": {
			"type": "string",
			"description": "Step-by-step instructions the agent follows when this skill is loaded. Can reference scripts via 'scripts/filename' to execute code during the workflow."
		},
		"rules": {
			"type": "string",
			"description": "Do/never/always constraints that govern how this skill should be used."
		},
		"output_format": {
			"type": "string",
			"description": "Expected output structure of the skill. Can reference asset templates via 'assets/filename' to define the output shape (e.g., markdown template for generated files)."
		},
		"examples": {
			"type": "string",
			"description": "Input/output examples demonstrating how the skill should be used in practice."
		},
		"references": {
			"type": "object",
			"additionalProperties": {"type": "string"},
			"description": "Documentation files (0 to N) as filename->content pairs. Written to references/. These are supplementary docs the skill may need (API guides, architecture notes, etc.)."
		},
		"assets": {
			"type": "object",
			"additionalProperties": {"type": "string"},
			"description": "Template or resource files (0 to N) as filename->content pairs. Written to assets/. Use these for output templates that the skill's output_format will reference to generate structured output."
		},
		"scripts": {
			"type": "object",
			"additionalProperties": {"type": "string"},
			"description": "Executable scripts (0 to N) as filename->content pairs. Written to scripts/ with +x permission. These are executable code that the skill's instructions can call during workflow (e.g., bash scripts, python scripts)."
		}
	},
	"required": ["name", "description", "overview", "instructions"]
}`),
	Execute: func(args map[string]interface{}) ToolResult {
		jsonBytes, err := json.Marshal(args)
		if err != nil {
			return ToolResult{Status: ResultStatusError, Error: "invalid arguments: " + err.Error()}
		}
		var a skillBuildArgs
		if err := json.Unmarshal(jsonBytes, &a); err != nil {
			return ToolResult{Status: ResultStatusError, Error: "parse arguments: " + err.Error()}
		}

		params := skills.BuildParams{
			Name:         a.Name,
			Description:  a.Description,
			Version:      a.Version,
			License:      a.License,
			AllowedTools: a.AllowedTools,
			Metadata:     a.Metadata,
			Overview:     a.Overview,
			Instructions: a.Instructions,
			Rules:        a.Rules,
			OutputFormat: a.OutputFormat,
			Examples:     a.Examples,
			References:   a.References,
			Assets:       a.Assets,
			Scripts:      a.Scripts,
		}

		reg := skills.GetRegistry()
		if reg == nil {
			return ToolResult{Status: ResultStatusError, Error: "skill registry not initialized"}
		}

		sk, err := reg.Build(params)
		if err != nil {
			return ToolResult{Status: ResultStatusError, Error: err.Error()}
		}

		return ToolResult{
			Status: ResultStatusSuccess,
			Result: fmt.Sprintf("Created skill %q at %s", sk.Name, sk.Path),
		}
	},
}
