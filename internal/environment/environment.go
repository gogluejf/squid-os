package environment

// Environment holds all sections of the sys1 environment message.
type Environment struct {
	OS       OSInfo
	Skills   []SkillInfo
	SquidOS  SquidOSInfo
	Project  *ProjectInfo   // nil if no working dir set
	Projects []ProjectEntry // all discovered projects under ProjectDir
}

// OSInfo holds OS-level context.
type OSInfo struct {
	OS            string
	Arch          string
	Home          string
	CurrentDir    string
	GitInstalled  bool
	TreeInstalled bool
}

// SkillInfo is a lightweight skill registry entry.
type SkillInfo struct {
	Name        string
	Description string
}

// SquidOSInfo holds Squid-OS directory paths and flags.
type SquidOSInfo struct {
	SkillsDir     string
	LogsDir       string
	SysPromptsDir string
	SessionsDir   string
	ProjectDir    string
	MemoryDir     string
	TempFolder    string
	DebugEnabled  bool
}

// ProjectInfo holds project-level context for the working directory.
type ProjectInfo struct {
	Path              string // absolute path to working directory
	IsUnderProjectDir bool   // is it under the configured ProjectDir
	IsGitRepo         bool   // has .git
	FileTree          string // tree output if git or under projects dir
}

// ProjectEntry represents a single discovered project.
type ProjectEntry struct {
	Name  string
	Path  string
	IsGit bool
}
