# Progress

## What Works (as of 2026-04-18, v1.0.17+)

- Full TUI session management: create, kill, attach, detach
- Git worktree isolation per session
- **jj workspace isolation per session** (new — Phase 2)
- tmux session lifecycle management
- Preview pane with async capture (fixed in #253 — no longer blocks UI)
- Diff view for reviewing changes before push
- Submit (`s`): commit + push branch to GitHub
- Checkout/pause (`c`) and resume (`r`)
- Profiles: named program configurations with picker overlay
- Source branch selection when creating sessions (#262)
- Auto-accept mode (`-y` / `--autoyes`) for yolo workflows
- Config at `~/.claude-squad/config.json`
- Works with: Claude Code, Codex, Gemini, Aider
- **VCS-agnostic workspace interface** (`vcs.Workspace`) with git and jj backends
- **Config-driven VCS selection** (`vcs_type: "jj"` in config.json)
- **JJ-native checkout** (`c` key): checks out bookmark in main repo via `jj edit` without pausing the agent session; git retains existing pause/resume flow
- **Integration tests for jj checkout** (`session/jj/workspace_test.go`): 6 real-jj-repo tests covering dirty workspace snapshot, false dirty-guard regression, bookmark placement, agent-continues post-checkout, and no random names
- **Multi-workspace tabs** (Phase 4): `[`/`]` keys switch between workspaces derived from launch directory; filtered instance list per tab; green tab highlight for workspaces with Ready agents
- **OS notifications** (Phase 4): macOS (osascript) and Linux (notify-send) alerts on Running→Ready transition; dedup via `NotifiedReady` flag; configurable via `"notifications"` in config.json
- **Notification acknowledgment system** (Phase 6): `ReadyAcknowledged` persisted in `InstanceData` JSON for cross-process sync; auto-acknowledged when agent becomes Ready while user is in that workspace; tab badge suppressed for active workspace via `DeriveWorkspaces(instances, activeWorkspacePath)`; acknowledged agent shows no icon (dot removed entirely); blink stops only on Enter (not on Up/Down navigation or workspace switch)
- **Post-Phase-6 notification fixes**: auto-acknowledge guard on `prevStatus == session.Running` (was firing every tick); 2-space icon slot padding for acknowledged agents; auto-switch to `currentRepoPath` after disk reload empties active workspace; `SetDetachedSize` caches size (with `sizeMu` mutex) and resets to 0 on `Detach()` to prevent ~2s resize flicker; auto-ack on workspace view removed (was suppressing notifications user still needed)
- **Ctrl+Q detach latency eliminated**: three root causes fixed — (1) post-detach callback no longer calls `m.instanceChanged()` synchronously (was running `tmux capture-pane -J` blocking Bubbletea render, ~10–100ms every detach); (2) `monitorWindowSize` goroutines removed from `t.wg` (was causing `Detach()`'s `wg.Wait()` to stall ~50ms probabilistically if 2s poll goroutine was mid-`tmux resize-window`); (3) **actual root cause**: `makeNonBlockingFile()` in `tmux_unix.go` called from `Attach()` in `tmux.go` — PTY master from `creack/pty` is blocking mode, causing `t.ptmx.Close()` in `Detach()` to deadlock indefinitely when agent is idle (Go's `runtime_Semacquire` waits for the `io.Copy` Read to return). Fix: dup fd + `SetNonblock` + `os.NewFile` registers fd with the netpoller so `pd.evict()` on Close unparks the blocked goroutine immediately. Preview now refreshes via 100ms `previewTickMsg` loop.
- **pi-mono + opencode Ctrl+Q + terminal leakage fixes**: stdin goroutine scans full buffer for `0x11` (was `nr==1` check — failed when mouse events bundled with keypress); `term.GetState`/`term.Restore` + explicit ANSI disable sequences on detach fix "extended keys are on" terminal state leakage from pi/opencode
- **pi-mono + opencode integration**: `n` launches pi-mono, `b` launches opencode, `m` launches claude; `ProgramOpencode`/`ProgramPiMono` constants strip `CLAUDE_CODE_OAUTH_TOKEN` and `GITHUB_TOKEN`; `resolveBaseProgram()` normalizes `env -u` prefixed commands for prompt detection; latent `HasUpdated()` vs `CheckAndHandleTrustPrompt()` matching inconsistency fixed
- **Atomic delete** (Phase 6): `DeleteInstanceByTitle` in `config/state.go` does read-modify-write under single flock, preventing stale-cache overwrite when multiple `cs` processes coexist
- **Workspace quit guard** (Phase 6): `handleQuit` blocks if active workspace path differs from launch path (`currentRepoPath`)
- **Non-blocking quit save** (Phase 6): `SaveInstancesNonBlocking` uses `LOCK_EX|LOCK_NB`; quit skips save if lock busy rather than blocking
- **ctrl+q bypass** (Phase 6): `KeyQuit` skips `handleMenuHighlighting`'s 2-pass re-send to avoid doubled quit latency
- **Tmux resize caching** (Phase 6): `TmuxSession.lastCols`/`lastRows` fields skip redundant resize calls; cache invalidated by `SetDetachedSize`

- **jj workspace robustness (2026-04-18)**: 8 bugs fixed across `session/jj/`:
  - `diff.go`: `Diff()` now snapshots WC with `jj status` before `jj diff --ignore-working-copy` — eliminates stale diff panel
  - `util.go`: `sanitizeBookmarkName` replaces `/` with `-` — prevents broken workspace paths with intermediate directories
  - `workspace_ops.go`: `jj log -r @-` (baseChangeID capture) now uses `runJJCommandWithRetry` in both `setupNewWorkspace` and `setupFromExistingBookmark`
  - `workspace_ops.go`: bookmark deletion error check changed from broad `"Bookmark"` to targeted `"No such bookmark"`
  - `workspace_jj.go`: `CommitChanges` sets bookmark to `@` BEFORE `jj new` (was after, causing orphan on failure)
  - `workspace_jj.go`: `CheckoutInMainRepo` now heals main repo staleness before `jj edit` — was the primary source of user-reported stale errors
  - `workspace_jj.go`: `update-stale` errors in `CheckoutInMainRepo` now logged instead of silently discarded
  - `refs.go`: `CleanupWorkspaces` now calls `jj workspace forget` for each workspace before deleting its directory
- **`SyncFromMainRepo()` (2026-04-18)**: new operation in `session/jj/workspace_jj.go` and `session/instance.go`. Called on Enter (attach) in `app/app.go`. Snapshots user edits from main repo into the checked-out commit, then `workspace update-stale` in agent workspace — agent sees user's amendments without any new commit being created. 1 integration test added (`TestSyncFromMainRepo`).

## Known Issues

- **Idle prompt strings for pi-mono and opencode** are placeholders (`">"`); real idle prompt text not yet confirmed. Until fixed, `HasUpdated()` may not correctly detect when these agents are idle.
- Orphaned instances with dead tmux sessions cause `capture-pane` exit-status-1 errors every ~500ms. Workaround: `cs reset`. Permanent fix: `TmuxAlive()` guard in `snapshotActiveInstances`.
- **Session name collision**: if two sessions produce the same sanitized bookmark name, `setupNewWorkspace` will call `workspace forget` + `os.RemoveAll` on the first session's directory while it is still running. No collision detection exists yet.
- **Workspace directory not verified on restore**: `FromInstanceData()` → `Start(false)` does not check that the workspace directory exists on disk. If the directory was deleted externally, all subsequent metadata ops fail silently.
- **Fragile stale error detection**: `isStaleError()` matches on literal substrings `"working copy is stale"` and `"workspace update-stale"`. If jj changes wording across versions, stale recovery silently breaks.

## Remaining Work / Planned

### jj Migration — Complete (Phase 1 + Phase 2 + Phase 3 done)

Phase 1 (pure abstraction refactor): done.
Phase 2 (jj implementation): done.
Phase 3 (jj checkout feature): done.

### Phase 4 — Multi-Workspace Tabs & Notifications — Complete

Remaining polish:
- [ ] Add `TmuxAlive()` guard in `snapshotActiveInstances` to skip dead tmux sessions (prevents error-flood + false-Ready notifications for orphaned instances)
- [ ] Consider jj error message normalization for the TUI (noted for future)

## Recent Milestones

| Version | Change |
|---------|--------|
| 1.0.17  | Phase 6: notification ack system (persisted ReadyAcknowledged, auto-ack, active-tab badge suppression, dot removed on ack, Enter-only); atomic delete; workspace quit guard; non-blocking quit save; ctrl+q bypass; tmux resize caching |
| 1.0.17  | Phase 4: multi-workspace tabs (`[`/`]`), filtered list, OS notifications, green Ready highlight; bug fixes for notification spam + workspace path |
| 1.0.17  | jj checkout tests: 6 integration tests (real jj repos) + 4 unit tests; mock `checkoutErr` made configurable |
| 1.0.17  | jj checkout: `c` key checks out bookmark via `jj edit` without pausing agent; git unchanged |
| 1.0.17  | jj Migration Phase 2: JJWorkspace implementation + full wiring |
| 1.0.17  | jj Migration Phase 1: vcs.Workspace interface + GitWorktree refactor |
| 1.0.17  | Fix stale preview pane; move expensive ops off UI event loop (#253) |
| 1.0.17  | Configurable preset profiles (#264) |
| 1.0.17  | Selectable source branch for sessions (#262) |
| 1.0.17  | Claude trust prompt fix (#263) |

## Regressions to Watch

- Preview pane performance — was broken before #253. Monitor for recurrence.
- Session restore on restart — verify sessions survive Claude Squad restarts after any storage changes.
- jj lock contention — multiple concurrent agent workspaces may hit file lock conflicts. Retry-with-backoff is in place but monitor for real-world failures.
