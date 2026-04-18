# Tech Context

## Language & Runtime

- **Go 1.23+** (toolchain 1.24.1)
- Module: `claude-squad`
- Binary: `cs`

## Version Control

- **Jujutsu (`jj`)** — we use jj, not git, for all VCS operations.
- The repo itself is a git repo under the hood (jj sits on top), but all developer-facing commands should use `jj`.

## Key Dependencies

| Package | Purpose |
|---------|---------|
| `charmbracelet/bubbletea` | TUI framework (Elm architecture) |
| `charmbracelet/bubbles` | Pre-built TUI components (viewport, textinput, etc.) |
| `charmbracelet/lipgloss` | TUI styling / layout |
| `go-git/go-git` | Git operations (worktree creation, branch ops) |
| `spf13/cobra` | CLI command parsing |
| `creack/pty` | PTY for tmux attach |
| `stretchr/testify` | Test assertions |

## External Tool Requirements

- **tmux** — must be installed on the host. Claude Squad creates/manages tmux sessions.
- **gh** (GitHub CLI) — used for pushing branches and PR creation.
- The AI agent itself (e.g. `claude`, `codex`, `gemini`, `aider`, `opencode`, `pi`) must be installed separately.
- **pi** and **opencode** must be launched with `env -u CLAUDE_CODE_OAUTH_TOKEN -u GITHUB_TOKEN` — pi reads `GITHUB_TOKEN` from the environment and tries to use it as an Anthropic API key, causing "Personal Access Tokens are not supported" auth failures. Both tokens must be stripped.
- **jj** (Jujutsu) — required only when `vcs_type: "jj"` is configured. Tested against v0.39.0. Note: jj uses file-level repo locking — concurrent mutating commands (`jj describe`, `jj new`) from multiple agent workspaces can conflict; the `JJWorkspace` implementation must use retry-with-backoff on mutating calls.
  - `jj bookmark list <name>` exits 0 even when the bookmark does not exist (prints a Warning line instead). Use `bookmarkExists()` in `session/jj/util.go` which checks for `bookmarkName + ":"` while skipping Warning/Hint lines.
  - `jj split` is interactive — it opens an editor and hangs. Use `EDITOR=true jj split ...` to suppress the editor when calling from code or scripts.
  - jj resolves paths through symlinks; on macOS `os.MkdirTemp` returns `/var/...` but jj returns `/private/var/...`. Use `filepath.EvalSymlinks` when comparing paths in tests.
  - `jj edit <bookmark>` is a working-copy operation and must be run with `cmd.Dir` set to the repo root — passing `--repository` flag does not update the working copy.
  - After `jj edit`, `jj status` always shows "Working copy changes:" (normal behavior — the working copy IS the checked-out change). Do not use a dirty-state guard before `jj edit`; it will always fire and block checkout.
  - When snapshotting an agent workspace before checkout, use describe + move bookmark to `@` (do NOT call `jj new`). Calling `jj new` advances the agent one empty commit ahead, leaving the bookmark behind.
  - Do not prepend `BranchPrefix` (e.g. `username/`) to jj bookmark names and do not append hex timestamp suffixes to workspace directory names — both cause bookmark-not-found errors and confuse users. Use the sanitized session title directly.
  - `jj diff --ignore-working-copy` reads from the last snapshot, not the live filesystem. Always run `jj status` first (without `--ignore-working-copy`) to force a snapshot before diffing, or the diff panel will show stale data.
  - A workspace becomes stale whenever any other workspace in the same repo advances the op log. `--ignore-working-copy` only prevents the current workspace from snapshotting (and thereby staling others) — it does NOT prevent receiving staleness from others. Always `workspace update-stale` before WC-touching operations like `jj edit`.
  - `jj bookmark list <name>` exits 0 even when the bookmark does not exist, and prints "No such bookmark" in the error text (not a warning line). The targeted error check is `strings.Contains(err, "No such bookmark")` — do NOT use `strings.Contains(err, "Bookmark")` (too broad, hides real errors).
  - `sanitizeBookmarkName` must replace `/` with `-` before the regex pass. A slash produces workspace paths like `worktrees/feature/foo` where the intermediate `worktrees/feature/` directory is never created, causing `jj workspace add` to fail silently.
  - In `CommitChanges`, set the bookmark to `@` BEFORE calling `jj new`. If set after using `@-` and `jj new` fails, the bookmark is permanently orphaned — `IsDirty()` returns false on the new empty WC so a retry is a no-op.
  - `CleanupWorkspaces` (used by `cs reset`) must call `jj workspace forget` for each workspace directory before deleting it. Skipping this leaves stale workspace registrations in the op log that cause "working copy is stale" errors on every subsequent `jj log`/`jj status`.
  - `runJJCommandWithRetry` should be used for the `jj log -r @-` calls that capture `baseChangeID` in `setupNewWorkspace` and `setupFromExistingBookmark`. Using `runJJCommand` (no retry) on a read-only query is fine in low-contention cases but causes unnecessary workspace destruction on first lock contention.
  - `jj status` (without `--ignore-working-copy`) run with `cmd.Dir` in the main repo is the correct way to snapshot user file edits before syncing to the agent workspace. Do not use `--repository` for this — it may not snapshot the right workspace.

## Configuration

Config file: `~/.claude-squad/config.json`

```json
{
  "default_program": "claude",
  "profiles": [
    { "name": "claude", "program": "claude" },
    { "name": "codex",  "program": "codex" },
    { "name": "aider",  "program": "aider --model ollama_chat/gemma3:1b" }
  ]
}
```

Path can be found via `cs debug`.

## Build

```bash
go build -o cs .
```

## Test

```bash
go test ./...
```

> Python tests (if any introduced) may hang in sandbox environments — run locally.

## CI

GitHub Actions at `.github/workflows/build.yml`. Runs on push/PR to main.

## Versioning

Version is bumped via `bump-version.sh`. Current: **1.0.17**.

## Upstream

Upstream repo: `https://github.com/smtg-ai/claude-squad`

To pull upstream changes:
```bash
jj git fetch --remote upstream
# Then integrate selectively with jj rebase
```
