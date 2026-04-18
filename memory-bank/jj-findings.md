# jj Implementation Findings

> Written 2026-04-18 after a thorough analysis and bug-fix session on `session/jj/`.
> Purpose: give a future agent a complete, accurate picture of the jj implementation without re-doing the analysis.

---

## 1. jj Multi-Workspace Staleness Model

### What staleness is

jj tracks all state in an **operation log** (op log). Every mutating command (`jj describe`, `jj new`, `jj bookmark set`, `jj edit`, `jj workspace add/forget`) advances the op log. Each workspace records the op log position it last synced to. When any workspace advances the op log, **all other workspaces in the same repo** see a newer op log head than their recorded position — they become stale.

Staleness is about op log divergence, not working copy content. A workspace can be stale even if no files changed on disk.

### What `--ignore-working-copy` does (and doesn't do)

`--ignore-working-copy` tells jj: "do not snapshot my working copy before running this command."

- **Does**: prevents the command from advancing the op log by snapshotting the current WC. This means other workspaces are NOT staled by this operation.
- **Does not**: prevent the current workspace from being stale if another workspace already advanced the op log. Staleness is received from others; `--ignore-working-copy` only controls whether you emit it.

Consequence: using `--ignore-working-copy` on all polling commands (diff, status checks) is correct for avoiding cross-workspace interference, but does not protect against staleness that already arrived.

### How staleness is healed

`jj workspace update-stale` (run from inside the stale workspace, or with `cmd.Dir` set to the workspace path) re-syncs the workspace's op log pointer and re-materializes the filesystem to match the current commit. It is a no-op if the workspace is not stale.

### The staleness cycle in normal usage

```
Agent runs `jj describe` (advances op log)
  → main repo workspace becomes stale
  → user runs CheckoutInMainRepo
  → must heal main repo before `jj edit` (WC-touching command fails on stale workspace)
  → `jj edit` from main repo advances op log again
  → agent workspace becomes stale
  → heal agent workspace with `workspace update-stale`
```

Both heal steps must happen. The original code only healed the agent workspace post-checkout, not the main repo pre-checkout — this was the primary cause of reported stale-workspace errors.

---

## 2. Entry/Exit Point Design

### Exit point: Checkout key (`c`)

User presses `c` in the CS TUI. Calls `instance.CheckoutInMainRepo()` → `workspace.CheckoutInMainRepo()` in `session/jj/workspace_jj.go`.

**Sequence** (after the fix applied 2026-04-18):

1. `jj workspace update-stale` in agent workspace (`j.workspacePath`) — heal any staleness from prior agent operations.
2. `jj describe -m "<snapshot msg>"` in agent workspace — always snapshot the WC, even if clean. This creates a named commit the user can inspect.
3. `jj bookmark set <name> -r @ --ignore-working-copy` in agent workspace — point the bookmark at the snapshot commit.
4. `jj workspace update-stale` from `cmd.Dir = j.repoPath` — **heal the main repo** before the WC-touching `jj edit`. This was the missing step that caused "working copy is stale" errors when the agent had been running.
5. `jj edit <bookmark>` from `cmd.Dir = j.repoPath` — move the main repo's working copy to the snapshot. Both workspaces now point to the same commit.
6. `jj workspace update-stale` in agent workspace — `jj edit` advanced the op log; agent workspace is now stale. Heal immediately.

**Result**: both workspaces AT the same commit. Main repo filesystem reflects agent's work. Agent keeps running. No commit was created from the user's side.

**Critical**: steps 4 and 5 must use `cmd.Dir`, not `--repository`. `jj edit` is a working-copy operation — it changes which change the workspace's WC tracks. `--repository` only sets the repo path for metadata lookups; it does not update the WC.

### Entry point: Enter key

User presses Enter on an agent in the CS TUI. In `app/app.go`, before `tmuxSession.Attach()`, calls `selected.SyncFromMainRepo()` → `workspace.SyncFromMainRepo()` in `session/jj/workspace_jj.go`.

**Sequence**:

