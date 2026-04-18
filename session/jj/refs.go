package jj

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// MaxBookmarkSearchResults is the maximum number of bookmarks returned by SearchBookmarks.
const MaxBookmarkSearchResults = 50

// FetchBookmarks fetches remote bookmarks (best-effort, won't fail if offline).
func FetchBookmarks(repoPath string) {
	cmd := exec.Command("jj", "--repository", repoPath, "git", "fetch", "--ignore-working-copy")
	_ = cmd.Run()
}

// SearchBookmarks searches for bookmarks whose name contains filter (case-insensitive).
// Returns at most MaxBookmarkSearchResults. If filter is empty, returns all bookmarks up to the limit.
func SearchBookmarks(repoPath, filter string) ([]string, error) {
	cmd := exec.Command("jj", "--repository", repoPath, "bookmark", "list", "--ignore-working-copy")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("failed to list bookmarks: %s (%w)", output, err)
	}

	seen := make(map[string]bool)
	var bookmarks []string
	lower := strings.ToLower(filter)
	for _, line := range strings.Split(strings.TrimSpace(string(output)), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "Warning:") || strings.HasPrefix(line, "Hint:") {
			continue
		}
		// jj bookmark list format: "bookmark-name: <change_id> <description>"
		// Extract just the bookmark name (before the colon)
		name := line
		if idx := strings.Index(line, ":"); idx > 0 {
			name = line[:idx]
		}
		name = strings.TrimSpace(name)
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		if filter != "" && !strings.Contains(strings.ToLower(name), lower) {
			continue
		}
		bookmarks = append(bookmarks, name)
		if len(bookmarks) >= MaxBookmarkSearchResults {
			break
		}
	}
	return bookmarks, nil
}

// CleanupWorkspaces removes all claude-squad workspaces and their directories.
func CleanupWorkspaces() error {
	configDir, err := getWorkspaceDirectory()
	if err != nil {
		return fmt.Errorf("failed to get workspace directory: %w", err)
	}

	if _, err := os.Stat(configDir); os.IsNotExist(err) {
		return nil
	}

	// Walk the workspace directory and forget every jj workspace registration
	// before deleting its directory. Without this step, jj retains stale workspace
	// entries in the repo's op log; subsequent `jj log` / `jj status` calls print
	// "working copy is stale" errors for each phantom workspace.
	filepath.Walk(configDir, func(path string, info os.FileInfo, err error) error { //nolint:errcheck
		if err != nil || !info.IsDir() || filepath.Base(path) != ".jj" {
			return nil
		}
		wsPath := filepath.Dir(path)
		wsName := filepath.Base(wsPath)
		if repoPath, findErr := findJJRepoRoot(wsPath); findErr == nil {
			_, _ = runJJCommandWithRetry(repoPath, "workspace", "forget", wsName, "--ignore-working-copy")
		}
		return filepath.SkipDir // don't descend into .jj internals
	})

	// Remove all workspace directories.
	entries, err := os.ReadDir(configDir)
	if err != nil {
		return fmt.Errorf("failed to read workspace directory: %w", err)
	}
	for _, entry := range entries {
		if entry.IsDir() {
			os.RemoveAll(filepath.Join(configDir, entry.Name()))
		}
	}

	return nil
}
