package vcs

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
