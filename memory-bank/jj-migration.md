# jj Migration Design Doc

> Status: Discovery complete. Implementation not started.
> Goal: Replace the `session/git/` layer with jujutsu (`jj`) equivalents.

---

## Why jj?

The key UX win: **you can inspect or check out an agent's changes from your main workspace without the agent needing to pause or check out of its worktree.**

In git today, `IsBranchCheckedOut` (`worktree_git.go:161`) blocks the `Resume()` flow if the user has checked out the agent's branch in their main workspace. With jj, each session lives in its own **workspace** — independent working copies sharing one repo. You can `jj diff -r 'workspace:agent@'` or `jj log` from the main workspace to see exactly what the agent has done, with zero conflict.

---

## Complete Git Operations Inventory

All git calls are in `session/git/` (6 files). Here is every call, its file/line, and the jj equivalent.

### Repo Detection

| What | File | Git command | jj equivalent |
|------|------|-------------|---------------|
| Check if in a repo | `util.go:52`, `main.go:46` | `git -C <path> rev-parse --show-toplevel` | `jj --repository <path> root` |
| Find repo root | `util.go:57` | `git -C <path> rev-parse --show-toplevel` | `jj --repository <path> root` |

### Workspace / Worktree Lifecycle

| What | File | Git command | jj equivalent |
|------|------|-------------|---------------|
| Create new workspace (new branch) | `worktree_ops.go:88` | `git worktree add -b <branch> <path> <commit>` | `jj workspace add --revision @ <path>` |
| Create workspace from existing bookmark | `worktree_ops.go:54,61` | `git worktree add [-b] <path> [origin/]<branch>` | `jj workspace add --revision <bookmark> <path>` |
| Remove workspace | `worktree_ops.go:139` | `git worktree remove -f <path>` | `jj workspace forget <name>` + `rm -rf <path>` |
| Prune dead workspace refs | `worktree_ops.go:147` | `git worktree prune` | `jj workspace forget` (for stale names) |
| List all workspaces | `worktree_ops.go:167` | `git worktree list --porcelain` | `jj workspace list` |
| Clean up all worktrees (reset cmd) | `worktree_ops.go:154` | `git worktree list` + `git worktree prune` | `jj workspace list` + `jj workspace forget` for each |

### Bookmark (Branch) Operations

| What | File | Git command | jj equivalent |
|------|------|-------------|---------------|
| Check if local branch exists | `worktree_ops.go:31,46` | `git show-ref --verify refs/heads/<name>` | `jj bookmark list <name>` (exit 0 if exists) |
| Check if remote branch exists | `worktree_ops.go:49` | `git show-ref --verify refs/remotes/origin/<name>` | `jj bookmark list --all <name>` |
| Delete branch | `worktree_ops.go:74,116` | `git branch -D <name>` | `jj bookmark delete <name>` |
| Is branch checked out in main | `worktree_git.go:161` | `git branch --show-current` | **Not needed** — jj workspaces are isolated |
| List all branches (picker) | `worktree_git.go:22` | `git branch -a --sort=-committerdate` | `jj bookmark list` (no remote/local split needed) |
| Fetch + prune remote branches | `worktree_git.go:14` | `git fetch --prune` | `jj git fetch` |

### Commit & Push

| What | File | Git command | jj equivalent |
|------|------|-------------|---------------|
| Get HEAD commit hash | `worktree_ops.go:76` | `git rev-parse HEAD` | `jj log -r @ --no-graph -T 'commit_id'` |
| Stage all files | `worktree_git.go:82,136` | `git add .` | Not needed — jj tracks all files automatically |
| Stage untracked (intent-to-add) | `diff.go:28` | `git add -N .` | Not needed — jj diffs include untracked files |
| Commit locally | `worktree_git.go:88,142` | `git commit -m <msg> --no-verify` | `jj describe -m <msg>` then `jj new` |
| Push branch to remote | `worktree_git.go:99` | `git push -u origin <branch>` | `jj git push --bookmark <name>` |
| Sync via gh CLI | `worktree_git.go:95,108` | `gh repo sync` | `jj git push` (gh not needed for push) |

### Status & Diff

