package environment

// FolderEntry represents a discovered folder with optional git info.
type FolderEntry struct {
	Name string
	Path string
}

// Environment holds all sections of the sys1 environment message.
type Environment struct {
	OS                 OSInfo
	Skills             []SkillInfo
	Agents             []AgentInfo
	SquidOS            SquidOSInfo
	Project            *ProjectInfo // nil if no working dir set
	Projects           []FolderEntry
	Memory             string // content of index.md from memory dir
	MemoryNamespace    string
	MemoryPath         string
	MemoryInstructions string
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

type AgentInfo struct {
	Name        string
	Description string
}

// SquidOSInfo holds Squid-OS directory paths and flags.
type SquidOSInfo struct {
	Version       string
	SkillsDir     string
	AgentsDir     string
	LogsDir       string
	SysPromptsDir string
	SessionsDir   string
	ProjectDir    string
	MemoryDir     string
	TempFolder    string
	SettingsFile  string
	EndpointsFile string
	HistoryFile   string
	DebugEnabled  bool
	Target        string // "interactive" or "autonomous"
}

// ProjectInfo holds project-level context for the working directory.
type ProjectInfo struct {
	Path              string // absolute path to working directory
	IsUnderProjectDir bool   // is it under the configured ProjectDir
	FileTree          string // tree output if git or under projects dir
}
