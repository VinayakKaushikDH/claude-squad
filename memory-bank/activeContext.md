# Active Context

## Current Focus

**jj migration**: Replacing the `session/git/` layer with jujutsu (`jj`) equivalents.
Discovery phase complete — full operations inventory and design doc written at `memory-bank/jj-migration.md`.
**Phase 1 complete** — pure abstraction refactor done; no behavior change, no jj code yet.
**Phase 2 complete** — JJWorkspace implementation done; full test suite passes.

## Recent Changes (from git log)

- Phase 2 implementation: `session/jj/` package (6 files), `session/vcs/detect.go`, storage/instance/main/app wiring
- `a4ab698` — fix: move expensive operations off UI event loop and fix stale preview pane (#253)
- `c4d0c03` — chore: Bump version to 1.0.17
- `52aa2dd` — feat: Allow configuring preset profiles for creating sessions (#264)

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

## Next Steps

- Architect review pending (code-reviewer agent running)
- After review approval: commit, update memory bank, consider end-to-end manual test with `vcs_type: "jj"` config

## Open Questions

- What specific enhancements do we want on top of upstream?
- Which upstream PRs (if any) should we cherry-pick or watch?

## Known Issues / Blockers

None currently identified.
