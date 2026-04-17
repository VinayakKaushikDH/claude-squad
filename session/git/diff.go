package git

import (
	"strings"

	"claude-squad/log"
	"claude-squad/session/vcs"
)

// Diff returns the git diff between the worktree and the base branch along with statistics.
// All operations are read-only (no index.lock acquired) so multiple cs processes can
// safely poll the same worktree without contention.
func (g *GitWorktree) Diff() *vcs.DiffStats {
	stats := &vcs.DiffStats{}

	content, err := g.runGitCommand(g.worktreePath, "--no-pager", "diff", g.GetBaseCommitSHA())
	if err != nil {
		stats.Error = err
		return stats
	}

	// Count untracked files as added lines (read-only, no lock needed).
	// This replaces the old `git add -N .` approach which acquired index.lock.
	untrackedOut, err := g.runGitCommand(g.worktreePath, "ls-files", "--others", "--exclude-standard")
	if err != nil {
		log.WarningLog.Printf("failed to list untracked files: %v", err)
	}

	lines := strings.Split(content, "\n")
	for _, line := range lines {
		if strings.HasPrefix(line, "+") && !strings.HasPrefix(line, "+++") {
			stats.Added++
		} else if strings.HasPrefix(line, "-") && !strings.HasPrefix(line, "---") {
			stats.Removed++
		}
	}

	if untrackedOut != "" {
		untrackedFiles := strings.Split(strings.TrimSpace(untrackedOut), "\n")
		stats.Added += len(untrackedFiles)
	}

	stats.Content = content

	return stats
}
