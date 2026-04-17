# System Patterns

## Architecture Overview

Claude Squad follows a **Bubbletea (Elm-style) TUI architecture** in Go.

```
main.go
  └── cmd/          (Cobra CLI entry — parses flags, calls app.Run)
       └── app/     (Bubbletea Model — owns the event loop)
            ├── app.go         Main model: Init, Update, View
            ├── instance.go    Instance lifecycle (create, kill, attach)
            ├── storage.go     Persist/restore sessions to disk
            ├── help.go        Help overlay
            ├── git/           Git worktree creation and branch ops
            └── tmux/          tmux session create/attach/kill
```

## Key Design Patterns

### 1. Bubbletea Elm Architecture

All UI state lives in the `Model` struct in `app/app.go`. Side effects go through `tea.Cmd` returns from `Update`. **Never side-effect in `View`.**

```go
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) { ... }
func (m Model) View() string { ... }
```

### 2. Instance = tmux session + workspace + metadata

Each running agent is an `Instance` (defined in `app/instance.go`). It encapsulates:
- The tmux session name
- The workspace path and branch/bookmark (git worktree or jj workspace)
- The program being run (from profile config)
- Status (running, paused, done)

The workspace is abstracted behind a `Workspace` interface (`session/workspace.go`). Two implementations: `session/git/GitWorktree` and `session/jj/JJWorkspace`. `instance.go` holds a `workspace Workspace` field — not a concrete type. `app.go` never touches the concrete workspace type; it calls `instance.CanKill()` and `instance.PushChanges()` which delegate to the interface.

### 3. Session Persistence

Sessions are stored to disk via `app/storage.go`. On startup, stored sessions are restored. This allows Claude Squad to survive restarts without losing track of running agents.

### 4. Workspace Isolation

Every new session creates an isolated workspace on a fresh branch/bookmark. With git: a git worktree via `session/git/`. With jj: a jj workspace via `session/jj/`. Both land under `~/.claude-squad/worktrees/`. The branch/bookmark is named after the session (sanitized). jj workspaces do NOT create git worktrees — they are a jj-only concept (`jj workspace add` produces only a `.jj/` dir, not a `.git` file).

### 5. Preview Pane (async)

The preview pane (`app/preview.go`) captures tmux output asynchronously. Expensive operations (diffing, capturing pane content) are done off the UI event loop to avoid blocking redraws — see fix in commit `a4ab698`.

### 6. Profiles

Profiles are named launch configurations in `config.json`. The overlay in `ui/overlay/` shows a profile picker when more than one profile is defined. Profiles decouple "what agent to launch" from session creation.

## Component Relationships

```
ui/list.go       ← renders session list
ui/preview.go    ← renders preview pane output
ui/overlay/      ← new session, help, profile picker overlays
ui/menu.go       ← bottom keybinding bar
app/app.go       ← wires everything together, owns Model
app/instance.go  ← one instance per agent session
app/git/         ← worktree create/delete
app/tmux/        ← tmux session create/attach/kill
config/          ← loads ~/.claude-squad/config.json
daemon/          ← background daemon for auto-accept mode
```

## Invariants

- One tmux session per instance.
- One git worktree per instance.
- Session list is the single source of truth for active instances.
- UI never blocks on I/O — all slow ops go through tea.Cmd or goroutines.
