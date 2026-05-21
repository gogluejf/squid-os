package environment

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"squid-os/internal/config"
	"squid-os/internal/skills"
)

// LoadEnvironment assembles all sections and returns a full Environment struct.
func LoadEnvironment(paths config.Paths, settings config.Settings, workingDir string) Environment {
	projectDir := paths.ProjectDir

	env := Environment{
		OS:     CollectOSInfo(workingDir),
		Skills: loadSkillEntries(),
		SquidOS: SquidOSInfo{
			SkillsDir:     paths.Skills,
			LogsDir:       paths.Logs,
			SysPromptsDir: paths.SysPrompts,
			SessionsDir:   paths.Sessions,
			ProjectDir:    projectDir,
			MemoryDir:     paths.MemoryDir,
			TempFolder:    paths.TempFolder,
			DebugEnabled:  settings.DebugEnabled,
		},
	}

	if workingDir != "" {
		env.Project = LoadProjectInfo(workingDir, projectDir)
	}
	env.Projects = FindProjects(projectDir)
	env.Memory = loadMemoryIndex(paths.MemoryDir)

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
	b.WriteString(fmt.Sprintf("- current-dir: %s\n", env.OS.CurrentDir))
	b.WriteString(fmt.Sprintf("- git: %s\n", boolOrNot(env.OS.GitInstalled)))
	b.WriteString(fmt.Sprintf("- tree: %s\n", boolOrNot(env.OS.TreeInstalled)))
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

	// [Squid-OS] section
	b.WriteString("## [Squid-OS]\n")
	b.WriteString(fmt.Sprintf("- skills: %s\n", env.SquidOS.SkillsDir))
	b.WriteString(fmt.Sprintf("- logs: %s\n", env.SquidOS.LogsDir))
	b.WriteString(fmt.Sprintf("- sys-prompts: %s\n", env.SquidOS.SysPromptsDir))
	b.WriteString(fmt.Sprintf("- sessions: %s\n", env.SquidOS.SessionsDir))
	b.WriteString(fmt.Sprintf("- project-dir: %s\n", env.SquidOS.ProjectDir))
	b.WriteString(fmt.Sprintf("- memory: %s\n", env.SquidOS.MemoryDir))
	b.WriteString(fmt.Sprintf("- temp: %s\n", env.SquidOS.TempFolder))
	if env.SquidOS.DebugEnabled {
		b.WriteString("- debug: enabled\n")
	}
	b.WriteString("\n")

	// [Current Project] section
	if env.Project != nil {
		b.WriteString("## [Current Project]\n")
		b.WriteString(fmt.Sprintf("- current-dir: %s\n", env.Project.Path))
		b.WriteString(fmt.Sprintf("- under-project-dir: %s\n", boolOrNot(env.Project.IsUnderProjectDir)))
		b.WriteString(fmt.Sprintf("- git-init: %s\n", boolOrNot(env.Project.IsGitRepo)))
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
			b.WriteString(fmt.Sprintf("- %s: %s\n", p.Name, p.Path))
		}
		b.WriteString("\n")
	}

	// [MEMORY] section
	if env.Memory != "" {
		b.WriteString("## [MEMORY]\n")
		b.WriteString(env.Memory)
		b.WriteString("\n\n")
	}

	return b.String()
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

func loadSkillEntries() []SkillInfo {
	var entries []SkillInfo
	registry := skills.GetRegistry()
	if registry == nil {
		return entries
	}
	skillEntries := registry.List()
	for _, e := range skillEntries {
		entries = append(entries, SkillInfo{
			Name:        e.Name,
			Description: e.Description,
		})
	}
	return entries
}

func boolOrNot(v bool) string {
	if v {
		return "yes"
	}
	return "no"
}
