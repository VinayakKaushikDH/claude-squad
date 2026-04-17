package jj

import (
	"claude-squad/session/vcs"
	"fmt"
	"strings"
)

// Diff returns the diff between the workspace and the base change, with statistics.
func (j *JJWorkspace) Diff() *vcs.DiffStats {
	stats := &vcs.DiffStats{}

	if j.baseChangeID == "" {
		// Use the same sentinel string as GitWorktree so callers can filter consistently
		stats.Error = fmt.Errorf("base commit SHA not set")
		return stats
	}

	// jj diff includes untracked files automatically — no staging step needed
	content, err := runJJCommand(j.workspacePath, "diff", "--from", j.baseChangeID, "--git")
	if err != nil {
		stats.Error = err
		return stats
	}

	lines := strings.Split(content, "\n")
	for _, line := range lines {
		if strings.HasPrefix(line, "+") && !strings.HasPrefix(line, "+++") {
			stats.Added++
		} else if strings.HasPrefix(line, "-") && !strings.HasPrefix(line, "---") {
			stats.Removed++
		}
	}
	stats.Content = content

	return stats
}
