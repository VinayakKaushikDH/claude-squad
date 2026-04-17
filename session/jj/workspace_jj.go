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
	output, err := runJJCommand(j.workspacePath, "status")
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

	// Create a new empty working copy change
	if _, err := runJJCommandWithRetry(j.workspacePath, "new"); err != nil {
		return fmt.Errorf("failed to create new change: %w", err)
	}

	// Set bookmark on the described change (@- is the parent of the new empty WC)
	if err := j.ensureBookmark("@-"); err != nil {
		return fmt.Errorf("failed to set bookmark: %w", err)
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
// If the workspace has uncommitted changes, they are committed first so the
// bookmark is advanced and the user sees all changes when they checkout.
func (j *JJWorkspace) CheckoutInMainRepo() error {
	// Snapshot the workspace's working copy: describe it and move the bookmark
	// to the current change (@) without creating a new child change. This keeps
	// both the agent and the user on the exact same jj change.
	if dirty, err := j.IsDirty(); err != nil {
		log.ErrorLog.Printf("failed to check workspace dirty state: %v", err)
	} else if dirty {
		msg := fmt.Sprintf("[claudesquad] snapshot for checkout on %s", time.Now().Format(time.RFC822))
		if _, err := runJJCommandWithRetry(j.workspacePath, "describe", "-m", msg); err != nil {
			return fmt.Errorf("failed to describe workspace changes: %w", err)
		}
	}

	// Ensure bookmark points to the current working copy change (@), creating
	// it if this is the first snapshot.
	if err := j.ensureBookmark("@"); err != nil {
		return fmt.Errorf("failed to set bookmark: %w", err)
	}

	// Checkout: edit the same change the agent is on.
	// Run directly with cmd.Dir rather than --repository, because jj edit is a
	// working-copy operation that needs to update files in the main repo directory.
	cmd := exec.Command("jj", "edit", j.bookmarkName)
	cmd.Dir = j.repoPath
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to checkout bookmark in main repo: %s (%w)", output, err)
	}
	return nil
}

// ensureBookmark creates or moves the bookmark to the given revision.
// Uses "bookmark set" which is idempotent (creates if missing, moves if exists).
func (j *JJWorkspace) ensureBookmark(revision string) error {
	_, err := runJJCommandWithRetry(j.workspacePath, "bookmark", "set", j.bookmarkName, "-r", revision, "-B")
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
