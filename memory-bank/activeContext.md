# Active Context

## Current Focus

**jj migration**: Replacing the `session/git/` layer with jujutsu (`jj`) equivalents.
Discovery phase complete — full operations inventory and design doc written at `memory-bank/jj-migration.md`.
Implementation not started.

## Recent Changes (from git log)

- `a4ab698` — fix: move expensive operations off UI event loop and fix stale preview pane (#253)
- `c4d0c03` — chore: Bump version to 1.0.17
- `52aa2dd` — feat: Allow configuring preset profiles for creating sessions (#264)
- `166112a` — fix: Update claude trust prompt handling (#263)
- `b4b43ab` — feat: Enable selecting source branch for session (#262)

Current version: **1.0.17**

## Active Decisions

- We use **jujutsu (`jj`)** for version control, not git. All VCS commands should use `jj`.
- This is a fork of `smtg-ai/claude-squad`. Track upstream; do not diverge silently.
- Memory bank initialized on 2026-04-17.

## Next Steps

Open questions fully resolved — see `memory-bank/jj-migration.md` for details.
Architecture decided: `vcs.Workspace` interface + two implementations (`GitWorktree`, `JJWorkspace`).
Config opt-in via `vcs_type: "jj"`. No session migration needed.

Two-phase implementation:

**Phase 1 — Refactor (no jj code, pure abstraction):**
1. [ ] Move `DiffStats` to `session/workspace_types.go`
2. [ ] Define `Workspace` interface in `session/workspace.go`
3. [ ] Add `CanResume()`/`CanRemove()` to `GitWorktree`, absorb `Prune()` into internals
4. [ ] Change `instance.go`: `gitWorktree` → `workspace Workspace`
5. [ ] Add `Instance.CanKill()` + `Instance.PushChanges()`; delete `GetGitWorktree()`
6. [ ] Update `app.go` kill/push to use new `Instance` methods
7. [ ] Add `VCSType` to config + storage; `json.RawMessage` dispatch
8. [ ] Tests pass — pure refactor, no behavior change

**Phase 2 — jj implementation:**
9. [ ] Implement `session/jj/JJWorkspace`
10. [ ] Add `DetectVCSType`, `vcs.IsRepo`, `vcs.CleanupWorkspaces`, `vcs.FetchRefs`, `vcs.SearchRefs`
11. [ ] Wire into `Instance.Start()`, `main.go`, `app.go`
12. [ ] jj-specific tests

## Open Questions

- What specific enhancements do we want on top of upstream?
- Which upstream PRs (if any) should we cherry-pick or watch?

## Known Issues / Blockers

None currently identified.
