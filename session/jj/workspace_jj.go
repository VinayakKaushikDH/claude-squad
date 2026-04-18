package jj

import (
	"claude-squad/log"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// IsDirty checks if the workspace has uncommitted changes.
func (j *JJWorkspace) IsDirty() (bool, error) {
	// Use runJJCommandWithRetry so stale workspaces are auto-recovered before checking.
	output, err := runJJCommandWithRetry(j.workspacePath, "status")
	if err != nil {
		return false, fmt.Errorf("failed to check workspace status: %w", err)
	}
	// jj status shows "Working copy changes:" when there are modifications
	return strings.Contains(output, "Working copy changes:"), nil
}

// CommitChanges snapshots the current working copy and advances to a new empty change.
func (j *JJWorkspace) CommitChanges(msg string) error {
	dirty, err := j.IsDirty()
	if err != nil {
		return fmt.Errorf("failed to check for changes: %w", err)
	}
	if !dirty {
		return nil
	}

	// Describe the working copy change
	if _, err := runJJCommandWithRetry(j.workspacePath, "describe", "-m", msg); err != nil {
		return fmt.Errorf("failed to describe change: %w", err)
	}

	// Set bookmark on the described change BEFORE creating a new WC.
	// If we set it after jj new (using "@-") and jj new fails, the bookmark is
	// lost and cannot be recovered: IsDirty() returns false on the new empty WC
	// so a retry of CommitChanges() is a no-op. Setting it here (@) is equivalent
	// since the described commit is @ before jj new and @- after it.
	if err := j.ensureBookmark("@"); err != nil {
		return fmt.Errorf("failed to set bookmark: %w", err)
	}

	// Create a new empty working copy change
	if _, err := runJJCommandWithRetry(j.workspacePath, "new"); err != nil {
		return fmt.Errorf("failed to create new change: %w", err)
	}

	return nil
}

// PushChanges commits (if dirty) and pushes the bookmark to the remote.
func (j *JJWorkspace) PushChanges(commitMessage string, open bool) error {
	if err := checkGHCLI(); err != nil {
		return err
	}

	// Commit if there are uncommitted changes
	dirty, err := j.IsDirty()
	if err != nil {
		return fmt.Errorf("failed to check for changes: %w", err)
	}
	if dirty {
		if err := j.CommitChanges(commitMessage); err != nil {
			return fmt.Errorf("failed to commit changes: %w", err)
		}
	}

	// Push the bookmark to the remote
	if _, err := runJJCommandWithRetry(j.workspacePath, "git", "push", "--bookmark", j.bookmarkName); err != nil {
		return fmt.Errorf("failed to push bookmark: %w", err)
	}

	if open {
		if err := j.openBranchURL(); err != nil {
			log.ErrorLog.Printf("failed to open branch URL: %v", err)
		}
	}

	return nil
}

// CanResume always returns nil — jj workspaces are independent, no checkout conflicts.
func (j *JJWorkspace) CanResume() error {
	return nil
}

// CanRemove always returns nil — jj workspaces are independent, no checkout conflicts.
func (j *JJWorkspace) CanRemove() error {
	return nil
}

// CheckoutInMainRepo checks out the bookmark in the main repository via `jj edit`.
// Always describes the workspace WC (creating a named snapshot even if clean),
// sets the bookmark to that snapshot, then edits it in the main repo. Both
// workspaces end up on the same commit; their filesystem directories remain
// independent so the agent keeps working normally.
func (j *JJWorkspace) CheckoutInMainRepo() error {
	// Ensure the working copy is current before any snapshot operations.
	// jj workspace update-stale is a no-op if not stale.
	if _, err := runJJCommand(j.workspacePath, "workspace", "update-stale"); err != nil {
		log.ErrorLog.Printf("jj workspace update-stale (pre-checkout) failed for %s: %v", j.workspaceName, err)
	}

	// Always describe the current WC to create a named snapshot commit.
	msg := fmt.Sprintf("[claudesquad] snapshot for checkout on %s", time.Now().Format(time.RFC822))
	if _, err := runJJCommandWithRetry(j.workspacePath, "describe", "-m", msg); err != nil {
		return fmt.Errorf("failed to describe workspace changes: %w", err)
	}

	// Set bookmark to the snapshot (the current WC @).
	if err := j.ensureBookmark("@"); err != nil {
		return fmt.Errorf("failed to set bookmark: %w", err)
	}

	// Heal the main repo's workspace staleness before editing.
	// When the agent has been working (running jj describe, jj new, etc.) it
	// advances the op log, which leaves the main repo workspace stale. jj edit
	// is a WC-touching command and will fail with "working copy is stale" if we
	// don't heal first. Run with cmd.Dir (not --repository) because update-stale
	// is a workspace operation that must run from the workspace directory.
	healMain := exec.Command("jj", "workspace", "update-stale")
	healMain.Dir = j.repoPath
	if out, err := healMain.CombinedOutput(); err != nil {
		log.ErrorLog.Printf("jj workspace update-stale (main repo pre-checkout) failed: %s (%v)", out, err)
	}

	// Checkout: edit the snapshot change in the main repo.
	// Run directly with cmd.Dir rather than --repository, because jj edit is a
	// working-copy operation that needs to update files in the main repo directory.
	cmd := exec.Command("jj", "edit", j.bookmarkName)
	cmd.Dir = j.repoPath
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to checkout bookmark in main repo: %s (%w)", output, err)
	}

	// jj edit runs from the main repo and advances the op log without snapshotting
	// this workspace, leaving it stale. Heal it immediately so the agent's next
	// operation doesn't hit a stale error.
	if _, err := runJJCommand(j.workspacePath, "workspace", "update-stale"); err != nil {
		log.ErrorLog.Printf("jj workspace update-stale (post-checkout) failed for %s: %v", j.workspaceName, err)
	}

	return nil
}

