# Product Context

## Problem It Solves

AI coding agents are powerful but sequential by default. A developer typically runs one agent at a time, waiting for it to finish before starting the next task. Claude Squad removes that bottleneck by letting you run many agents at once, each on its own branch, with a single TUI to monitor and manage them all.

## Target Users

- Developers using Claude Code, Codex, Gemini, or Aider heavily.
- People who parallelize work across multiple repos or branches.
- Power users comfortable in the terminal who don't want to leave it.

## User Experience Goals

- **Zero friction startup**: `cs` opens the TUI immediately. Creating a new session is one keypress (`n` or `N`).
- **Non-blocking**: Agents run in the background. The user can switch between them, review diffs, or start new ones without interrupting running agents.
- **Safe review before push**: The diff view and `s` (submit/push) workflow keeps the human in the loop.
- **Yolo mode for trust**: `-y` / `--autoyes` lets experienced users skip confirmation prompts entirely.

## Key User Flows

1. **Start a session**: `n` → optional prompt → agent launches in tmux + worktree.
2. **Monitor progress**: Preview pane shows live output; diff tab shows pending changes.
3. **Attach to reprompt**: `Enter/o` drops into the tmux session for follow-up instructions.
4. **Submit work**: `s` commits and pushes the branch to GitHub.
5. **Checkout / pause**: `c` commits changes and pauses the session for later.
6. **Resume**: `r` resumes a paused session.

## Pain Points to Avoid

- Long startup times (agents should launch quickly).
- Confusing state when a session dies unexpectedly.
- Branch conflicts when multiple agents touch the same files.
