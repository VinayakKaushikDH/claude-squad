# Active Context

## Current Focus

Memory bank system is fully set up. `AGENTS.md`, all 6 `memory-bank/*.md` files, and the `/remember` skill (`~/.claude/skills/remember/SKILL.md`) are in place. Ready for feature work.

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

- [ ] Identify specific patches or divergences from upstream we want to maintain.
- [ ] Document any fork-specific behavior in `systemPatterns.md`.
- [ ] Begin any planned feature work (to be added here as it starts).

## Open Questions

- What specific enhancements do we want on top of upstream?
- Which upstream PRs (if any) should we cherry-pick or watch?

## Known Issues / Blockers

None currently identified.
