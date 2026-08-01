package environment

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"squid-os/internal/agent"
	"squid-os/internal/config"
	"squid-os/internal/git"
	"squid-os/internal/skills"
	"squid-os/internal/util"
	"squid-os/internal/version"
)

// sectionRe matches "## [SectionName]" headings and captures the name inside brackets.
var sectionRe = regexp.MustCompile(`##\s+\[([^\]]+)\]`)

// LoadEnvironment assembles environment sections using resolved session scopes.
func LoadEnvironment(paths config.Paths, sessionConfig config.SessionConfig) Environment {
	workingDir := sessionConfig.WorkingDir
	debugEnabled := sessionConfig.DebugEnabled
	sessionMemory := sessionConfig.Memory
	projectDir := paths.ProjectDir

	env := Environment{
		OS:     CollectOSInfo(workingDir),
		Skills: loadSkillEntries(sessionConfig.Skills),
		Agents: loadAgentEntries(sessionConfig.Agents),
		SquidOS: SquidOSInfo{
			Version:       version.Full(),
			SkillsDir:     paths.Skills,
			AgentsDir:     paths.Agents,
			LogsDir:       paths.Logs,
			SysPromptsDir: paths.SysPrompts,
			SessionsDir:   paths.Sessions,
			ProjectDir:    projectDir,
			MemoryDir:     paths.MemoryDir,
			TempFolder:    paths.TempFolder,
			DocumentsDir:  paths.DocumentsDir,
			SettingsFile:  paths.SettingsFile(),
			EndpointsFile: paths.EndpointsFile(),
			HistoryFile:   paths.HistoryFile(),
			DebugEnabled:  debugEnabled,
		},
	}

	if workingDir != "" {
		env.Project = LoadProjectInfo(workingDir, projectDir)
	}
	env.Projects = FindProjects(projectDir)
	env.Documents = FindDocuments(paths.DocumentsDir)
	env.MemoryNamespace = sessionMemory.Namespace
	env.MemoryPath = sessionMemory.Path
	env.MemoryInstructions = sessionMemory.Instructions
	memoryPath := sessionMemory.Path
	if memoryPath == "" {
		memoryPath = paths.MemoryDir
	}
	env.Memory = loadMemoryIndex(memoryPath)

	return env
}

