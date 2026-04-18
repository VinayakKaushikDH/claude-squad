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

The workspace is abstracted behind a `Workspace` interface (`session/vcs/workspace.go`). `DiffStats` lives in `session/vcs/types.go`. Two implementations: `session/git/GitWorktree` and `session/jj/JJWorkspace`. `instance.go` holds a `workspace vcs.Workspace` field — not a concrete type. `app.go` never touches the concrete workspace type; it calls `instance.CanKill()`, `instance.PushChanges()`, and `instance.CheckoutInMainRepo()` which delegate to the interface. Serialization in `ToInstanceData()` uses a type assertion `i.workspace.(*git.GitWorktree)` to access git-specific getters — this is the only place the concrete type leaks. `Remove()` on `GitWorktree` calls `Prune()` internally; callers must not call `Prune()` separately.

**VCS-branching via sentinel error**: `CheckoutInMainRepo()` in the git backend returns `vcs.ErrCheckoutRequiresPause`; the jj backend performs the checkout directly. `app.go`'s `KeyCheckout` handler uses `errors.Is(err, vcs.ErrCheckoutRequiresPause)` to dispatch: git takes the existing pause/resume flow, jj shows a non-disruptive confirmation (agent keeps running). New VCS behaviors that diverge between backends should follow this sentinel-error pattern rather than adding VCS type-checks in `app.go`.

### 3. Session Persistence

Sessions are stored to disk via `app/storage.go`. On startup, stored sessions are restored. This allows Claude Squad to survive restarts without losing track of running agents.

### 4. Workspace Isolation

Every new session creates an isolated workspace on a fresh branch/bookmark. With git: a git worktree via `session/git/`. With jj: a jj workspace via `session/jj/`. Both land under `~/.claude-squad/worktrees/`. The branch/bookmark is named after the session (sanitized). jj workspaces do NOT create git worktrees — they are a jj-only concept (`jj workspace add` produces only a `.jj/` dir, not a `.git` file).

### 5. Preview Pane (async)

The preview pane (`app/preview.go`) captures tmux output asynchronously. Expensive operations (diffing, capturing pane content) are done off the UI event loop to avoid blocking redraws — see fix in commit `a4ab698`.

### 6. Profiles

Profiles are named launch configurations in `config.json`. The overlay in `ui/overlay/` shows a profile picker when more than one profile is defined. Profiles decouple "what agent to launch" from session creation.

### 10. Program-Specific Launch Keys (post-Phase-6)

`KeyNew` was replaced with three dedicated keys: `n` (pi-mono), `b` (opencode), `m` (claude). `N` still opens the full profile/branch picker. The program constants (`ProgramOpencode`, `ProgramPiMono`) in `session/tmux/tmux.go` include `env -u` prefixes — `resolveBaseProgram()` strips `env` and its flags to get the bare binary name for prompt detection. When adding a new `env -u VAR program` constant, verify `resolveBaseProgram()` handles the prefix correctly or it will match `"env"` instead of the actual program and break `HasUpdated()` / `CheckAndHandleTrustPrompt()`.

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

### 7. Testing jj Code

Mock-based unit tests (e.g., `mockWorkspace` in `instance_test.go`) cannot catch jj command behavior bugs — they pass even when the real implementation is broken. Any new jj workspace behavior must be covered by integration tests in `session/jj/workspace_test.go` that spin up a real jj repo (using `setupTestJJRepo` helper). The 6 checkout tests are the reference pattern.

### 8. Workspace Tab Architecture (Phase 4)

Workspaces are a **view-layer concept only** — derived at runtime from `Instance.Path` via `app/workspace.go:DeriveWorkspaces()`. No storage schema changes; the daemon is untouched. The filtered-view pattern in `ui/list.go` (`filterPath` + `filteredIdxs` + `selectionMemo`) is the single source of truth: `GetInstances()` always returns all items (used by `SaveInstances`), while `NumInstances()` returns the filtered count. Global limits (10-instance cap) must use `NumAllInstances()`.

The `ui` package must not import `app` — circular dependency. UI types that mirror app structs (e.g., `WorkspaceTab` vs `app.Workspace`) must be defined independently in the `ui` package.

`NotifiedReady` on `Instance` must only be reset on user-initiated actions (`SendPrompt()`), never on automated metadata poll callbacks (`r.updated` branch). Resetting on tmux output fluctuations causes Running→Ready→Running→Ready cycles that spam notifications.

### 9. Notification Acknowledgment Model (Phase 6)

Tab badge and individual agent blink/dot are controlled by **two separate mechanisms** — do not collapse them into one:

