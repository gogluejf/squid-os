package tools

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"squid-os/internal/style"
	"squid-os/internal/util"
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
	DisplayParams: []string{"agent", "label"},
	Style:        style.AgentStyle(),
	Schema: []byte(`{
		"type": "object",
		"properties": {
			"label": {
				"type": "string",
				"description": "Short description (10 words max) of the caller's intent for this agent call. Used for display."
			},
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
	Name:         "inline_agent",
	Description:  "Run an ad hoc inline agent and return its final answer.",
	DisplayParams: []string{"label"},
	Style:        style.AgentStyle(),
	Schema: []byte(`{
		"type": "object",
		"properties": {
			"label": {
				"type": "string",
				"description": "Short description (15 words max) of the caller's intent for this inline agent call. Used for display."
			},
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

func executeListAgents(_ map[string]interface{}, ctx RuntimeContext) ToolResult {
	cfg := ctx.Config
	if len(cfg.Agents) == 0 {
		return success("No callable agents.")
	}
	catalog := ctx.Catalog
	var lines []string
	for _, ref := range cfg.Agents {
		description := "(unavailable)"
		if catalog != nil {
			entry, ok := catalog.ResolveAgent(ref.Name)
			if ok && entry.Scope == ref.Scope {
				description = entry.Description
			}
		}
		lines = append(lines, fmt.Sprintf("- %s: [%s] %s", ref.Name, ref.Scope, description))
	}
	return success(strings.Join(lines, "\n"))
}

func executeCallAgent(args map[string]interface{}, ctx RuntimeContext) ToolResult {
	cfg := ctx.Config
	name, _ := args["agent"].(string)
	prompt, _ := args["prompt"].(string)
	label, _ := args["label"].(string)
	if name == "" || prompt == "" {
		return failure("agent and prompt are required")
	}
	label = util.TruncateWords(label, 15)
	if label != "" {
		args["label"] = label
	}
	ref, ok := findCapability(cfg.Agents, name)
	if !ok {
		return failure(fmt.Sprintf("agent %q is not callable. Run list_agents to see available agents.", name))
	}
	catalog := ctx.Catalog
	if catalog == nil {
		return failure("agent catalog not initialized")
	}
	if _, err := catalog.LoadAgent(ref.Scope, ref.Name); err != nil {
		return failure(err.Error())
	}
	return executeAgentCLI(name, prompt, args, ctx, nil)
}

func executeInlineAgent(args map[string]interface{}, ctx RuntimeContext) ToolResult {
	prompt, _ := args["prompt"].(string)
	label, _ := args["label"].(string)
	if prompt == "" {
		return failure("prompt is required")
	}
	label = util.TruncateWords(label, 15)
	if label != "" {
		args["label"] = label
	}
	return executeAgentCLI("", prompt, args, ctx, args)
}

func executeAgentCLI(name, prompt string, values map[string]interface{}, ctx RuntimeContext, inline map[string]interface{}) ToolResult {
	cfg := ctx.Config
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

	// Pass preallocated child session lineage to the child process
	if ctx.ChildRef.ID != "" && ctx.ChildRef.Name != "" {
		argv = append(argv,
			"--session-id", ctx.ChildRef.ID,
			"--parent-session-id", ctx.Identity.ID,
			"--root-session-id", ctx.Identity.RootID,
			"--parent-tool-call-id", ctx.ToolCallID,
			"--session-depth", fmt.Sprint(ctx.Identity.Depth+1),
			"--parent-session-dir", ctx.SessionDir,
			"--save-name", ctx.ChildRef.Name,
		)
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

func success(value string) ToolResult { return ToolResult{Status: ResultStatusSuccess, Result: value} }
func failure(value string) ToolResult { return ToolResult{Status: ResultStatusError, Error: value} }
