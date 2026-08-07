package runtime

import (
	"fmt"
	"path/filepath"
	"strings"

	"squid-os/internal/agent"
	"squid-os/internal/config"
	"squid-os/internal/skills"
)

// Catalog is the immutable global + workspace capability snapshot owned by a session.
type Catalog struct {
	WorkingDir string
	Skills     *skills.Registry
	Agents     *agent.Registry
}

// ResolvedCapabilities is derived live from policies and a catalog.
type ResolvedCapabilities struct {
	Skills        []config.CapabilityRef
	MissingSkills []string
	Agents        []config.CapabilityRef
	MissingAgents []string
}

func LoadCatalog(paths config.Paths, workingDir string) (Catalog, error) {
	workspaceRoot := filepath.Join(workingDir, ".squid-os")
	skillRegistry, err := skills.LoadRegistry(paths.Skills, filepath.Join(workspaceRoot, "skills"))
	if err != nil {
		return Catalog{}, fmt.Errorf("load skill catalog: %w", err)
	}
	agentRegistry, err := agent.LoadRegistry(paths.Agents, filepath.Join(workspaceRoot, "agents"))
	if err != nil {
		return Catalog{}, fmt.Errorf("load agent catalog: %w", err)
	}
	return Catalog{WorkingDir: workingDir, Skills: skillRegistry, Agents: agentRegistry}, nil
}

func (catalog Catalog) Resolve(skillPolicy, agentPolicy config.CapabilityPolicy) ResolvedCapabilities {
	skillRefs, missingSkills := resolveSkills(catalog.Skills, skillPolicy)
	agentRefs, missingAgents := resolveAgents(catalog.Agents, agentPolicy)
	return ResolvedCapabilities{
		Skills:        skillRefs,
		MissingSkills: missingSkills,
		Agents:        agentRefs,
		MissingAgents: missingAgents,
	}
}

func (catalog Catalog) ResolveSkill(name string) (skills.SkillEntry, bool) {
	if catalog.Skills == nil {
		return skills.SkillEntry{}, false
	}
	return catalog.Skills.Resolve(name)
}

func (catalog Catalog) ResolveAgent(name string) (agent.Entry, bool) {
	if catalog.Agents == nil {
		return agent.Entry{}, false
	}
	return catalog.Agents.Resolve(name)
}

func (catalog Catalog) LoadSkill(scope config.CapabilityScope, name string) (*skills.Skill, error) {
	if catalog.Skills == nil {
		return nil, fmt.Errorf("skill catalog not initialized")
	}
	return catalog.Skills.LoadScoped(scope, name)
}

func (catalog Catalog) LoadAgent(scope config.CapabilityScope, name string) (*agent.Definition, error) {
	if catalog.Agents == nil {
		return nil, fmt.Errorf("agent catalog not initialized")
	}
	return catalog.Agents.LoadScoped(scope, name)
}

func (catalog Catalog) SkillEntries(refs []config.CapabilityRef) []skills.SkillEntry {
	entries := make([]skills.SkillEntry, 0, len(refs))
	for _, ref := range refs {
		entry, ok := catalog.ResolveSkill(ref.Name)
		if ok && entry.Scope == ref.Scope {
			entries = append(entries, entry)
		}
	}
	return entries
}

func (catalog Catalog) AgentEntries(refs []config.CapabilityRef) []agent.Entry {
	entries := make([]agent.Entry, 0, len(refs))
	for _, ref := range refs {
		entry, ok := catalog.ResolveAgent(ref.Name)
		if ok && entry.Scope == ref.Scope {
			entries = append(entries, entry)
		}
	}
	return entries
}

func (catalog Catalog) MissingSkills(policy config.CapabilityPolicy) []string {
	_, missing := resolveSkills(catalog.Skills, policy)
	return missing
}

func (catalog Catalog) MissingAgents(policy config.CapabilityPolicy) []string {
	_, missing := resolveAgents(catalog.Agents, policy)
	return missing
}