// SyncFromMainRepo snapshots the main repo's current working copy into the
// current commit, then updates the agent workspace to reflect those changes.
//
// This is the reverse of CheckoutInMainRepo. Use it when the user has edited
// files in the main repo (amending the checked-out commit in place, not
// creating a new commit) and wants the agent to continue from that state.
//
// jj model: both workspaces are AT the same change (change_id B). When the
// main repo snapshots its WC, B's content is updated (new commit_id, same
// change_id). Running workspace update-stale in the agent workspace re-syncs
// its filesystem to B's updated content. No new commit is created; the graph
// stays linear; neither workspace is left stale.
func (j *JJWorkspace) SyncFromMainRepo() error {
	// Step 1: Snapshot the main repo's WC. The user may have edited files
	// without running any jj commands; jj status forces a snapshot that captures
	// those edits into the current WC commit and advances the op log.
	// Run with cmd.Dir so jj uses the default workspace (not --repository which
	// might resolve to a different workspace context).
	snapCmd := exec.Command("jj", "status")
	snapCmd.Dir = j.repoPath
	if out, err := snapCmd.CombinedOutput(); err != nil {
		// Non-fatal: if the snapshot fails (e.g. repo locked), still try to
		// update-stale in case a prior op already captured the changes.
		log.ErrorLog.Printf("jj status (main repo snapshot for sync) failed: %s (%v)", out, err)
	}

	// Step 2: Update the agent workspace. The snapshot above advanced the op
	// log, making the agent workspace stale. update-stale re-syncs the agent
	// workspace's filesystem to the current commit content — which now includes
	// the user's edits from step 1.
	if _, err := runJJCommandWithRetry(j.workspacePath, "workspace", "update-stale"); err != nil {
		return fmt.Errorf("failed to sync agent workspace from main repo: %w", err)
	}

	return nil
}

// ensureBookmark creates or moves the bookmark to the given revision.
// Uses "bookmark set" which is idempotent (creates if missing, moves if exists).
// --ignore-working-copy: bookmark set is pure metadata — no WC snapshot needed,
// and snapshotting would advance the op log unnecessarily, staling other workspaces.
func (j *JJWorkspace) ensureBookmark(revision string) error {
	_, err := runJJCommandWithRetry(j.workspacePath, "bookmark", "set", j.bookmarkName, "-r", revision, "-B", "--ignore-working-copy")
	return err
}

// openBranchURL opens the bookmark's branch in the default browser via gh CLI.
func (j *JJWorkspace) openBranchURL() error {
	if err := checkGHCLI(); err != nil {
		return err
	}
	cmd := exec.Command("gh", "browse", "--branch", j.bookmarkName)
	cmd.Dir = j.workspacePath
	return cmd.Run()
}
