# Project Brief

## What Is This

Claude Squad (`cs`) is a terminal TUI application that manages multiple AI coding agents simultaneously — Claude Code, Codex, Gemini, Aider, and any other CLI-based agent. Each agent gets its own isolated tmux session and git worktree so parallel work never conflicts.

## Fork Status

This is a **maintained fork** of [smtg-ai/claude-squad](https://github.com/smtg-ai/claude-squad). We track upstream releases and layer our own patches on top.

## Core Goals

1. Let developers run multiple AI agents in parallel without branch conflicts.
2. Provide a clean TUI for navigating, reviewing, committing, and pushing agent work.
3. Stay close to upstream to benefit from community improvements.
4. Apply targeted enhancements for our specific workflow needs.

## Scope

- Terminal-first. No Electron, no heavy GUI.
- Agent-agnostic. Works with any CLI-based coding agent.
- Workspace isolation is a hard requirement — each session must be on its own branch/bookmark, backed by either a git worktree or a jj workspace depending on `vcs_type` config.
- Configuration via `~/.claude-squad/config.json`.

## Out of Scope

- A cloud-hosted version.
- Managing non-coding agents.
- Replacing or wrapping the agents themselves — we just launch and manage them.
