package vcs

import "errors"

// ErrCheckoutRequiresPause is returned by CheckoutInMainRepo when the VCS backend
// cannot checkout in the main repo without pausing the session (e.g. git worktrees).
var ErrCheckoutRequiresPause = errors.New("checkout requires pausing the session")

// Workspace abstracts VCS-specific operations for a session's working directory.
type Workspace interface {
	// Lifecycle
	Setup() error
	Cleanup() error
	Remove() error

	// State
	IsDirty() (bool, error)
	Diff() *DiffStats

	// Mutations
	CommitChanges(msg string) error
	PushChanges(msg string, open bool) error
	CheckoutInMainRepo() error

	// Guards
	CanResume() error
	CanRemove() error

	// Identity
	GetWorktreePath() string
	GetBranchName() string
	GetRepoPath() string
	GetRepoName() string
	GetBaseCommitSHA() string
	IsExistingBranch() bool
}