| What | File | Git command | jj equivalent |
|------|------|-------------|---------------|
| Check if dirty | `worktree_git.go:152` | `git status --porcelain` | `jj status` (empty output = clean) |
| Full diff vs base | `diff.go:35` | `git --no-pager diff <baseCommitSHA>` | `jj diff --from <baseChangeID>` |

---

## Struct / Data Model Changes

### `GitWorktree` → `JJWorkspace`

```go
// Current
type GitWorktree struct {
    repoPath         string
    worktreePath     string
    sessionName      string
    branchName       string
    baseCommitSHA    string  // git commit hash
    isExistingBranch bool
}

// Proposed
type JJWorkspace struct {
    repoPath          string
    workspacePath     string
    sessionName       string
    workspaceName     string  // jj workspace name (used in `jj workspace add`)
    bookmarkName      string  // jj bookmark (maps to git branch on push)
    baseChangeID      string  // jj change ID at workspace creation time
    isExistingBookmark bool
}
```

`GitWorktreeData` in `session/storage.go` needs a corresponding rename + migration path for existing stored sessions.

---

## Key Behavior Changes

### `IsBranchCheckedOut` → eliminated

**Current**: `Resume()` in `instance.go:481` checks whether the branch is checked out in the main workspace and blocks resumption if so. This forces the user to switch branches before they can resume a paused session.

**With jj**: This check disappears entirely. Each jj workspace is independent — the user can have the same bookmark "visible" from the main workspace and the agent workspace simultaneously without conflict. Resume becomes unconditional.

### `Pause()` with jj

**Current**: Commits dirty changes locally → removes git worktree (keeps branch).

**With jj**:
1. `jj describe -m <pause-msg>` to snapshot the working copy change.
2. `jj new` to leave the described change as a clean commit.
3. `jj workspace forget <name>` + `rm -rf <path>` to remove the workspace.
4. Bookmark remains pointing at the last committed change.

### `Diff()` — simpler with jj

**Current**: Requires `git add -N .` to include untracked files, then `git diff <baseCommitSHA>`.

**With jj**: Just `jj diff --from <baseChangeID>`. Untracked files are automatically included. No staging step.

### Bonus: Inspect agent work from main workspace

