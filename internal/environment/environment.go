package environment

// FolderEntry represents a discovered folder with optional git info.
type FolderEntry struct {
	Name string
	Path string
}

// Environment holds all sections of the sys1 environment message.
type Environment struct {
	OS        OSInfo
	Skills    []SkillInfo
	SquidOS   SquidOSInfo
	Project   *ProjectInfo // nil if no working dir set
	Projects  []FolderEntry
	Documents []FolderEntry
	Memory    string // content of index.md from memory dir
}

// OSInfo holds OS-level context.
type OSInfo struct {
	OS            string
	Arch          string
	Home          string
	WorkingDir    string
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
	Version       string
	SkillsDir     string
	LogsDir       string
	SysPromptsDir string
	SessionsDir   string
	ProjectDir    string
	MemoryDir     string
	TempFolder    string
	DocumentsDir  string
	SettingsFile  string
	EndpointsFile string
	HistoryFile   string
	DebugEnabled  bool
}

// ProjectInfo holds project-level context for the working directory.
type ProjectInfo struct {
	Path              string // absolute path to working directory
	IsUnderProjectDir bool   // is it under the configured ProjectDir
	FileTree          string // tree output if git or under projects dir
}
