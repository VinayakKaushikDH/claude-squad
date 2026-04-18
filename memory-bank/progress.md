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
- **Post-Phase-6 notification fixes**: auto-acknowledge guard on `prevStatus == session.Running` (was firing every tick); 2-space icon slot padding for acknowledged agents; auto-switch to `currentRepoPath` after disk reload empties active workspace; `SetDetachedSize` caches size to prevent ~2s resize flicker from two processes fighting
- **pi-mono + opencode integration**: `n` launches pi-mono, `b` launches opencode, `m` launches claude; `ProgramOpencode`/`ProgramPiMono` constants strip `CLAUDE_CODE_OAUTH_TOKEN` and `GITHUB_TOKEN`; `resolveBaseProgram()` normalizes `env -u` prefixed commands for prompt detection; latent `HasUpdated()` vs `CheckAndHandleTrustPrompt()` matching inconsistency fixed
- **Atomic delete** (Phase 6): `DeleteInstanceByTitle` in `config/state.go` does read-modify-write under single flock, preventing stale-cache overwrite when multiple `cs` processes coexist
- **Workspace quit guard** (Phase 6): `handleQuit` blocks if active workspace path differs from launch path (`currentRepoPath`)
- **Non-blocking quit save** (Phase 6): `SaveInstancesNonBlocking` uses `LOCK_EX|LOCK_NB`; quit skips save if lock busy rather than blocking
- **ctrl+q bypass** (Phase 6): `KeyQuit` skips `handleMenuHighlighting`'s 2-pass re-send to avoid doubled quit latency
- **Tmux resize caching** (Phase 6): `TmuxSession.lastCols`/`lastRows` fields skip redundant resize calls; cache invalidated by `SetDetachedSize`

## Known Issues

None currently tracked. Check upstream issues at `https://github.com/smtg-ai/claude-squad/issues` for community-reported bugs.

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
