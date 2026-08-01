package tools

import (
	"fmt"
	"strings"

	"squid-os/internal/config"
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
	Execute: func(args map[string]interface{}, cfg config.SessionConfig) ToolResult {
		name, ok := args["name"].(string)
		if !ok || name == "" {
			return ToolResult{Status: ResultStatusError, Error: "name is required and must be a string"}
		}
		if !contains(cfg.Skills, name) {
			return ToolResult{Status: ResultStatusError, Error: fmt.Sprintf("skill %q is not available. Run skill_list to see available skills.", name)}
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
	Execute: func(_ map[string]interface{}, cfg config.SessionConfig) ToolResult {
		reg := skills.GetRegistry()
		if reg == nil {
			return ToolResult{Status: ResultStatusError, Error: "skill registry not initialized"}
		}
		allowed := make(map[string]bool, len(cfg.Skills))
		for _, name := range cfg.Skills {
			allowed[name] = true
		}
		var entries []skills.SkillEntry
		for _, entry := range reg.List() {
			if allowed[entry.Name] {
				entries = append(entries, entry)
			}
		}
		if len(entries) == 0 {
			return ToolResult{Status: ResultStatusSuccess, Result: "No skills available in this session."}
		}
		var b strings.Builder
		b.WriteString(fmt.Sprintf("Available skills (%d):\n", len(entries)))
		for _, e := range entries {
			b.WriteString(fmt.Sprintf("  - %s: %s\n", e.Name, e.Description))
		}
		return ToolResult{Status: ResultStatusSuccess, Result: b.String()}
	},
}
