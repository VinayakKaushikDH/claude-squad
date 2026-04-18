package jj

import (
	"claude-squad/session/vcs"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// diffTimeout limits how long a jj diff command can run. If the command takes
// longer (e.g., due to repo lock contention with another cs process), it's
// killed and an error is returned. The metadata loop will retry on the next tick.
const diffTimeout = 5 * time.Second

// Diff returns the diff between the workspace and the base change, with statistics.
func (j *JJWorkspace) Diff() *vcs.DiffStats {
	stats := &vcs.DiffStats{}

	if j.baseChangeID == "" {
		stats.Error = fmt.Errorf("base commit SHA not set")
		return stats
	}

	// jj diff includes untracked files automatically — no staging step needed.
	// Use a timeout to cover both the snapshot and diff steps.
	ctx, cancel := context.WithTimeout(context.Background(), diffTimeout)
	defer cancel()

	// Snapshot the working copy before diffing. jj diff uses --ignore-working-copy
	// (to avoid repeatedly staling other workspaces on every poll tick), which means
	// it reads from the last snapshot — not the live filesystem. Without this call
	// the diff panel shows stale data whenever the agent writes files without running
	// jj commands itself. Non-fatal: if the snapshot fails we fall through and diff
	// against whatever snapshot currently exists.
	snapCmd := exec.CommandContext(ctx, "jj", "--repository", j.workspacePath, "status")
	snapCmd.CombinedOutput() //nolint:errcheck // snapshot errors are non-fatal

	// --ignore-working-copy: reads from the fresh snapshot created above.
	args := []string{"--repository", j.workspacePath, "diff", "--from", j.baseChangeID, "--git", "--ignore-working-copy"}
	cmd := exec.CommandContext(ctx, "jj", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		stats.Error = fmt.Errorf("jj diff failed: %s (%w)", output, err)
		return stats
	}
	content := string(output)

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
