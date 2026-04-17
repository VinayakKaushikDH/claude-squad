# Tech Context

## Language & Runtime

- **Go 1.23+** (toolchain 1.24.1)
- Module: `claude-squad`
- Binary: `cs`

## Version Control

- **Jujutsu (`jj`)** — we use jj, not git, for all VCS operations.
- The repo itself is a git repo under the hood (jj sits on top), but all developer-facing commands should use `jj`.

## Key Dependencies

| Package | Purpose |
|---------|---------|
| `charmbracelet/bubbletea` | TUI framework (Elm architecture) |
| `charmbracelet/bubbles` | Pre-built TUI components (viewport, textinput, etc.) |
| `charmbracelet/lipgloss` | TUI styling / layout |
| `go-git/go-git` | Git operations (worktree creation, branch ops) |
| `spf13/cobra` | CLI command parsing |
| `creack/pty` | PTY for tmux attach |
| `stretchr/testify` | Test assertions |

## External Tool Requirements

- **tmux** — must be installed on the host. Claude Squad creates/manages tmux sessions.
- **gh** (GitHub CLI) — used for pushing branches and PR creation.
- The AI agent itself (e.g. `claude`, `codex`, `gemini`, `aider`) must be installed separately.

## Configuration

Config file: `~/.claude-squad/config.json`

```json
{
  "default_program": "claude",
  "profiles": [
    { "name": "claude", "program": "claude" },
    { "name": "codex",  "program": "codex" },
    { "name": "aider",  "program": "aider --model ollama_chat/gemma3:1b" }
  ]
}
```

Path can be found via `cs debug`.

## Build

```bash
go build -o cs .
```

## Test

```bash
go test ./...
```

> Python tests (if any introduced) may hang in sandbox environments — run locally.

## CI

GitHub Actions at `.github/workflows/build.yml`. Runs on push/PR to main.

## Versioning

Version is bumped via `bump-version.sh`. Current: **1.0.17**.

## Upstream

Upstream repo: `https://github.com/smtg-ai/claude-squad`

To pull upstream changes:
```bash
jj git fetch --remote upstream
# Then integrate selectively with jj rebase
```
