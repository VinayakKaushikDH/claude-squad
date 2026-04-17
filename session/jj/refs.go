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

	entries, err := os.ReadDir(configDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("failed to read workspace directory: %w", err)
	}

	for _, entry := range entries {
		if entry.IsDir() {
			wsPath := filepath.Join(configDir, entry.Name())
			// We skip `jj workspace forget` here because we don't have a repo path to
			// pass via --repository. jj will detect orphaned workspaces on next operation
			// and clean them up automatically. Just remove the directory.
			os.RemoveAll(wsPath)
		}
	}

	return nil
}
