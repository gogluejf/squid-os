# Tools

## Core

| Tool | Description |
|------|-------------|
| `read_file(path)` | Read and return the contents of a file at the given path. Supports text, binary (base64), images, and PDFs. |
| `write_file(path, content)` | Create a new file or completely overwrite an existing file with the given content. Use for new files or full rewrites only. |
| `edit_file(path, old_string, new_string, replace_all)` | Perform a precise string replacement in an existing file. `old_string` must match exactly; `replace_all` replaces every occurrence. Prefer over `write_file` for modifications. |

## Bash

| Tool | Description |
|------|-------------|
| `bash(command, timeout, run_in_background)` | Execute a shell command and return stdout/stderr. Use for git, find, grep, curl, and any CLI tool. `timeout` in milliseconds (default 120s). `run_in_background` spawns without blocking. Destructive commands (rm -rf, push --force) require user confirmation first. |

## Git

| Tool | Description |
|------|-------------|
| `git_status()` | Show working tree status: untracked files, staged changes, branch info. |
| `git_add(files[])` | Stage specific files. Prefer explicit file paths over `.` or `-A` to avoid committing sensitive files. |
| `git_commit(message)` | Create a new commit with the given message. Never force-commit or skip hooks unless explicitly requested. |
| `git_diff(staged)` | Show changes. `staged=true` for staged only, `false` for unstaged, omit for both. |
| `git_log(line_count)` | Show recent commit history. Default 10 commits. |
| `git_push(remote, branch)` | Push to remote. Never force-push to main/master. Requires confirmation for destructive pushes. |

## OS

| Tool | Description |
|------|-------------|
| `open(path_or_url)` | Open a file, URL, or directory with the system default application (xdg-open on Linux). Use for launching browsers, editors, or previewing files. |
| `process_list()` | List currently running processes with pid, command, and status. |
| `kill_process(pid)` | Terminate a process by PID. Requires user confirmation before executing. |

## Language

| Tool | Description |
|------|-------------|
| `execute_python(code)` | Execute a Python code snippet in an isolated process and return stdout/stderr. Use for data processing, calculations, and scripts. Runs with a timeout to prevent infinite loops. |

## Util

| Tool | Description |
|------|-------------|
| `time()` | Return the current date and time in the user's local timezone. |
| `calculate(expression)` | Evaluate a mathematical expression and return the result. Supports basic arithmetic, trigonometry, and common functions. |

## Workflow

| Tool | Description |
|------|-------------|
| `ask_question(questions[])` | Prompt the user for input. Supports text questions, single-select options, multi-select options, and free-text input. Execution blocks until the user responds. Use for confirmations, clarifications, and decisions. |
| `switch_mode(mode)` | Change the agent's operating mode. Modes: `plan` (explore and design), `execute` (implement), `review` (review and validate). Updates the system context to match the mode's behavior. |

## Skills

| Tool | Description |
|------|-------------|
| `invoke_skill(skill_name, args)` | Load and execute a named skill. Skills are reusable prompt templates with constrained tool access. Use instead of re-composing the same tool sequences. Arguments are passed as a string. |

## External (later)

| Tool | Description |
|------|-------------|
| `web_search(query)` | Search the web using the Brave Search API and return results with titles, URLs, and snippets. Use for current events, documentation, and information beyond the agent's knowledge cutoff. |