From the main workspace (not the agent's workspace), you can:
```bash
jj log -r 'all()'        # see all changes including agent workspace
jj diff -r agent-workspace@   # diff agent's working copy vs its parent
jj diff --from <baseChangeID> --to agent-workspace@  # all accumulated changes
```
This is the key UX win — **zero checkout conflicts**.

---

## File-Level Change Map

| File | Change needed |
|------|---------------|
| `session/git/worktree.go` | Replace struct + constructors with jj equivalents |
| `session/git/worktree_ops.go` | Replace all `git worktree` calls with `jj workspace` |
| `session/git/worktree_git.go` | Replace commit/push/fetch/status/branch calls |
| `session/git/diff.go` | Replace `git diff` with `jj diff` |
| `session/git/util.go` | Replace `git rev-parse` with `jj root`; `sanitizeBranchName` becomes `sanitizeBookmarkName` |
| `session/instance.go` | Remove `IsBranchCheckedOut` check in `Resume()`; update type refs |
| `session/storage.go` | Rename `GitWorktreeData` → `JJWorkspaceData`; add migration for stored sessions |
| `main.go` | Replace `IsGitRepo` with `IsJJRepo` (or keep git check + add jj check) |
| `app/app.go` | Update `FetchBranches`, `SearchBranches`, `PushChanges` call sites |

---

## Open Questions — Resolved

### Q1: Does `jj workspace add` in colocated mode create a git worktree?

**Answer: No.** Tested empirically with jj 0.39.0.

```
$ jj workspace add ../jj-test-workspace
$ ls ../jj-test-workspace/
.jj/            ← only a .jj dir, no .git file

$ git worktree list
/private/tmp/jj-test d96b14c [main]   ← unaffected, only main workspace
```

jj workspaces are a jj-only concept. They don't map to git worktrees in colocated mode. The workspace dir materializes the working copy files + `.jj/` but no `.git`.

**Implication**: The `~/.claude-squad/worktrees/` directory still works as the location to put workspace dirs. tmux sessions work fine (they just need a directory path). The agent running in the workspace sees real files. But `git worktree list/prune` is irrelevant for jj sessions — use `jj workspace list/forget` instead.

Resume via `jj workspace add --revision <bookmark> <path>` confirmed working: files are materialized at the bookmark's commit state.

### Q2: Existing session migration

**Not needed.** Existing git-based sessions continue to work unchanged. New sessions use whichever VCS is configured. The session data stores which VCS type was used at creation time.

### Q3: Config toggle — git vs jj

Add `VCSType` to `config.Config` (default `"git"`, option `"jj"`). At startup, validate that the configured VCS is present (`jj root` check for jj mode, `git rev-parse` for git mode). Both implementations coexist. Users opt-in to jj via config.

```json
{ "vcs_type": "jj" }
```

### Q4: Branch prefix with bookmarks

`config.BranchPrefix` applies identically to bookmark names. The default `username/` prefix works the same way.

### Q5: GitHub PR flow

`jj git push --bookmark <name>` replaces `git push -u origin <name>`. The `gh` CLI PR creation flow is unchanged — it works against the git remote regardless of whether jj or git managed the push.

The `gh repo sync` fallback in `PushChanges` is not needed with jj — `jj git push` handles everything.

### Q6: Workspace naming

jj derives the workspace name from the last path component of the path passed to `jj workspace add`. We use the same sanitized-session-name path convention as git worktrees, so workspace name = sanitized session name. No separate tracking needed.

---

## Architecture Decision: Interface + Two Implementations

> Refined after Opus architect review (2026-04-17).

### Core interface: `session/workspace.go`

Define the `Workspace` interface in `session/workspace.go` — close to its sole consumer (`Instance`), no new package needed.

```go
// session/workspace.go
type Workspace interface {
    // Lifecycle
    Setup() error
    Cleanup() error     // full teardown: remove workspace + delete branch/bookmark
    Remove() error      // remove workspace, keep branch/bookmark

    // State
    IsDirty() (bool, error)
    Diff() *DiffStats

    // Mutations
    CommitChanges(msg string) error
    PushChanges(msg string, open bool) error

    // Guards — express intent, not mechanism
    CanResume() error   // git: checks branch not checked out; jj: returns nil
    CanRemove() error   // git: checks branch not checked out; jj: returns nil

    // Identity
    GetWorktreePath() string
    GetBranchName() string
    GetRepoPath() string
    GetRepoName() string
    GetBaseCommitSHA() string
    IsExistingBranch() bool
}
```

Key changes from initial design:
- **`Prune()` removed from interface** — it's a git-specific detail; absorbed into `Cleanup()`/`Remove()` internally.
- **`IsBranchCheckedOut()` → `CanResume()`/`CanRemove()`** — expresses intent rather than mechanism. Git impl checks branch checkout status; jj impl returns nil. Avoids Liskov violation (no-op returning `false, nil`).
- **Interface lives in `session/`** not `session/vcs/` — avoids a new package, stays close to `Instance`.

### Fix concrete type leak in app.go

**Problem**: `app.go:682` and `app.go:722` call `instance.GetGitWorktree()` to reach the concrete `*git.GitWorktree` for `IsBranchCheckedOut()` and `PushChanges()`. This leaks the git type into the app layer.

**Fix**: Add methods to `Instance` itself:
- `instance.CanKill() error` — delegates to `workspace.CanRemove()`
- `instance.PushChanges(msg string, open bool) error` — delegates to `workspace.PushChanges()`
- Delete `GetGitWorktree()` entirely.

### DiffStats location

`DiffStats` moves from `session/git/diff.go` to `session/workspace_types.go` — both implementations produce it, so it belongs with the interface contract.

### Storage: tagged union with json.RawMessage

```go
type InstanceData struct {
    // ... existing fields ...
    VCSType   string            `json:"vcs_type"`    // "git" or "jj"
    Worktree  json.RawMessage   `json:"worktree"`    // defer deserialization
    DiffStats DiffStatsData     `json:"diff_stats"`
}
```

`FromInstanceData` dispatches on `VCSType` to unmarshal into `GitWorktreeData` or `JJWorkspaceData` and construct the right workspace. Missing `vcs_type` defaults to `"git"` for backward compat — no migration needed.

### Package-level functions

These operate on the repo, not on a workspace instance. Live as free functions in `session/` (or a thin `session/vcs/` if preferred):

| Current (git pkg) | Replacement |
|---|---|
| `git.IsGitRepo(path)` | `vcs.IsRepo(path)` — auto-detect: `.jj/` first, then `.git/` |
| `git.CleanupWorktrees()` | `vcs.CleanupWorkspaces(vcsType)` |
| `git.FetchBranches(path)` | `vcs.FetchRefs(path, vcsType)` |
| `git.SearchBranches(path, filter)` | `vcs.SearchRefs(path, filter, vcsType)` |

`DetectVCSType(path) string` — new helper: checks for `.jj/` first (preferred), then `.git/`. Config `vcs_type` overrides auto-detection.

### jj-specific concerns

**Lock contention**: jj uses file locking on the repo. Multiple agent workspaces running `jj new` / `jj describe` concurrently may hit lock conflicts (git worktrees each have independent indexes, so git doesn't have this problem). The jj implementation should wrap mutating commands with retry-with-backoff.

**Bookmark sanitization**: jj bookmarks have different naming rules than git branches (e.g., cannot contain `..`). The jj implementation needs its own `sanitizeBookmarkName` function alongside the existing `sanitizeBranchName`.

**Colocated drift**: If someone runs raw `git` commands behind jj's back, jj detects the drift on next operation and attempts reconciliation. The jj implementation should catch and surface errors mentioning "concurrent operation" or "conflicting changes" clearly.

### jj-specific command reference (confirmed working)

```bash
# Dirty check — parse for "Working copy changes:"
jj status

# Commit
jj describe -m "<msg>"
jj new

# Diff against base change ID
jj diff --from <baseChangeID> --to @   # working copy (may be dirty)
jj diff --from <baseChangeID> --to @-  # last committed change

# Workspace lifecycle
jj workspace add --revision <bookmark> <path>   # create (or resume from bookmark)
jj workspace forget <name>                       # remove (bookmark preserved)
jj workspace list                                # enumerate

# Bookmark ops
jj bookmark create <name> --revision @-
jj bookmark list [--all]
jj bookmark delete <name>
jj git fetch
jj git push --bookmark <name>
```

---

## Implementation Order

### Phase 1: Refactor — abstract the git layer (no jj code yet)

1. Move `DiffStats` from `session/git/diff.go` to `session/workspace_types.go`.
2. Define `Workspace` interface in `session/workspace.go`.
3. Add `CanResume() error` and `CanRemove() error` to `GitWorktree` (wrapping `IsBranchCheckedOut`). Absorb `Prune()` into `Cleanup()`/`Remove()`.
4. Verify `GitWorktree` satisfies the `Workspace` interface.
5. Change `instance.go`: `gitWorktree *git.GitWorktree` → `workspace Workspace`.
6. Add `Instance.CanKill()` and `Instance.PushChanges()` methods. Delete `GetGitWorktree()`.
7. Update `app.go:682` (kill) and `app.go:722` (push) to use the new `Instance` methods.
8. Add `VCSType` to `config.Config` (default `"git"`) and `InstanceData`.
9. Update `FromInstanceData` to use `json.RawMessage` + dispatch on `vcs_type`.
10. Tests pass — no behavior change, pure refactor.

### Phase 2: Add jj implementation

11. Implement `session/jj/JJWorkspace` with all `Workspace` interface methods.
12. Add `sanitizeBookmarkName` for jj-specific bookmark naming rules.
13. Add retry-with-backoff wrapper for jj mutating commands (lock contention).
14. Add `DetectVCSType(path)`, `vcs.IsRepo(path)`.
15. Add `vcs.CleanupWorkspaces(vcsType)`, `vcs.FetchRefs(path, vcsType)`, `vcs.SearchRefs(path, filter, vcsType)`.
16. Wire jj workspace creation into `Instance.Start()` when `vcs_type == "jj"`.
17. Update `main.go` startup guard + reset command.
18. Update `app.go` `FetchBranches`/`SearchBranches` → `FetchRefs`/`SearchRefs`.
19. Write jj-specific tests.

Phase 1 is safe to ship independently — it's a pure refactor that sets up the interface without changing any behavior.

