package agent

// Definition is a reusable installed runtime preset.
type Definition struct {
	Name             string       `yaml:"name"`
	Description      string       `yaml:"description"`
	Mode             string       `yaml:"mode"`
	AuthMode         string       `yaml:"auth_mode"`
	Model            string       `yaml:"model"`
	Thinking         *bool        `yaml:"thinking"`
	WorkingDirectory string       `yaml:"working_directory"`
	System           string       `yaml:"system"`
	Tools            []string     `yaml:"tools"`
	Skills           []string     `yaml:"skills"`
	Agents           []string     `yaml:"agents"`
	Save             SaveConfig   `yaml:"save"`
	Memory           MemoryConfig `yaml:"memory"`
	Limits           LimitsConfig `yaml:"limits"`
}

type SaveConfig struct {
	Enabled bool `yaml:"enabled"`
}

type MemoryConfig struct {
	Namespace    string        `yaml:"namespace"`
	Instructions string        `yaml:"instructions"`
	Journal      JournalConfig `yaml:"journal"`
	Summary      SummaryConfig `yaml:"summary"`
}

type JournalConfig struct {
	Enabled    bool `yaml:"enabled"`
	MaxEntries int  `yaml:"max_entries"`
}

type SummaryConfig struct {
	Enabled bool `yaml:"enabled"`
}

type LimitsConfig struct {
	MaxSteps            int    `yaml:"steps"`
	MaxTools            int    `yaml:"tools"`
	MaxTime             string `yaml:"time"`
	MaxToolResultTokens int    `yaml:"max_tool_result_tokens"`
	MaxAgentDepth       int    `yaml:"max_agent_depth"`
}

type Entry struct {
	Name, Description, Path string
}
