package tools

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"squid-os/internal/agent"
	"squid-os/internal/config"
	"squid-os/internal/style"
)

var ListAgents = Tool{
	Name:        "list_agents",
	Description: "List callable agents.",
	Style:       style.AgentStyle(),
	Schema: []byte(`{
		"type": "object",
		"properties": {},
		"additionalProperties": false
	}`),
	Execute: executeListAgents,
}

var CallAgent = Tool{
	Name:         "call_agent",
	Description:  "Run an installed callable agent and return its final answer.",
	DisplayParam: "agent",
	Style:        style.AgentStyle(),
	Schema: []byte(`{
		"type": "object",
		"properties": {
			"agent": {
				"type": "string",
				"description": "Name of the installed agent to execute. The agent must be allowed by the current session's callable agent scope."
			},
			"prompt": {
				"type": "string",
				"description": "Task or instruction to send to the agent."
			},
			"max_steps": {
				"type": "integer",
				"description": "Maximum number of agent loop steps allowed for the delegated run."
			},
			"max_tools": {
				"type": "integer",
				"description": "Maximum number of tool executions allowed for the delegated run."
			},
			"max_time": {
				"type": "string",
				"description": "Maximum duration of the delegated run, expressed as a Go duration such as 30m or 2h."
			}
		},
		"required": ["agent", "prompt"],
		"additionalProperties": false
	}`),
	Execute: executeCallAgent,
}

var InlineAgent = Tool{
	Name:        "inline_agent",
	Description: "Run an ad hoc inline agent and return its final answer.",
	Style:       style.AgentStyle(),
	Schema: []byte(`{
		"type": "object",
		"properties": {
			"prompt": {
				"type": "string",
				"description": "Task or instruction to send to the inline agent."
			},
			"system": {
				"type": "string",
				"description": "Agent-specific system instructions for the inline run; this does not replace the Squid-OS base system prompt."
			},
			"model": {
				"type": "string",
				"description": "Model override in provider/model form, such as openai/gpt-5.5."
			},
			"tools": {
				"type": "array",
				"description": "Names of tools available to the inline agent.",
				"items": {
					"type": "string"
				}
			},
			"skills": {
				"type": "array",
				"description": "Names of skills available to the inline agent; this sets the available skill scope, not an active loaded skill.",
				"items": {
					"type": "string"
				}
			},
			"agents": {
				"type": "array",
				"description": "Names of installed agents that the inline agent may call as subagents.",
				"items": {
					"type": "string"
				}
			},
			"auth_mode": {
				"type": "string",
				"description": "Non-interactive authorization policy applied to tool execution during the inline run."
			},
			"max_steps": {
				"type": "integer",
				"description": "Maximum number of agent loop steps allowed for the inline run."
			},
			"max_tools": {
				"type": "integer",
				"description": "Maximum number of tool executions allowed for the inline run."
			},
			"max_time": {
				"type": "string",
				"description": "Maximum duration of the inline run, expressed as a Go duration such as 30m or 2h."
			}
		},
		"required": ["prompt"],
		"additionalProperties": false
	}`),
	Execute: executeInlineAgent,
}

func executeListAgents(_ map[string]interface{}, cfg config.SessionConfig) ToolResult {
	registry := agent.GetRegistry()
	if registry == nil {
		return failure("agent registry not initialized")
	}
	allowed := make(map[string]bool, len(cfg.Agents))
	for _, name := range cfg.Agents {
		allowed[name] = true
	}
	var lines []string
	for _, entry := range registry.List() {
		if allowed[entry.Name] {
			lines = append(lines, fmt.Sprintf("- %s: %s", entry.Name, entry.Description))
		}
	}
	if len(lines) == 0 {
		return success("No callable agents.")
	}
	return success(strings.Join(lines, "\n"))
}

func executeCallAgent(args map[string]interface{}, cfg config.SessionConfig) ToolResult {
	name, _ := args["agent"].(string)
	prompt, _ := args["prompt"].(string)
	if name == "" || prompt == "" {
		return failure("agent and prompt are required")
	}
	if !contains(cfg.Agents, name) {
		return failure(fmt.Sprintf("agent %q is not callable. Run list_agents to see available agents.", name))
	}
	return executeAgentCLI(name, prompt, args, cfg, nil)
}

func executeInlineAgent(args map[string]interface{}, cfg config.SessionConfig) ToolResult {
	prompt, _ := args["prompt"].(string)
	if prompt == "" {
		return failure("prompt is required")
	}
	return executeAgentCLI("", prompt, args, cfg, args)
}

func executeAgentCLI(name, prompt string, values map[string]interface{}, cfg config.SessionConfig, inline map[string]interface{}) ToolResult {
	if cfg.Limits.MaxAgentDepth <= 0 {
		return failure("agent call depth exceeded")
	}
	executable, err := os.Executable()
	if err != nil {
		return failure(err.Error())
	}
	argv := []string{"run"}
	if name != "" {
		argv = append(argv, name)
	}
	argv = append(argv, "--prompt", prompt, "--mode", "final_message", "--max-agent-depth", fmt.Sprint(cfg.Limits.MaxAgentDepth-1))
	if cfg.WorkingDir != "" {
		argv = append(argv, "--working-dir", cfg.WorkingDir)
	}
	appendOptional := func(key, flag string) {
		if value, ok := values[key]; ok {
			argv = append(argv, flag, fmt.Sprint(value))
		}
	}
	appendOptional("max_steps", "--max-steps")
	appendOptional("max_tools", "--max-tools")
	appendOptional("max_time", "--max-time")
	if inline != nil {
		appendOptional("system", "--system")
		appendOptional("model", "--model")
		appendOptional("auth_mode", "--auth-mode")
		for _, pair := range []struct{ key, flag string }{{"tools", "--tools"}, {"skills", "--skills"}, {"agents", "--agents"}} {
			if raw, ok := inline[pair.key]; ok {
				data, _ := json.Marshal(raw)
				var list []string
				_ = json.Unmarshal(data, &list)
				if len(list) > 0 {
					argv = append(argv, pair.flag, strings.Join(list, ","))
				}
			}
		}
	}
	cmd := exec.Command(executable, argv...)
	cmd.Dir = cfg.WorkingDir
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		return failure(strings.TrimSpace(stderr.String() + " " + err.Error()))
	}
	return success(strings.TrimSpace(stdout.String()))
}

func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
func success(value string) ToolResult { return ToolResult{Status: ResultStatusSuccess, Result: value} }
func failure(value string) ToolResult { return ToolResult{Status: ResultStatusError, Error: value} }
