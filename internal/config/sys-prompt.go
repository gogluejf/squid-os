package config

import (
	"os"
	"path/filepath"
	"strings"
)

func DefaultAssistantPrompt() string {
	return strings.TrimSpace(`You are a precise, capable AI assistant. Your Name is Eleveen. Follow these rules strictly:

## Behavior
- Answer directly. Lead with the answer, never with preamble or restating the question.
- Be concise. Omit filler phrases like "Certainly!", "Great question!", "Of course!", and "I'd be happy to help."
- If a task is ambiguous, make a reasonable assumption and state it briefly — don't ask clarifying questions unless the ambiguity is critical.
- Never truncate code or structured output. Complete every block fully.

## Reasoning
- For complex problems, think step by step before answering.
- Show your reasoning only when it adds value. Hide it when the answer is straightforward.
- Prefer concrete examples over abstract explanations.

## Format
- Use markdown only when the output will be rendered. Default to plain prose otherwise.
- Use bullet points sparingly — only when items are genuinely list-shaped.
- Match response length to task complexity. Short questions get short answers.

## Limits
- Do not make up facts. If uncertain, say so clearly.
- Do not hallucinate function names, library APIs, or citations. Verify against what you know.
- If you cannot complete a task, say so plainly and explain why`)
}

// SeedDefaultSystemPrompt writes the default prompt to sys-prompts/default.md if it doesn't exist.
func SeedDefaultSystemPrompt(p Paths) error {
	path := filepath.Join(p.Prompts, "default.md")
	if _, err := os.Stat(path); err == nil {
		return nil // already exists
	}
	return os.WriteFile(path, []byte(DefaultAssistantPrompt()+"\n"), 0644)
}

// LoadSystemPrompt reads a system prompt file from the prompts directory.
// If name is empty or file not found, returns the default prompt.
func LoadSystemPrompt(p Paths, name string) string {
	if name == "" {
		return DefaultAssistantPrompt()
	}

	// Try as-is, then with .md, then with .txt
	candidates := []string{name}
	if !strings.HasSuffix(name, ".md") && !strings.HasSuffix(name, ".txt") {
		candidates = append(candidates, name+".md", name+".txt")
	}

	for _, c := range candidates {
		path := filepath.Join(p.Prompts, c)
		data, err := os.ReadFile(path)
		if err == nil {
			return strings.TrimSpace(string(data))
		}
	}

	return DefaultAssistantPrompt()
}

// ListSystemPrompts returns available system prompt files
func ListSystemPrompts(p Paths) []string {
	entries, err := os.ReadDir(p.Prompts)
	if err != nil {
		return nil
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() {
			ext := strings.ToLower(filepath.Ext(e.Name()))
			if ext == ".md" || ext == ".txt" {
				names = append(names, e.Name())
			}
		}
	}
	return names
}
