package environment

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"squid-os/internal/config"
	"squid-os/internal/git"
	runtimeconfig "squid-os/internal/runtime"
	"squid-os/internal/util"
	"squid-os/internal/version"
)

// sectionRe matches "## [SectionName]" headings and captures the name inside brackets.
var sectionRe = regexp.MustCompile(`##\s+\[([^\]]+)\]`)

// LoadEnvironment assembles environment sections using resolved session scopes.
func LoadEnvironment(paths config.Paths, sessionConfig config.SessionConfig, catalog runtimeconfig.Catalog) Environment {
	workingDir := sessionConfig.WorkingDir
	debugEnabled := sessionConfig.DebugEnabled
	target := sessionConfig.Target
	sessionMemory := sessionConfig.Memory
	projectDir := paths.ProjectDir

	workspaceMemory := ""
	workspaceSkills := ""
	workspaceAgents := ""
	if workingDir != "" {
		workspaceRoot := filepath.Join(workingDir, ".squid-os")
		workspaceMemory = filepath.Join(workspaceRoot, "memory")
		workspaceSkills = filepath.Join(workspaceRoot, "skills")
		workspaceAgents = filepath.Join(workspaceRoot, "agents")
	}

	env := Environment{
		OS:     CollectOSInfo(workingDir),
		Skills: catalog.FormatSkills(sessionConfig),
		Agents: catalog.FormatAgents(sessionConfig),
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
			SettingsFile:  paths.SettingsFile(),
			EndpointsFile: paths.EndpointsFile(),
			HistoryFile:   paths.HistoryFile(),
			DebugEnabled:  debugEnabled,
			Target:        target,
		},
		WorkspaceMemory: workspaceMemory,
		WorkspaceSkills: workspaceSkills,
		WorkspaceAgents: workspaceAgents,
	}

	if workingDir != "" {
		env.Project = LoadProjectInfo(workingDir, projectDir)
	}
	env.Projects = FindProjects(projectDir)
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

	// Effective capability lists for this session.
	b.WriteString("## [Skills]\n")
	b.WriteString(env.Skills)
	b.WriteString("\n## [Agents]\n")
	b.WriteString(env.Agents)
	b.WriteString("\n")

	// [Working Directory] section — project context
	if env.Project != nil {
		b.WriteString("## [Working Directory]\n")
		b.WriteString(fmt.Sprintf("- working-dir: %s\n", util.FriendlyPath(git.Decorate(env.Project.Path))))
		b.WriteString(fmt.Sprintf("- under-project-dir: %s\n", boolOrNot(env.Project.IsUnderProjectDir)))
		if env.WorkspaceMemory != "" {
			b.WriteString("- workspace-memory: " + util.FriendlyPath(env.WorkspaceMemory) + "\n")
		}
		if env.WorkspaceSkills != "" {
			b.WriteString("- workspace-skills: " + util.FriendlyPath(env.WorkspaceSkills) + "\n")
		}
		if env.WorkspaceAgents != "" {
			b.WriteString("- workspace-agents: " + util.FriendlyPath(env.WorkspaceAgents) + "\n")
		}
		if env.Project.FileTree != "" {
			b.WriteString("- file-tree:\n")
			b.WriteString("```\n")
			b.WriteString(env.Project.FileTree)
			b.WriteString("```\n")
		}
		b.WriteString("\n")
	}

	// [Squid-OS] section — global installation context
	b.WriteString("## [Squid-OS]\n")
	b.WriteString("- version: " + env.SquidOS.Version + "\n")
	if env.SquidOS.Target != "" {
		b.WriteString("- session-mode: " + env.SquidOS.Target + "\n")
	}
	b.WriteString("- global-skills: " + util.FriendlyPath(git.Decorate(env.SquidOS.SkillsDir)) + "\n")
	b.WriteString("- global-agents: " + util.FriendlyPath(git.Decorate(env.SquidOS.AgentsDir)) + "\n")
	b.WriteString("- logs: " + util.FriendlyPath(git.Decorate(env.SquidOS.LogsDir)) + "\n")
	b.WriteString("- sys-prompts: " + util.FriendlyPath(git.Decorate(env.SquidOS.SysPromptsDir)) + "\n")
	b.WriteString("- sessions: " + util.FriendlyPath(git.Decorate(env.SquidOS.SessionsDir)) + "\n")
	b.WriteString("- project-dir: " + util.FriendlyPath(git.Decorate(env.SquidOS.ProjectDir)) + "\n")
	b.WriteString("- memory: " + util.FriendlyPath(git.Decorate(env.SquidOS.MemoryDir)) + "\n")
	b.WriteString("- temp: " + util.FriendlyPath(git.Decorate(env.SquidOS.TempFolder)) + "\n")
	b.WriteString("- settings: " + util.FriendlyPath(env.SquidOS.SettingsFile) + "\n")
	b.WriteString("- endpoints: " + util.FriendlyPath(env.SquidOS.EndpointsFile) + "\n")
	b.WriteString("- history: " + util.FriendlyPath(env.SquidOS.HistoryFile) + "\n")
	if env.SquidOS.DebugEnabled {
		b.WriteString("- debug: enabled\n")
	}
	b.WriteString("\n")

	// [Projects] section
	if len(env.Projects) > 0 {
		b.WriteString("## [Projects]\n")
		for _, p := range env.Projects {
			b.WriteString(fmt.Sprintf("- %s: %s\n", p.Name, util.FriendlyPath(git.Decorate(p.Path))))
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
