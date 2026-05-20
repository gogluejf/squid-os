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
		env.Project = loadProjectInfo(workingDir, projectDir)
	}
	env.Projects = findProjects(projectDir)

	return env
}

// FormatEnvironment renders the Environment into a markdown string for the sys1 message.
func FormatEnvironment(env Environment) string {
	var b strings.Builder
	b.WriteString("# Environment\n\n")

	// [OS] section
	b.WriteString("## [OS]\n")
	b.WriteString(fmt.Sprintf("OS: %s\n", env.OS.OS))
	b.WriteString(fmt.Sprintf("Arch: %s\n", env.OS.Arch))
	b.WriteString(fmt.Sprintf("Home: %s\n", env.OS.Home))
	b.WriteString(fmt.Sprintf("Current Dir: %s\n", env.OS.CurrentDir))
	b.WriteString(fmt.Sprintf("Git: %s\n", boolOrNot(env.OS.GitInstalled)))
	b.WriteString(fmt.Sprintf("Tree: %s\n", boolOrNot(env.OS.TreeInstalled)))
	b.WriteString("\n")

	// [Skill] section
	b.WriteString("## [Skill]\n")
	if len(env.Skills) == 0 {
		b.WriteString("No skills loaded\n")
	} else {
		for _, s := range env.Skills {
			b.WriteString(fmt.Sprintf("- %s: %s\n", s.Name, s.Description))
		}
	}
	b.WriteString("\n")

	// [Squid-OS] section
	b.WriteString("## [Squid-OS]\n")
	b.WriteString(fmt.Sprintf("Skills: %s\n", env.SquidOS.SkillsDir))
	b.WriteString(fmt.Sprintf("Logs: %s\n", env.SquidOS.LogsDir))
	b.WriteString(fmt.Sprintf("System Prompts: %s\n", env.SquidOS.SysPromptsDir))
	b.WriteString(fmt.Sprintf("Sessions: %s\n", env.SquidOS.SessionsDir))
	b.WriteString(fmt.Sprintf("Project Dir: %s\n", env.SquidOS.ProjectDir))
	b.WriteString(fmt.Sprintf("Memory Dir: %s\n", env.SquidOS.MemoryDir))
	b.WriteString(fmt.Sprintf("Temp Folder: %s\n", env.SquidOS.TempFolder))
	if env.SquidOS.DebugEnabled {
		b.WriteString("Debug: enabled\n")
	}
	b.WriteString("\n")

	// [Project] section
	if env.Project != nil {
		b.WriteString("## [Project]\n")
		b.WriteString(fmt.Sprintf("Working Directory: %s\n", env.Project.Path))
		b.WriteString(fmt.Sprintf("Under Project Dir: %s\n", boolOrNot(env.Project.IsUnderProjectDir)))
		b.WriteString(fmt.Sprintf("Git Init: %s\n", boolOrNot(env.Project.IsGitRepo)))
		if len(env.Projects) > 0 {
			b.WriteString("Projects:\n")
			for _, p := range env.Projects {
				b.WriteString(fmt.Sprintf("- %s (%s)\n", p.Name, p.Path))
			}
		}
		if env.Project.FileTree != "" {
			b.WriteString("File Tree:\n")
			b.WriteString("```\n")
			b.WriteString(env.Project.FileTree)
			b.WriteString("```\n")
		}
		b.WriteString("\n")
	}

	return b.String()
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

func loadProjectInfo(workingDir, projectDir string) *ProjectInfo {
	info := &ProjectInfo{
		Path:      workingDir,
		IsGitRepo: hasGit(workingDir),
	}

	// Check if workingDir is under projectDir
	if strings.HasPrefix(workingDir, projectDir) {
		info.IsUnderProjectDir = true
	}

	// Generate file tree if it's a git repo or under the projects dir
	if info.IsGitRepo || info.IsUnderProjectDir {
		info.FileTree = GenerateTree(workingDir, 3)
	}

	return info
}

func hasGit(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, ".git"))
	return err == nil
}

func findProjects(projectDir string) []ProjectEntry {
	var entries []ProjectEntry
	if projectDir == "" {
		return entries
	}

	infos, err := os.ReadDir(projectDir)
	if err != nil {
		return entries
	}

	for _, info := range infos {
		if !info.IsDir() {
			continue
		}
		path := filepath.Join(projectDir, info.Name())
		if hasGit(path) {
			entries = append(entries, ProjectEntry{
				Name:  info.Name(),
				Path:  path,
				IsGit: true,
			})
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