func (catalog Catalog) FormatSkills(cfg config.SessionConfig) string {
	var b strings.Builder
	b.WriteString("### Available Skills\n")
	entries := catalog.SkillEntries(cfg.Skills)
	if len(entries) == 0 {
		b.WriteString("- none\n")
	} else {
		for _, entry := range entries {
			b.WriteString(fmt.Sprintf("  - %s: [%s] %s\n", entry.Name, entry.Scope, entry.Description))
		}
	}
	b.WriteString("\n### Missing Skills\n")
	writeMissing(&b, catalog.MissingSkills(cfg.SkillPolicy))
	return b.String()
}

func (catalog Catalog) FormatAgents(cfg config.SessionConfig) string {
	var b strings.Builder
	b.WriteString("### Available Agents\n")
	entries := catalog.AgentEntries(cfg.Agents)
	if len(entries) == 0 {
		b.WriteString("- none\n")
	} else {
		for _, entry := range entries {
			b.WriteString(fmt.Sprintf("  - %s: [%s] %s\n", entry.Name, entry.Scope, entry.Description))
		}
	}
	b.WriteString("\n### Missing Agents\n")
	writeMissing(&b, catalog.MissingAgents(cfg.AgentPolicy))
	return b.String()
}

func (catalog Catalog) FormatCapabilities(cfg config.SessionConfig) string {
	return "## [Skills]\n" + catalog.FormatSkills(cfg) + "\n## [Agents]\n" + catalog.FormatAgents(cfg)
}

func writeMissing(b *strings.Builder, names []string) {
	if len(names) == 0 {
		b.WriteString("- none\n")
		return
	}
	for _, name := range names {
		b.WriteString("- " + name + "\n")
	}
}

func resolveCatalog(catalog Catalog, skillPolicy, agentPolicy config.CapabilityPolicy, rejectMissing bool) ([]config.CapabilityRef, []config.CapabilityRef, error) {
	resolved := catalog.Resolve(skillPolicy, agentPolicy)
	if rejectMissing && len(resolved.MissingSkills) > 0 {
		return nil, nil, fmt.Errorf("skills not found: %s", strings.Join(resolved.MissingSkills, ", "))
	}
	if rejectMissing && len(resolved.MissingAgents) > 0 {
		return nil, nil, fmt.Errorf("agents not found: %s", strings.Join(resolved.MissingAgents, ", "))
	}
	return resolved.Skills, resolved.Agents, nil
}

func resolveSkills(registry *skills.Registry, policy config.CapabilityPolicy) ([]config.CapabilityRef, []string) {
	if registry == nil {
		return nil, append([]string(nil), policy.Requested...)
	}
	if policy.Mode == config.PolicyModeAll {
		entries := registry.List()
		refs := make([]config.CapabilityRef, 0, len(entries))
		for _, entry := range entries {
			refs = append(refs, config.CapabilityRef{Scope: entry.Scope, Name: entry.Name})
		}
		return refs, nil
	}
	return resolveRequested(policy.Requested, func(name string) (config.CapabilityRef, bool) {
		entry, ok := registry.Resolve(name)
		return config.CapabilityRef{Scope: entry.Scope, Name: entry.Name}, ok
	})
}

func resolveAgents(registry *agent.Registry, policy config.CapabilityPolicy) ([]config.CapabilityRef, []string) {
	if registry == nil {
		return nil, append([]string(nil), policy.Requested...)
	}
	if policy.Mode == config.PolicyModeAll {
		entries := registry.List()
		refs := make([]config.CapabilityRef, 0, len(entries))
		for _, entry := range entries {
			refs = append(refs, config.CapabilityRef{Scope: entry.Scope, Name: entry.Name})
		}
		return refs, nil
	}
	return resolveRequested(policy.Requested, func(name string) (config.CapabilityRef, bool) {
		entry, ok := registry.Resolve(name)
		return config.CapabilityRef{Scope: entry.Scope, Name: entry.Name}, ok
	})
}

func resolveRequested(names []string, resolve func(string) (config.CapabilityRef, bool)) ([]config.CapabilityRef, []string) {
	refs := make([]config.CapabilityRef, 0, len(names))
	var missing []string
	seen := make(map[string]bool, len(names))
	for _, name := range names {
		if seen[name] {
			continue
		}
		seen[name] = true
		ref, ok := resolve(name)
		if !ok {
			missing = append(missing, name)
			continue
		}
		refs = append(refs, ref)
	}
	return refs, missing
}
