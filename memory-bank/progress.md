# Progress

## What Works (as of 2026-04-17, v1.0.17)

- Full TUI session management: create, kill, attach, detach
- Git worktree isolation per session
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

## Known Issues

None currently tracked. Check upstream issues at `https://github.com/smtg-ai/claude-squad/issues` for community-reported bugs.

## Remaining Work / Planned

### jj Migration (in progress — discovery done)
- [ ] `IsJJRepo` startup guard
- [ ] `JJWorkspace` struct: `Setup`, `Cleanup`, `Remove`, `Prune`
- [ ] `IsDirty`, `Diff`, `CommitChanges`, `PushChanges`
- [ ] `SearchBookmarks`, `FetchBookmarks`
- [ ] Remove `IsBranchCheckedOut` from `Resume()` (jj workspaces don't conflict)
- [ ] Storage migration: `GitWorktreeData` → `JJWorkspaceData`
- [ ] Tests

See `memory-bank/jj-migration.md` for full design doc.

## Recent Milestones

| Version | Change |
|---------|--------|
| 1.0.17  | Fix stale preview pane; move expensive ops off UI event loop (#253) |
| 1.0.17  | Configurable preset profiles (#264) |
| 1.0.17  | Selectable source branch for sessions (#262) |
| 1.0.17  | Claude trust prompt fix (#263) |

## Regressions to Watch

- Preview pane performance — was broken before #253. Monitor for recurrence.
- Session restore on restart — verify sessions survive Claude Squad restarts after any storage changes.
