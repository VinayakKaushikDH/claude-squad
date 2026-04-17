# Active Context

## Current Focus

**jj checkout feature**: JJ-native checkout (`c` key) implemented and working. When `c` is pressed on a jj session, the bookmark is checked out in the main repo via `jj edit <bookmark>` without pausing the agent session.

## Recent Changes (from git log)

- JJ checkout feature: added `CheckoutInMainRepo()` to `vcs.Workspace` interface + `ErrCheckoutRequiresPause` sentinel, git returns sentinel (existing pause flow), jj snapshots + edits in place
- Phase 2 implementation: `session/jj/` package (6 files), `session/vcs/detect.go`, storage/instance/main/app wiring
- `a4ab698` — fix: move expensive operations off UI event loop and fix stale preview pane (#253)

Current version: **1.0.17**

## Active Decisions

- We use **jujutsu (`jj`)** for version control, not git. All VCS commands should use `jj`.
- This is a fork of `smtg-ai/claude-squad`. Track upstream; do not diverge silently.
- Memory bank initialized on 2026-04-17.

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

## Next Steps

- Consider jj error message normalization for the TUI (noted for future; currently raw passthrough)

## Open Questions

- What specific enhancements do we want on top of upstream?
- Which upstream PRs (if any) should we cherry-pick or watch?

## Known Issues / Blockers

None currently identified.
