# Active Context

## Current Focus

**Phase 6 complete + pi-mono/opencode integration added**: program-specific launch keys (`n`/`b`/`m`) implemented; `resolveBaseProgram()` normalization in `session/tmux/tmux.go`; env stripping for pi/opencode launch commands confirmed working.

## Recent Changes (from git log)

- **pi-mono/opencode integration**: `n` launches pi-mono, `b` launches opencode, `m` launches claude; `KeyNew` replaced with `KeyNewPiMono`/`KeyNewOpencode`/`KeyNewClaude` in `keys/keys.go`; `ProgramOpencode`/`ProgramPiMono` constants added to `session/tmux/tmux.go`; `resolveBaseProgram()` helper normalizes program matching for `HasUpdated()` and `CheckAndHandleTrustPrompt()`; launch commands strip `CLAUDE_CODE_OAUTH_TOKEN` and `GITHUB_TOKEN`
- Phase 6 post-compaction fixes: auto-acknowledge was firing on every tick (not just Running→Ready transition); blank icon slot (need 2-space padding not empty string); cross-instance workspace hang (auto-switch to `currentRepoPath` after merge if active workspace is empty); `SetDetachedSize` flicker (cache inside `SetDetachedSize`, never invalidate)
- Phase 6: notification system redesign — `ReadyAcknowledged` persisted, auto-ack when in workspace, separate tab badge / blink control, 1s disk reload
- Phase 6 bug fixes: blink persists after Enter (render never checked flag), ctrl+q slow (key went through 2-pass menu highlighting), workspace quit guard, cross-instance kill overwrote disk with stale cache (`DeleteInstanceByTitle` atomic fix), window shrinks 2s (tmux resize caching via `lastCols`/`lastRows`)
- Phase 5: disk reload (`mergeReloadedInstances`, `ReloadAndParse`), `ReadyAcknowledged` + blink UI, workspace fixes, tmux resize polling
- Phase 4: multi-workspace tabs (`[`/`]` keys), filtered list view, OS notifications (macOS/Linux), green tab highlight for Ready workspaces
- JJ checkout feature: `CheckoutInMainRepo()` + `ErrCheckoutRequiresPause` sentinel, jj snapshots + edits in place
- Phase 2 implementation: `session/jj/` package (6 files), `session/vcs/detect.go`, storage/instance/main/app wiring

Current version: **1.0.17** (Phase 6 changes committed, not yet pushed)

## Active Decisions

- We use **jujutsu (`jj`)** for version control, not git. All VCS commands should use `jj`.
- This is a fork of `smtg-ai/claude-squad`. Track upstream; do not diverge silently.
- Memory bank initialized on 2026-04-17.
- Workspaces are a **view-layer concept only** — derived at runtime from `Instance.Path`, no storage schema changes. The filtered-view pattern on `List` is the single source of truth.

## Completed Work

### Phase 1 — Refactor (no jj code, pure abstraction):
1. [x] Move `DiffStats` to `session/vcs/types.go`
2. [x] Define `Workspace` interface in `session/vcs/workspace.go`
3. [x] Add `CanResume()`/`CanRemove()` to `GitWorktree`, absorb `Prune()` into `Remove()` internally
4. [x] Change `instance.go`: `gitWorktree *git.GitWorktree` → `workspace vcs.Workspace`
5. [x] Add `Instance.CanKill()` + `Instance.PushChanges()`; delete `GetGitWorktree()`
6. [x] Update `app.go` kill/push to use new `Instance` methods; `*git.DiffStats` → `*vcs.DiffStats`
7. [x] Add `VCSType` to config + storage; `json.RawMessage` dispatch
8. [x] Tests pass — pure refactor, no behavior change

### Phase 2 — jj implementation:
9. [x] Implement `session/jj/JJWorkspace` (all `Workspace` interface methods)
10. [x] Add `vcs.IsRepo`, `jj.FetchBookmarks`, `jj.SearchBookmarks`, `jj.CleanupWorkspaces`
11. [x] Wire into `Instance.Start()`, `main.go`, `app.go`
12. [x] jj-specific tests (13 tests, real jj repos)

