package environment

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"squid-os/internal/config"
	"squid-os/internal/skills"
)

// sectionRe matches "## [SectionName]" headings and captures the name inside brackets.
var sectionRe = regexp.MustCompile(`##\s+\[([^\]]+)\]`)

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
		DocumentsDir:    paths.DocumentsDir,
			DebugEnabled:  settings.DebugEnabled,
		},
	}

	if workingDir != "" {
		env.Project = LoadProjectInfo(workingDir, projectDir)
	}
	env.Projects = FindProjects(projectDir)
	env.Documents = FindDocuments(paths.DocumentsDir)
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

	// [Squid-OS] section
	b.WriteString("## [Squid-OS]\n")
	b.WriteString("- skills: " + dirOrGit(env.SquidOS.SkillsDir) + "\n")
	b.WriteString("- logs: " + dirOrGit(env.SquidOS.LogsDir) + "\n")
	b.WriteString("- sys-prompts: " + dirOrGit(env.SquidOS.SysPromptsDir) + "\n")
	b.WriteString("- sessions: " + dirOrGit(env.SquidOS.SessionsDir) + "\n")
	b.WriteString("- project-dir: " + dirOrGit(env.SquidOS.ProjectDir) + "\n")
	b.WriteString("- memory: " + dirOrGit(env.SquidOS.MemoryDir) + "\n")
	b.WriteString("- temp: " + dirOrGit(env.SquidOS.TempFolder) + "\n")
	b.WriteString("- documents: " + dirOrGit(env.SquidOS.DocumentsDir) + "\n")
	if env.SquidOS.DebugEnabled {
		b.WriteString("- debug: enabled\n")
	}
	b.WriteString("\n")

	// [Working Directory] section
	if env.Project != nil {
		b.WriteString("## [Working Directory]\n")
		b.WriteString(fmt.Sprintf("- working-dir: %s\n", dirOrGit(env.Project.Path)))
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
			b.WriteString(fmt.Sprintf("- %s: %s (git)\n", p.Name, p.Path))
		}
		b.WriteString("\n")
	}

	// [Documents] section
	if len(env.Documents) > 0 {
		b.WriteString("## [Documents]\n")
		for _, d := range env.Documents {
			if d.IsGit {
				b.WriteString(fmt.Sprintf("- %s: %s (git)\n", d.Name, d.Path))
			} else {
				b.WriteString(fmt.Sprintf("- %s: %s\n", d.Name, d.Path))
			}
		}
		b.WriteString("\n")
	}

	// [Memory] section
	if env.Memory != "" {
		b.WriteString("## [Memory]\n")
		b.WriteString(env.Memory)
		b.WriteString("\n\n")
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

func installedOrNot(v bool) string {
	if v {
		return "✔ installed"
	}
	return "✘ not installed"
}

func dirOrGit(dir string) string {
	if hasGit(dir) {
		return dir + " (git)"
	}
	return dir
}