1. `jj status` run with `snapCmd.Dir = j.repoPath` — this forces jj to snapshot the main repo's WC, capturing any files the user edited after checkout. Advances the op log. Non-fatal if it fails (may already be snapshotted).
2. `jj workspace update-stale` in agent workspace — step 1 advanced the op log, making the agent workspace stale. `update-stale` re-materializes the agent workspace's filesystem to the commit's updated content (which now includes the user's edits).

**Result**: agent workspace filesystem has the user's amendments. No new commit was created. The jj change graph stays linear (same change_id, new content). Neither workspace is stale when the agent starts working.

**jj model explanation**: when both workspaces are AT the same change_id B, editing files in the main repo without running any jj commands does NOT advance the op log — the files change on disk, but B's snapshot is still the old content. Running `jj status` forces a snapshot: B's content is updated (new commit_id, same change_id). `workspace update-stale` in the agent workspace sees the op log advanced and re-syncs the filesystem to B's new content.

### When SyncFromMainRepo is a no-op

- Agent is not started (`i.started == false`)
- Workspace is not a `*jj.JJWorkspace` (git sessions return nil immediately)
- User did not edit any files since checkout (snapshot finds nothing new; `update-stale` finds workspace is not stale)

---

## 3. SyncFromMainRepo Mechanics

The key insight: **jj's change_id is stable across amendments**. When both workspaces are at change B:

```
@ default workspace = B  (filesystem: agent's original files)
@ agent workspace = B    (filesystem: same files, different directory)
```

User edits `agent.txt` in the main repo. No jj command has run, so the op log has not advanced. The agent workspace is not stale. It just has the old filesystem content.

Running `jj status` from the main repo:
- jj detects that the filesystem has changed vs the last snapshot
- jj records new file hashes — B now has new content (new commit_id, same change_id)
- op log advances by one entry

Running `workspace update-stale` in the agent workspace:
- agent workspace's op log pointer is behind the new head
- jj re-materializes the agent workspace filesystem to B's updated content
- agent workspace now has the user's edits

No new node in the commit graph. No new change_id. The graph:

```
Before:   ◆ B  ←  initial snapshot (old content)
After:    ◆ B  ←  same change_id, updated content (new commit_id)
```

This is why the user's instruction "I will never add a new commit, only amend" works perfectly with this model: `SyncFromMainRepo` is designed for exactly that case.

---

## 4. All Bugs Found and Fixed (2026-04-18)

### Bug 1: Stale diff display
**File**: `session/jj/diff.go`

**Problem**: `Diff()` used `--ignore-working-copy` on `jj diff`, reading from the last snapshot rather than the live filesystem. The metadata polling loop called `Diff()` without any prior snapshot step. Result: the diff panel could show indefinitely stale data whenever the agent wrote files without running jj commands.

**Fix**: Added a `jj status` call (without `--ignore-working-copy`) inside `Diff()` before the `jj diff` call, using the same 5-second timeout context. Non-fatal: if the snapshot fails, falls through and diffs against whatever snapshot currently exists.

**Location after fix**: `diff.go:37-38` — `snapCmd` runs `jj --repository j.workspacePath status`.

---

### Bug 2: Slash in bookmark names creates unresolvable workspace paths
**File**: `session/jj/util.go`

**Problem**: `sanitizeBookmarkName` allowed `/` through (regex `[^a-z0-9\-_/.]+` only removed characters outside that set). A session named `"feature/my-branch"` produced `workspacePath = ".../worktrees/feature/my-branch"`. `jj workspace add` fails because the intermediate directory `worktrees/feature/` does not exist and `os.MkdirAll` only creates the `worktrees/` parent.

**Fix**: Added `s = strings.ReplaceAll(s, "/", "-")` before the regex pass in `sanitizeBookmarkName`.

**Location after fix**: `util.go:22`.

---

### Bug 3: `runJJCommand` (no retry) for base change ID capture
**File**: `session/jj/workspace_ops.go`

**Problem**: Both `setupNewWorkspace` and `setupFromExistingBookmark` used `runJJCommand` (no retry/recovery) when capturing `baseChangeID` via `jj log -r @-`. A single lock contention event at this step returned an error that propagated up as workspace setup failure, unnecessarily destroying the workspace.

**Fix**: Changed both calls to `runJJCommandWithRetry` at the `jj log -r @-` capture lines.

**Location after fix**: `workspace_ops.go:77` and `workspace_ops.go:104`.

---

### Bug 4: Overly broad bookmark error suppression
**File**: `session/jj/workspace_ops.go`

**Problem**: In `Cleanup()`, the bookmark deletion error check was `!strings.Contains(err.Error(), "Bookmark")`. This suppressed any error containing the word "Bookmark" — including legitimate errors like permission failures or unexpected jj error messages that happened to mention "Bookmark".

**Fix**: Changed to `!strings.Contains(errMsg, "No such bookmark") && !strings.Contains(errMsg, "not found")` — matches only the specific expected non-error cases.

**Location after fix**: `workspace_ops.go:144-145`.

---

### Bug 5: CommitChanges bookmark orphan
**File**: `session/jj/workspace_jj.go`

**Problem**: `CommitChanges` called `jj describe`, then `jj new`, then `ensureBookmark("@-")`. If `jj new` succeeded but `ensureBookmark("@-")` failed (e.g., lock contention), the workspace was left with a described commit, a new empty WC, but no bookmark. A retry of `CommitChanges` would not fix it: `IsDirty()` returns false on the new empty WC, so `CommitChanges` is a no-op and the bookmark is permanently lost.

**Fix**: Set the bookmark to `@` BEFORE calling `jj new`. The described commit is `@` before `jj new` and `@-` after it — so the end state is identical, but the ordering makes it recoverable if `jj new` fails (bookmark is set before any risk).

**Location after fix**: `workspace_jj.go:42-43` — `ensureBookmark("@")` called between `describe` and `new`.

---

### Bug 6: CleanupWorkspaces missing `jj workspace forget`
**File**: `session/jj/refs.go`

**Problem**: `CleanupWorkspaces()` (called by `cs reset`) deleted workspace directories with `os.RemoveAll` but never called `jj workspace forget`. jj retained stale workspace entries in the repo's op log. Every subsequent `jj log` or `jj status` emitted "working copy is stale" warnings for each phantom workspace. These accumulated across resets.

**Fix**: Before deleting workspace directories, walk the config dir for `.jj` subdirectories, resolve the repo root via `findJJRepoRoot`, and call `jj workspace forget <workspaceName>` for each.

**Location after fix**: `refs.go:74-84` — `filepath.Walk` pass before the `os.ReadDir` deletion loop.

---

### Bug 7: CheckoutInMainRepo missing main repo heal (primary stale-error source)
**File**: `session/jj/workspace_jj.go`

**Problem**: `CheckoutInMainRepo` healed the agent workspace (pre-checkout `update-stale`) and healed it again post-checkout, but never healed the **main repo** before `jj edit`. When the agent had been running (running `jj describe`, `jj new`, etc.), the main repo's workspace became stale. `jj edit` is a WC-touching command and fails with "working copy is stale" on a stale workspace. The error was returned to the user as a checkout failure. This was the primary root cause of the reported stale-workspace errors.

**Fix**: Added a `healMain` step that runs `jj workspace update-stale` from `cmd.Dir = j.repoPath` between the bookmark-set and the `jj edit` call.

**Location after fix**: `workspace_jj.go:124-128`.

---

### Bug 8 (pre-existing, logged): Swallowed `update-stale` errors
**File**: `session/jj/workspace_jj.go`

**Problem**: Both `update-stale` calls in `CheckoutInMainRepo` originally used `_, _ = runJJCommand(...)` — errors silently discarded.

**Fix**: Changed to log failures via `log.ErrorLog.Printf` so they appear in the CS log file. The retry loop in the caller still compensates, but failures are now observable.

**Location after fix**: `workspace_jj.go:103-104` and `workspace_jj.go:142-144`.

---

## 5. Open Issues / Not Yet Fixed

### Session name collision detection (HIGH)
**File**: `session/jj/workspace_ops.go:57-61`

If two sessions produce the same sanitized bookmark name (e.g. "My Feature" and "my-feature" both become `"my-feature"`), the second session's `setupNewWorkspace` calls `workspace forget` and `os.RemoveAll` on the first session's directory while the agent is actively running in it. No collision detection exists.

**Mitigation needed**: check if `j.workspaceName` is already registered in `jj workspace list` before forgetting it, or append a short hash suffix when a collision is detected.

---

### Workspace directory not verified on restore (MEDIUM)
**File**: `session/instance.go:219`

`FromInstanceData()` → `Start(false)` → `tmuxSession.Restore()` does not check whether the workspace directory exists on disk. If the directory was manually deleted or there was a disk issue, all subsequent metadata operations (diff, status) fail silently — errors logged but no user-visible state change.

**Fix needed**: after constructing the workspace, `os.Stat(workspace.GetWorktreePath())` and mark the instance as Paused if missing.

---

### Fragile stale error detection (LOW)
**File**: `session/jj/util.go:104-108`

`isStaleError()` matches on literal substrings `"working copy is stale"` and `"workspace update-stale"`. jj error messages have changed across versions. If jj changes the wording, stale recovery silently breaks — the retry loop treats it as a permanent failure.

**Fix needed**: broaden the match or test against exit codes.

---

### Race: metadata loop + Pause (LOW)
**File**: `app/app.go:1327-1335` + `session/instance.go:535`

Metadata loop goroutines can still be running `Diff()` on a workspace while `Pause()` is calling `workspace forget` on the main thread. `Diff()` fails; error gets swallowed by the polling loop. Not data loss, but error log accumulation.

---

## 6. jj Assumptions That Were Questioned

### "jj workspace add creates a git worktree in colocated mode"
**FALSE.** `jj workspace add` creates a directory with only `.jj/` — no `.git` file. git worktree list is unaffected. `git worktree list/prune` is irrelevant for jj sessions.

### "IsDirty() always runs before Diff() in production"
**FALSE.** The test for `Diff()` had a comment claiming this, but the production metadata polling loop calls `Diff()` directly without a prior `IsDirty()`. The fix is inside `Diff()` itself (snapshot step), not a calling-convention requirement.

### "After jj edit, the workspace shows as dirty (Working copy changes:)"
**TRUE, and expected.** `jj edit` moves the WC to the checked-out commit, so the WC IS the commit. `jj status` always shows "Working copy changes:" after `jj edit`. Do not use a dirty guard before `jj edit` — it will always fire and block checkout.

### "jj bookmark list exits non-zero when a bookmark doesn't exist"
**FALSE.** `jj bookmark list <name>` exits 0 even for non-existent bookmarks, printing a Warning line instead. The `bookmarkExists()` helper in `util.go` is required to distinguish a real match from a warning.

### "update-stale in the agent workspace is sufficient before jj edit in the main repo"
**FALSE.** The main repo's workspace can be independently stale from operations the agent ran. Both the agent workspace and the main repo workspace must be healed — in that order — before `jj edit`.

### "Diff polling with --ignore-working-copy doesn't stale other workspaces"
**TRUE** — this is the correct design. Using `--ignore-working-copy` on the snapshot-then-diff sequence inside `Diff()` would defeat the purpose (re-staling others on every poll tick). The fix is: snapshot once without `--ignore-working-copy`, then diff with `--ignore-working-copy`. The snapshot step is intentional; the `--ignore-working-copy` on diff reads from that fresh snapshot.

### "Bookmark set with --ignore-working-copy is safe"
**TRUE.** Bookmark set is pure metadata — it writes a pointer to a change_id. No filesystem changes, no WC snapshot needed. Using `--ignore-working-copy` on `ensureBookmark` is correct and avoids unnecessarily staling other workspaces.

---

## 7. Key File References

| File | Role |
|------|------|
| `session/jj/diff.go` | `Diff()` — snapshot then diff |
| `session/jj/util.go` | `sanitizeBookmarkName`, `runJJCommand`, `runJJCommandWithRetry`, `isStaleError`, `isLockError`, `bookmarkExists` |
| `session/jj/workspace_ops.go` | `Setup`, `setupNewWorkspace`, `setupFromExistingBookmark`, `Cleanup`, `Remove` |
| `session/jj/workspace_jj.go` | `IsDirty`, `CommitChanges`, `PushChanges`, `CheckoutInMainRepo`, `SyncFromMainRepo`, `ensureBookmark` |
| `session/jj/refs.go` | `CleanupWorkspaces`, `FetchBookmarks`, `SearchBookmarks` |
| `session/jj/workspace_test.go` | 13 integration tests using real jj repos — reference pattern for any new jj behavior tests |
| `session/instance.go:534` | `Instance.SyncFromMainRepo()` — type-asserts to `*jj.JJWorkspace`, returns nil for git |
| `app/app.go:1126,1146` | Two attach call sites where `SyncFromMainRepo()` is called before `tmuxSession.Attach()` |
