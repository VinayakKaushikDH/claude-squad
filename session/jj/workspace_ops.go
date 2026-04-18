package jj

import (
	"fmt"
	"os"
	"strings"
)

// Setup creates a new jj workspace for the session.
func (j *JJWorkspace) Setup() error {
	// Ensure worktrees directory exists
	wsDir, err := getWorkspaceDirectory()
	if err != nil {
		return fmt.Errorf("failed to get workspace directory: %w", err)
	}
	if err := os.MkdirAll(wsDir, 0755); err != nil {
		return err
	}

	if j.isExistingBookmark {
		return j.setupFromExistingBookmark()
	}

	// Check if bookmark already exists (e.g. resuming a session)
	// jj bookmark list exits 0 even for non-existent bookmarks but prints a warning.
	// A real match has the bookmark name at the start of a line (not in a "Warning:" line).
	output, _ := runJJCommand(j.repoPath, "bookmark", "list", j.bookmarkName, "--ignore-working-copy")
	if bookmarkExists(output, j.bookmarkName) {
		return j.setupFromExistingBookmark()
	}

	return j.setupNewWorkspace()
}

// resolveBaseRevision returns the change ID to use as the base for a new
// workspace. It tries @ first (the current working-copy change), then falls
// back to trunk() and latest(all()) for repos where the default workspace has
// no working-copy commit (e.g. init'd with --no-working-copy, or "default"
// workspace was accidentally forgotten).
func (j *JJWorkspace) resolveBaseRevision() (string, error) {
	for _, rev := range []string{"@", "trunk()", "latest(all())"} {
		out, err := runJJCommand(j.repoPath, "log", "-r", rev, "--no-graph", "-T", "change_id", "--ignore-working-copy")
		if err == nil {
			if r := strings.TrimSpace(out); r != "" {
				return r, nil
			}
		}
	}
	return "", fmt.Errorf("failed to resolve base revision: no working-copy commit and no fallback found")
}

// setupNewWorkspace creates a workspace from the current change (@).
func (j *JJWorkspace) setupNewWorkspace() error {
	// Never forget the "default" workspace — it is the main repo's primary
	// workspace. Calling `jj workspace forget default` would destroy the
	// working-copy tracking for the entire repo, making @ unresolvable.
	if j.workspaceName != "default" {
		_, _ = runJJCommandWithRetry(j.repoPath, "workspace", "forget", j.workspaceName, "--ignore-working-copy")
	}
	// Remove the directory: jj workspace add fails if the path already exists.
	_ = os.RemoveAll(j.workspacePath)

	// Resolve the base revision without triggering a working-copy snapshot.
	rev, err := j.resolveBaseRevision()
	if err != nil {
		return err
	}

	// Create workspace from the resolved change ID
	if _, err := runJJCommandWithRetry(j.repoPath, "workspace", "add", "--revision", rev, j.workspacePath); err != nil {
		return fmt.Errorf("failed to create jj workspace: %w", err)
	}

	// Capture base change ID from the new workspace (parent of working copy)
	output, err := runJJCommand(j.workspacePath, "log", "-r", "@-", "--no-graph", "-T", "change_id")
	if err != nil {
		return fmt.Errorf("failed to get base change ID: %w", err)
	}
	j.baseChangeID = strings.TrimSpace(output)

	return nil
}

// setupFromExistingBookmark creates a workspace from an existing bookmark.
func (j *JJWorkspace) setupFromExistingBookmark() error {
	// Never forget the "default" workspace (see setupNewWorkspace).
	if j.workspaceName != "default" {
		_, _ = runJJCommand(j.repoPath, "workspace", "forget", j.workspaceName, "--ignore-working-copy")
	}
	// Remove the directory: jj workspace add fails if the path already exists.
	_ = os.RemoveAll(j.workspacePath)

	// Create workspace from the bookmark's revision
	if _, err := runJJCommandWithRetry(j.repoPath, "workspace", "add", "--revision", j.bookmarkName, j.workspacePath); err != nil {
		return fmt.Errorf("failed to create jj workspace from bookmark %s: %w", j.bookmarkName, err)
	}

	// Capture base change ID if not already set (e.g. resuming a paused session)
	if j.baseChangeID == "" {
		output, err := runJJCommand(j.workspacePath, "log", "-r", "@-", "--no-graph", "-T", "change_id")
		if err != nil {
			return fmt.Errorf("failed to get base change ID: %w", err)
		}
		j.baseChangeID = strings.TrimSpace(output)
	}

	return nil
}

// Cleanup removes the workspace, deletes the bookmark (if not pre-existing), and cleans up the directory.
func (j *JJWorkspace) Cleanup() error {
	var errs []error

	// Forget the workspace
	if _, err := runJJCommandWithRetry(j.repoPath, "workspace", "forget", j.workspaceName, "--ignore-working-copy"); err != nil {
		// Only error if workspace actually exists
		if !strings.Contains(err.Error(), "No such workspace") {
			errs = append(errs, fmt.Errorf("failed to forget workspace: %w", err))
		}
	}

	// Remove the workspace directory
	if _, err := os.Stat(j.workspacePath); err == nil {
		if err := os.RemoveAll(j.workspacePath); err != nil {
			errs = append(errs, fmt.Errorf("failed to remove workspace directory: %w", err))
		}
	}

	// Delete the bookmark if we created it
	if !j.isExistingBookmark {
		if _, err := runJJCommandWithRetry(j.repoPath, "bookmark", "delete", j.bookmarkName, "--ignore-working-copy"); err != nil {
			if !strings.Contains(err.Error(), "not found") && !strings.Contains(err.Error(), "Bookmark") {
				errs = append(errs, fmt.Errorf("failed to delete bookmark %s: %w", j.bookmarkName, err))
			}
		}
	}

	if len(errs) > 0 {
		return combineErrors(errs)
	}
	return nil
}

// Remove removes the workspace but keeps the bookmark (used for Pause).
func (j *JJWorkspace) Remove() error {
	var errs []error

	// Forget the workspace
	if _, err := runJJCommandWithRetry(j.repoPath, "workspace", "forget", j.workspaceName, "--ignore-working-copy"); err != nil {
		if !strings.Contains(err.Error(), "No such workspace") {
			errs = append(errs, fmt.Errorf("failed to forget workspace: %w", err))
		}
	}

	// Remove the workspace directory
	if _, err := os.Stat(j.workspacePath); err == nil {
		if err := os.RemoveAll(j.workspacePath); err != nil {
			errs = append(errs, fmt.Errorf("failed to remove workspace directory: %w", err))
		}
	}

	if len(errs) > 0 {
		return combineErrors(errs)
	}
	return nil
}

func combineErrors(errs []error) error {
	if len(errs) == 0 {
		return nil
	}
	if len(errs) == 1 {
		return errs[0]
	}
	errMsg := "multiple errors occurred:"
	for _, err := range errs {
		errMsg += "\n  - " + err.Error()
	}
	return fmt.Errorf("%s", errMsg)
}
