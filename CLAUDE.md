# AGENTS.md — Claude Squad Agent Guide

> **This is a maintained fork of [smtg-ai/claude-squad](https://github.com/smtg-ai/claude-squad).**
> We track upstream and apply our own patches on top. When in doubt, check upstream for context on existing behavior before changing it.

---

## Memory Bank

Agents operating on this repo use a structured **Memory Bank** — a set of markdown files that capture the project's state across sessions. Without it, context resets every conversation. With it, agents pick up exactly where things left off.

The memory bank lives in `memory-bank/`. **Always read it at the start of a session.** Update the relevant files whenever significant changes occur.

### Core Files

```
memory-bank/
├── projectbrief.md      # What this project is and what we want from it
├── productContext.md    # Why it exists, who uses it, UX goals
├── activeContext.md     # ← Update most often. Current focus, recent changes, next steps
├── systemPatterns.md    # Architecture, design patterns, component relationships
├── techContext.md       # Stack, tooling, constraints, dependencies
└── progress.md          # What works, what's broken, what's left
```

### File Purposes

| File | Update When |
|------|-------------|
| `projectbrief.md` | Scope or goals change |
| `productContext.md` | User-facing behavior or product direction shifts |
| `activeContext.md` | Every session — before and after doing work |
| `systemPatterns.md` | New patterns introduced or architectural decisions made |
| `techContext.md` | Dependencies added/removed, tooling changed |
| `progress.md` | Features shipped, bugs fixed, regressions found |

### Workflow

1. **Start of session**: Read all memory bank files to restore context.
2. **During work**: Keep `activeContext.md` updated as you make decisions.
3. **End of session**: Summarize what changed in `activeContext.md` and `progress.md`.
4. **Significant milestones**: Update `systemPatterns.md` or `techContext.md` if the architecture or stack changed.

---

## Version Control: Jujutsu (jj)

**We use [Jujutsu](https://github.com/martinvonz/jj) (`jj`), not git, for version control.**

Key differences from git:

```bash
# Check status
jj status         # (not git status)

# Describe current change
jj describe -m "your message"

# Create a new change (like git commit + checkout new branch)
jj new

# Push
jj git push

# See log
jj log

# Squash staged changes into current change
jj squash

# Abandon a change
jj abandon
```

- Every working-copy change is already a "change" (no staging area dance needed).
- Do not run `git commit`, `git add`, or `git checkout`. Use `jj` equivalents.
- `jj git push` handles pushing to the remote.

---

## Project Overview

**Claude Squad** (`cs`) is a terminal TUI app that manages multiple AI coding agents (Claude Code, Codex, Gemini, Aider) in isolated workspaces simultaneously.

### How It Works

- **tmux** creates isolated terminal sessions per agent
- **git worktrees** isolate each session's codebase on its own branch
- **Bubbletea** drives the TUI (charmbracelet ecosystem)

### Directory Map

```
.
├── main.go            # Entrypoint
├── cmd/               # Cobra CLI commands
├── app/               # Core app logic (Bubbletea model, session orchestration)
│   ├── git/           # Git/worktree operations
│   └── tmux/          # tmux session management
├── session/           # Session state and lifecycle
├── ui/                # TUI components (list, menu, overlay, preview, diff)
├── config/            # Config loading (~/.claude-squad/config.json)
├── daemon/            # Background daemon process
├── keys/              # Keybinding definitions
├── log/               # Logging
└── web/               # Web UI (secondary interface)
```

### Key Concepts

- **Instance**: A single running agent session (tmux + worktree + state)
- **Session**: Persisted instance metadata (stored via `app/storage.go`)
- **Profile**: Named program config in `config.json` (e.g. `claude`, `codex`, `aider`)

---

## Fork Maintenance

This repo is a fork. Our responsibilities:

1. **Track upstream** (`smtg-ai/claude-squad`) for new releases.
2. **Apply upstream changes** that are safe to merge without breaking our patches.
3. **Keep our patches clean** — prefer small, isolated changes over big diffs.
4. **Document divergences** in `memory-bank/activeContext.md` when we intentionally deviate from upstream.

When merging upstream:
```bash
jj git fetch --remote upstream
# Review upstream changes, then integrate via jj rebase or cherry-pick as appropriate
```

---

## Build & Run

```bash
go build -o cs .
./cs
```

Run tests:
```bash
go test ./...
```

> Note: Python tests (if any) may fail or hang in sandbox environments — run them locally.

---

## Code Conventions

- **Go** — standard formatting via `gofmt`. No custom linter config; keep it idiomatic.
- **Bubbletea** — follow the Elm architecture: `Model`, `Update`, `View`. Don't side-effect in `View`.
- **Error handling** — return errors up the stack; don't panic outside of initialization.
- **No unnecessary abstractions** — add helpers only when used in 3+ places.

---

## Agent Instructions

- Read `memory-bank/activeContext.md` first, every session.
- Prefer editing existing files over creating new ones.
- Do not use `git`; use `jj` for all VCS operations.
- Do not add features beyond what was asked.
- Do not add comments unless the logic is non-obvious.
- After completing work, update `memory-bank/activeContext.md` and `memory-bank/progress.md`.
