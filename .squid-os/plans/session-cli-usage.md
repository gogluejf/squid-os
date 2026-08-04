# CLI Usage

## Run a new session

```
squid-os run \
  --provider openai \
  --model gpt-4o \
  --thinking on \
  --prompt "Review this code" \
  --tools read_file,bash \
  --skills code-review \
  --sysprompt ./prompts/review.md \
  --working-dir ./project
```

## Pipe input as prompt

```
cat code.go | squid-os run --provider openai --model gpt-4o
```

## Resume a saved session

```
squid-os resume my-session \
  --prompt "Continue from where you left off"
```

## Arguments

| Flag | Description |
|------|-------------|
| `--provider` | Provider name (openai, anthropic, etc.) |
| `--model` | Model name |
| `--thinking` | Thinking mode: on/off |
| `--prompt` | User message (if omitted, reads from stdin) |
| `--tools` | Comma-separated tool names (empty = all) |
| `--skills` | Comma-separated skill names |
| `--sysprompt` | System prompt file path |
| `--working-dir` | Working directory |