| Signal | Controls | Set when | Cleared when |
|--------|----------|----------|--------------|
| `ReadyAcknowledged` (persisted in `InstanceData`) | Agent icon blink + green dot | Enter pressed on agent | Agent resumes (goes back to Running) |
| `activeWorkspacePath` param in `DeriveWorkspaces` | Tab badge (`HasReady`) suppressed for the active tab | Derived from current `m.workspaces[m.activeWorkspace]` | N/A — always computed fresh |

`ReadyAcknowledged` is **persisted to disk** (field in `InstanceData` JSON) so cross-process acknowledgment propagates via the existing disk reload mechanism (~1 second). Only Enter (`app.go` attach path) sets `ReadyAcknowledged` — workspace switching and being in the same workspace while an agent becomes Ready must NOT auto-acknowledge. That auto-ack was tried and removed because it silently suppressed notifications the user still needed to see.

`DeriveWorkspaces(instances []Instance, activeWorkspacePath string)` — the second parameter suppresses the `HasReady` badge for the workspace the user is currently viewing. Test callers pass `""` for no suppression.

The disk reload counter threshold is 2 ticks (~1 second) not 10 (~5 seconds) — changed to make cross-process acknowledgment feel near-instant.

The auto-acknowledge in `metadataUpdateDoneMsg` must only fire on the Running→Ready transition (`prevStatus == session.Running`), NOT on every tick when `isViewingWorkspace` is true. Firing on every tick was a bug that caused workspace switch to clear all agent notifications.

When `ReadyAcknowledged` is true, the Render function must emit `"  "` (2 spaces), not `""`, to preserve the icon slot width — an empty string causes the selected-item background highlight to be shorter than other rows.

After `mergeReloadedInstances`, if the active workspace has no remaining instances (e.g. another process killed them all), auto-switch to `currentRepoPath` to avoid an empty/stuck view.

`TmuxSession.lastCols`/`lastRows` are shared between the `monitorWindowSize` goroutine (writes full-terminal dims every 2s while attached) and `SetDetachedSize` (called from the Bubbletea main loop). These fields are protected by `sizeMu sync.Mutex`. On `Detach()`, reset both to 0 so the first `SetDetachedSize` call after return always issues a resize to preview dims. `SetDetachedSize` caches and skips the tmux call when dims are unchanged — do NOT reset to 0 from inside `SetDetachedSize`; reset only from `Detach()`.

### 11. Ctrl+Q Detection in stdin Goroutine (`tmux.go`)

The stdin relay goroutine detects Ctrl+Q by scanning the **full read buffer** for byte `0x11`, not by requiring `nr == 1`. Pi/opencode enable mouse reporting (SGR mode), so stdin is flooded with multi-byte escape sequences; Ctrl+Q arrives bundled with other bytes (`nr > 1`) and the single-byte check fails silently — the user sees a slow/frozen detach that only fires when a quiet moment happens to land the byte alone. Any new detach-key detection must scan the whole buffer.

### 13. PTY Master fd Must Be Non-Blocking (`tmux_unix.go` + `tmux.go`)

`creack/pty`'s `pty.Start()` opens the PTY master fd in blocking mode. Go's `os.File.Close()` for non-netpoller (blocking) files calls `runtime_Semacquire` which waits for all in-progress `Read` calls to return before closing the fd. The `io.Copy(os.Stdout, t.ptmx)` goroutine blocks indefinitely in `Read` when the agent is idle — so `t.ptmx.Close()` in `Detach()` never returns, causing an infinite deadlock. Fix: `makeNonBlockingFile()` in `tmux_unix.go` dups the fd, calls `syscall.SetNonblock`, and wraps with `os.NewFile`; Go 1.23+ detects `O_NONBLOCK` and registers the fd with the netpoller, making `Close()` call `pd.evict()` which immediately unparks the blocked goroutine. This function must be called immediately after `pty.Start()` in `Attach()` before storing `t.ptmx`.

### 12. Terminal State Save/Restore on Attach/Detach (`tmux.go`)

Programs like pi and opencode enable extended key modes (`\033[>4;1m`, kitty `\033[=1u`) and mouse reporting via the PTY. These live in the terminal emulator's application state, not the kernel tty, so `tcsetattr` cannot undo them. Pattern: call `term.GetState` to save tty attrs before creating the PTY in `Attach()`; after `wg.Wait()` in `Detach()`, call `term.Restore` then send explicit ANSI reset sequences (`\033[>4;0m`, `\033[=0u`, mouse disable, bracketed-paste disable). Skipping this leaves the outer TUI terminal dirty and produces "extended keys are on" warnings.

## Invariants

- One tmux session per instance.
- One git worktree per instance.
- Session list is the single source of truth for active instances.
- UI never blocks on I/O — all slow ops go through tea.Cmd or goroutines.
