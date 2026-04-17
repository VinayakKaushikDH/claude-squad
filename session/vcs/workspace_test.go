package vcs_test

import (
	"claude-squad/session/git"
	"claude-squad/session/vcs"
	"testing"
)

// TestGitWorktreeImplementsWorkspace is a compile-time check that *git.GitWorktree
// satisfies the vcs.Workspace interface.
func TestGitWorktreeImplementsWorkspace(t *testing.T) {
	var _ vcs.Workspace = (*git.GitWorktree)(nil)
}
