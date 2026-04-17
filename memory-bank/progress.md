# Progress

## What Works (as of 2026-04-17, v1.0.17)

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

## Known Issues

None currently tracked. Check upstream issues at `https://github.com/smtg-ai/claude-squad/issues` for community-reported bugs.

## Remaining Work / Planned

### jj Migration — Complete (Phase 1 + Phase 2 done)

Phase 1 (pure abstraction refactor): done.
Phase 2 (jj implementation): done.

Remaining polish:
- [ ] End-to-end manual test with `vcs_type: "jj"` config
- [ ] Consider jj error message normalization for the TUI (noted for future)

## Recent Milestones

| Version | Change |
|---------|--------|
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
