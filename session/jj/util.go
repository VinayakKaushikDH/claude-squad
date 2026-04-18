package jj

import (
	"fmt"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

const maxRetries = 3

// sanitizeBookmarkName transforms an arbitrary string into a jj bookmark-friendly name.
// jj bookmarks cannot contain "..", must not start/end with ".", and follow similar
// rules to git branch names with the additional ".." restriction.
func sanitizeBookmarkName(s string) string {
	s = strings.ToLower(s)
	s = strings.ReplaceAll(s, " ", "-")
	// Replace slashes before the regex pass. A slash in a bookmark name produces a
	// workspace path like worktrees/feature/foo where the intermediate directory
	// worktrees/feature/ is never created, causing `jj workspace add` to fail.
	s = strings.ReplaceAll(s, "/", "-")

	// Remove characters not in our safe subset: letters, digits, dash, underscore, dot
	re := regexp.MustCompile(`[^a-z0-9\-_.]+`)
	s = re.ReplaceAllString(s, "")

	// Replace ".." sequences (forbidden in jj bookmarks)
	s = strings.ReplaceAll(s, "..", ".")

	// Collapse multiple dashes
	reDash := regexp.MustCompile(`-+`)
	s = reDash.ReplaceAllString(s, "-")

	// Trim leading/trailing dashes, slashes, and dots
	s = strings.Trim(s, "-/.")

	return s
}

// IsJJRepo checks if the given path is within a jj repository.
func IsJJRepo(path string) bool {
	cmd := exec.Command("jj", "--repository", path, "root", "--ignore-working-copy")
	return cmd.Run() == nil
}

// findJJRepoRoot returns the root path of the jj repository containing path.
func findJJRepoRoot(path string) (string, error) {
	cmd := exec.Command("jj", "--repository", path, "root", "--ignore-working-copy")
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("failed to find jj repository root from path: %s", path)
	}
	return strings.TrimSpace(string(out)), nil
}

// runJJCommand executes a jj command in the given path and returns combined output.
func runJJCommand(path string, args ...string) (string, error) {
	baseArgs := []string{"--repository", path}
	cmd := exec.Command("jj", append(baseArgs, args...)...)

	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("jj command failed: %s (%w)", output, err)
	}

	return string(output), nil
}

// runJJCommandWithRetry executes a jj command with retry-and-backoff for lock contention
// and automatic recovery for stale working copies.
// Use this for mutating commands (describe, new, bookmark set, workspace forget).
func runJJCommandWithRetry(path string, args ...string) (string, error) {
	var lastErr error
	for attempt := 0; attempt < maxRetries; attempt++ {
		output, err := runJJCommand(path, args...)
		if err == nil {
			return output, nil
		}
		if isStaleError(err) {
			// Heal the stale working copy, then retry immediately.
			if _, fixErr := runJJCommand(path, "workspace", "update-stale"); fixErr != nil {
				return "", fmt.Errorf("failed to update stale workspace: %w", fixErr)
			}
			lastErr = err
			continue
		}
		if !isLockError(err) {
			return "", err
		}
		lastErr = err
		time.Sleep(retryBackoff(attempt))
	}
	return "", fmt.Errorf("jj command failed after %d retries: %w", maxRetries, lastErr)
}

func isLockError(err error) bool {
	msg := err.Error()
	return strings.Contains(msg, "FileLock") ||
		strings.Contains(msg, "concurrent operation") ||
		strings.Contains(msg, "repo is locked")
}

func isStaleError(err error) bool {
	msg := err.Error()
	return strings.Contains(msg, "working copy is stale") ||
		strings.Contains(msg, "workspace update-stale")
}

func retryBackoff(attempt int) time.Duration {
	// 100ms, 200ms, 400ms
	return time.Duration(100*(1<<attempt)) * time.Millisecond
}

// bookmarkExists checks if jj bookmark list output actually contains a matching bookmark.
// jj exits 0 even for non-existent bookmarks, printing "Warning: No matching bookmarks".
// A real match has the bookmark name at the start of a non-warning line.
func bookmarkExists(output string, bookmarkName string) bool {
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "Warning:") || strings.HasPrefix(line, "Hint:") {
			continue
		}
		// jj bookmark list format: "name: <revid> description"
		// Match on exact name followed by ":" to avoid prefix false positives
		if strings.HasPrefix(line, bookmarkName+":") || line == bookmarkName {
			return true
		}
	}
	return false
}

// checkGHCLI checks if GitHub CLI is installed and configured.
func checkGHCLI() error {
	if _, err := exec.LookPath("gh"); err != nil {
		return fmt.Errorf("GitHub CLI (gh) is not installed. Please install it first")
	}
	cmd := exec.Command("gh", "auth", "status")
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("GitHub CLI is not configured. Please run 'gh auth login' first")
	}
	return nil
}
