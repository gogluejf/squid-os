package tools

import (
	"fmt"
	"strings"

	"squid-os/internal/config"
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
	Execute: func(args map[string]interface{}, rt RuntimeContext) ToolResult {
		name, ok := args["name"].(string)
		if !ok || name == "" {
			return ToolResult{Status: ResultStatusError, Error: "name is required and must be a string"}
		}
		ref, ok := findCapability(rt.Config.Skills, name)
		if !ok {
			return ToolResult{Status: ResultStatusError, Error: fmt.Sprintf("skill %q is not available. Run skill_list to see available skills.", name)}
		}
		if rt.Catalog == nil {
			return ToolResult{Status: ResultStatusError, Error: "skill catalog not initialized"}
		}
		sk, err := rt.Catalog.LoadSkill(ref.Scope, ref.Name)
		if err != nil {
			return ToolResult{Status: ResultStatusError, Error: err.Error()}
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
	Execute: func(_ map[string]interface{}, rt RuntimeContext) ToolResult {
		if len(rt.Config.Skills) == 0 {
			return ToolResult{Status: ResultStatusSuccess, Result: "No skills available in this session."}
		}
		catalog := rt.Catalog
		var b strings.Builder
		b.WriteString(fmt.Sprintf("Available skills (%d):\n", len(rt.Config.Skills)))
		for _, ref := range rt.Config.Skills {
			if catalog != nil {
				entry, ok := catalog.ResolveSkill(ref.Name)
				if ok && entry.Scope == ref.Scope {
					b.WriteString(fmt.Sprintf("  - %s: [%s] %s\n", ref.Name, ref.Scope, entry.Description))
					continue
				}
			}
			b.WriteString(fmt.Sprintf("  - %s: [%s] (unavailable)\n", ref.Name, ref.Scope))
		}
		return ToolResult{Status: ResultStatusSuccess, Result: b.String()}
	},
}

func findCapability(refs []config.CapabilityRef, name string) (config.CapabilityRef, bool) {
	for _, ref := range refs {
		if ref.Name == name {
			return ref, true
		}
	}
	return config.CapabilityRef{}, false
}