// FormatEnvironment renders the Environment into a markdown string for the sys1 message.
func FormatEnvironment(env Environment) string {
	var b strings.Builder
	b.WriteString("# Environment\n\n")

	// [OS] section
	b.WriteString("## [OS]\n")
	b.WriteString(fmt.Sprintf("- os: %s\n", env.OS.OS))
	b.WriteString(fmt.Sprintf("- arch: %s\n", env.OS.Arch))
	b.WriteString(fmt.Sprintf("- home: %s\n", env.OS.Home))
	b.WriteString("- git: " + installedOrNot(env.OS.GitInstalled) + "\n")
	b.WriteString("- tree: " + installedOrNot(env.OS.TreeInstalled) + "\n")
	b.WriteString("\n")

	// [Skills] section
	b.WriteString("## [Skills]\n")
	if len(env.Skills) == 0 {
		b.WriteString("- none: \n")
	} else {
		for _, s := range env.Skills {
			b.WriteString(fmt.Sprintf("- %s: %s\n", s.Name, s.Description))
		}
	}
	b.WriteString("\n")

	b.WriteString("## [Agents]\n")
	if len(env.Agents) == 0 {
		b.WriteString("- none: \n")
	} else {
		for _, a := range env.Agents {
			b.WriteString(fmt.Sprintf("- %s: %s\n", a.Name, a.Description))
		}
	}
	b.WriteString("\n")

	// [Squid-OS] section
	b.WriteString("## [Squid-OS]\n")
	b.WriteString("- version: " + env.SquidOS.Version + "\n")
	b.WriteString("- skills: " + util.FriendlyPath(git.Decorate(env.SquidOS.SkillsDir)) + "\n")
	b.WriteString("- agents: " + util.FriendlyPath(git.Decorate(env.SquidOS.AgentsDir)) + "\n")
	b.WriteString("- logs: " + util.FriendlyPath(git.Decorate(env.SquidOS.LogsDir)) + "\n")
	b.WriteString("- sys-prompts: " + util.FriendlyPath(git.Decorate(env.SquidOS.SysPromptsDir)) + "\n")
	b.WriteString("- sessions: " + util.FriendlyPath(git.Decorate(env.SquidOS.SessionsDir)) + "\n")
	b.WriteString("- project-dir: " + util.FriendlyPath(git.Decorate(env.SquidOS.ProjectDir)) + "\n")
	b.WriteString("- memory: " + util.FriendlyPath(git.Decorate(env.SquidOS.MemoryDir)) + "\n")
	b.WriteString("- temp: " + util.FriendlyPath(git.Decorate(env.SquidOS.TempFolder)) + "\n")
	b.WriteString("- documents: " + util.FriendlyPath(git.Decorate(env.SquidOS.DocumentsDir)) + "\n")
	b.WriteString("- settings: " + util.FriendlyPath(env.SquidOS.SettingsFile) + "\n")
	b.WriteString("- endpoints: " + util.FriendlyPath(env.SquidOS.EndpointsFile) + "\n")
	b.WriteString("- history: " + util.FriendlyPath(env.SquidOS.HistoryFile) + "\n")
	if env.SquidOS.DebugEnabled {
		b.WriteString("- debug: enabled\n")
	}
	b.WriteString("\n")

	// [Working Directory] section
	if env.Project != nil {
		b.WriteString("## [Working Directory]\n")
		b.WriteString(fmt.Sprintf("- working-dir: %s\n", util.FriendlyPath(git.Decorate(env.Project.Path))))
		b.WriteString(fmt.Sprintf("- under-project-dir: %s\n", boolOrNot(env.Project.IsUnderProjectDir)))
		if env.Project.FileTree != "" {
			b.WriteString("- file-tree:\n")
			b.WriteString("```\n")
			b.WriteString(env.Project.FileTree)
			b.WriteString("```\n")
		}
		b.WriteString("\n")
	}

	// [Projects] section
	if len(env.Projects) > 0 {
		b.WriteString("## [Projects]\n")
		for _, p := range env.Projects {
			b.WriteString(fmt.Sprintf("- %s: %s\n", p.Name, util.FriendlyPath(git.Decorate(p.Path))))
		}
		b.WriteString("\n")
	}

	// [Documents] section
	if len(env.Documents) > 0 {
		b.WriteString("## [Documents]\n")
		for _, d := range env.Documents {
			b.WriteString(fmt.Sprintf("- %s: %s\n", d.Name, util.FriendlyPath(git.Decorate(d.Path))))
		}
		b.WriteString("\n")
	}

	// [Memory] section
	if env.MemoryNamespace != "" || env.Memory != "" {
		b.WriteString("## [Memory]\n")
		if env.MemoryNamespace != "" {
			b.WriteString("- namespace: " + env.MemoryNamespace + "\n")
		}
		if env.MemoryPath != "" {
			b.WriteString("- path: " + util.FriendlyPath(env.MemoryPath) + "\n")
		}
		if env.MemoryInstructions != "" {
			b.WriteString("- instructions: " + env.MemoryInstructions + "\n")
		}
		if env.Memory != "" {
			b.WriteString(env.Memory)
			b.WriteString("\n")
		}
		b.WriteString("\n")
	}

	return b.String()
}

// ExtractSectionNames parses the formatted environment content to find all
// "## [Section]" headings and returns the section names in order.
func ExtractSectionNames(content string) []string {
	matches := sectionRe.FindAllStringSubmatch(content, -1)
	names := make([]string, 0, len(matches))
	for _, m := range matches {
		names = append(names, m[1])
	}
	return names
}

func loadMemoryIndex(memoryDir string) string {
	if memoryDir == "" {
		return ""
	}
	idxPath := filepath.Join(memoryDir, "index.md")
	data, err := os.ReadFile(idxPath)
	if err != nil {
		return ""
	}
	content := strings.TrimSpace(string(data))
	// Cap at 1500 chars to keep token usage reasonable
	if len(content) > 1500 {
		content = content[:1500] + "\n... (truncated)"
	}
	return content
}

func loadSkillEntries(allowedNames []string) []SkillInfo {
	registry := skills.GetRegistry()
	if registry == nil {
		return nil
	}
	allowed := make(map[string]bool, len(allowedNames))
	for _, name := range allowedNames {
		allowed[name] = true
	}
	var entries []SkillInfo
	for _, entry := range registry.List() {
		if allowed[entry.Name] {
			entries = append(entries, SkillInfo{Name: entry.Name, Description: entry.Description})
		}
	}
	return entries
}

func loadAgentEntries(allowedNames []string) []AgentInfo {
	registry := agent.GetRegistry()
	if registry == nil {
		return nil
	}
	allowed := make(map[string]bool, len(allowedNames))
	for _, name := range allowedNames {
		allowed[name] = true
	}
	var entries []AgentInfo
	for _, entry := range registry.List() {
		if allowed[entry.Name] {
			entries = append(entries, AgentInfo{Name: entry.Name, Description: entry.Description})
		}
	}
	return entries
}

func boolOrNot(v bool) string {
	if v {
		return "yes"
	}
	return "no"
}

func installedOrNot(v bool) string {
	if v {
		return "✔ installed"
	}
	return "✘ not installed"
}
