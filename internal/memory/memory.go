package memory

import (
	"fmt"
	"path/filepath"
)

type Namespace string

const (
	NamespaceWorkspace Namespace = "workspace"
	NamespaceGlobal    Namespace = "global"
	NamespaceAgent     Namespace = "agent"
)

type Config struct {
	Namespace    Namespace `json:"namespace,omitempty"`
	Path         string    `json:"path,omitempty"`
	Instructions string    `json:"instructions,omitempty"`
}

type Paths struct {
	GlobalMemoryDir string
	AgentsDir       string
}

func ResolvePath(namespace Namespace, workingDir string, paths Paths, agentName string) (string, error) {
	switch namespace {
	case "", NamespaceGlobal:
		return paths.GlobalMemoryDir, nil
	case NamespaceWorkspace:
		if workingDir == "" {
			return "", fmt.Errorf("workspace memory requires a working directory")
		}
		return filepath.Join(workingDir, "memory"), nil
	case NamespaceAgent:
		if agentName == "" {
			return "", fmt.Errorf("agent memory requires an agent name")
		}
		return filepath.Join(paths.AgentsDir, agentName, "memory"), nil
	default:
		return "", fmt.Errorf("unknown memory namespace %q", namespace)
	}
}