### Phase 3 — jj checkout feature:
13. [x] Added `CheckoutInMainRepo() error` to `vcs.Workspace` interface + `ErrCheckoutRequiresPause` sentinel in `session/vcs/workspace.go`
14. [x] Git implementation returns `ErrCheckoutRequiresPause` (existing pause/resume flow unchanged)
15. [x] JJ implementation: snapshot dirty workspace (describe + move bookmark to `@`), then `jj edit <bookmark>` in main repo
16. [x] `app.go` `KeyCheckout` handler branches on `ErrCheckoutRequiresPause` — git pauses, jj checks out non-disruptively
17. [x] `helpTypeJJCheckout` added in `app/help.go` — explains agent keeps running, bookmark copied to clipboard
18. [x] Bookmark names use exact session title (no `BranchPrefix` prefix, no hex timestamp suffix in workspace path)
19. [x] 6 integration tests in `session/jj/workspace_test.go` covering checkout bugs (dirty snapshot ordering, false dirty-guard, `@` vs `@-` placement, no random names)
20. [x] 4 unit tests in `session/instance_test.go` covering `CheckoutInMainRepo` delegation; mock made configurable via `checkoutErr` field

### Phase 4 — Multi-workspace tabs & OS notifications:
21. [x] `ui/tab_styles.go` — shared tab border styles extracted (exports: `ActiveTabStyle`, `InactiveTabStyle`, `HighlightColor`)
22. [x] `app/workspace.go` — `DeriveWorkspaces()`, `FindWorkspaceIndex()`; groups by Path, disambiguates basename collisions, sets HasReady
23. [x] `ui/list.go` — filtered-view refactor: `filterPath`, `filteredIdxs`, `selectionMemo`; `SetFilter()`, `GetVisibleInstances()`, `NumAllInstances()`
24. [x] `ui/workspace_tabs.go` — `WorkspaceTabBar` with sliding window (maxVisibleTabs=7), notify-highlighted tab style (green #51bd73)
25. [x] `notify/notify.go` — `Send()` for macOS (osascript) and Linux (notify-send), async via `tea.Cmd`
26. [x] `session/instance.go` — `NotifiedReady bool` field added
27. [x] `config/config.go` — `Notifications *bool` + `GetNotifications()` (default: true)
28. [x] `keys/keys.go` + `ui/menu.go` — `[`/`]` workspace navigation keys
29. [x] `app/app.go` — workspace fields, `refreshWorkspaces()`, `syncWorkspaceUI()`, tab bar rendering, notification dispatch with dedup
30. [x] Bug fix: `NotifiedReady` reset only in `SendPrompt()`, not on `r.updated` tmux fluctuations
31. [x] Bug fix: new instances always use `currentRepoPath` (launch dir), not active tab path; `activeWorkspacePath()` removed

## Next Steps

- Update idle prompt detection strings for opencode and pi-mono in `session/tmux/tmux.go` — currently placeholder `">"`, needs the actual idle prompt text from each program
- Push Phase 6 + pi/opencode integration commits (`jj git push`)
- **Phase 7 — Async preview/terminal rendering**: `instanceChanged()` currently runs synchronous `tmux capture-pane` subprocesses inside Bubbletea's `Update` handler, blocking key processing (including Ctrl+Q) for 50–200ms+ per call. Fix: move tmux captures into background `tea.Cmd` goroutines that write to a cache; `instanceChanged()` reads the cache (never blocks). This eliminates the main source of Ctrl+Q sluggishness and preview tick jank.
- Cross-process notification sync: `mergeReloadedInstances` now propagates `ReadyAcknowledged` from disk (committed but not pushed).
- Consider adding `TmuxAlive()` guard in `snapshotActiveInstances` to skip dead instances and prevent error-flood / false-Ready status on orphaned sessions
- Consider jj error message normalization for the TUI (noted for future; currently raw passthrough)
- `SaveInstances` (bulk save) still uses the old stale-cache pattern; only `DeleteInstanceByTitle` is atomic. Low-priority since deletes are the main conflict path.

## Open Questions

- Which upstream PRs (if any) should we cherry-pick or watch?

## Known Issues / Blockers

- Orphaned instances with dead tmux sessions cause `capture-pane` exit-status-1 errors every ~500ms, growing the log file rapidly and triggering spurious Ready notifications. Workaround: `cs reset` or clear `~/.claude-squad/state.json`. Permanent fix: add `TmuxAlive()` guard.
