package session

import (
	"errors"
	"testing"

	"claude-squad/session/vcs"
)

// mockWorkspace implements vcs.Workspace for testing.
type mockWorkspace struct {
	canRemoveErr   error
	canResumeErr   error
	pushChangesErr error
	checkoutErr    error
	worktreePath   string
	branchName     string
}

func (m *mockWorkspace) Setup() error                             { return nil }
func (m *mockWorkspace) Cleanup() error                          { return nil }
func (m *mockWorkspace) Remove() error                           { return nil }
func (m *mockWorkspace) IsDirty() (bool, error)                  { return false, nil }
func (m *mockWorkspace) Diff() *vcs.DiffStats                    { return &vcs.DiffStats{} }
func (m *mockWorkspace) CommitChanges(msg string) error          { return nil }
func (m *mockWorkspace) PushChanges(msg string, open bool) error { return m.pushChangesErr }
func (m *mockWorkspace) CheckoutInMainRepo() error               { return m.checkoutErr }
func (m *mockWorkspace) CanResume() error                        { return m.canResumeErr }
func (m *mockWorkspace) CanRemove() error                        { return m.canRemoveErr }
func (m *mockWorkspace) GetWorktreePath() string                 { return m.worktreePath }
func (m *mockWorkspace) GetBranchName() string                   { return m.branchName }
func (m *mockWorkspace) GetRepoPath() string                     { return "" }
func (m *mockWorkspace) GetRepoName() string                     { return "" }
func (m *mockWorkspace) GetBaseCommitSHA() string                { return "" }
func (m *mockWorkspace) IsExistingBranch() bool                  { return false }

func TestCanKill_notStarted(t *testing.T) {
	i := &Instance{}
	if err := i.CanKill(); err != nil {
		t.Errorf("CanKill() on unstarted instance = %v, want nil", err)
	}
}

func TestCanKill_started_nilError(t *testing.T) {
	mock := &mockWorkspace{canRemoveErr: nil}
	i := &Instance{started: true, workspace: mock}
	if got := i.CanKill(); got != nil {
		t.Errorf("CanKill() = %v, want nil", got)
	}
}

func TestCanKill_started_delegatesError(t *testing.T) {
	want := errors.New("cannot remove: branch checked out")
	mock := &mockWorkspace{canRemoveErr: want}
	i := &Instance{started: true, workspace: mock}
	if got := i.CanKill(); got != want {
		t.Errorf("CanKill() = %v, want %v", got, want)
	}
}

func TestPushChanges_notStarted(t *testing.T) {
	i := &Instance{}
	if err := i.PushChanges("msg", false); err == nil {
		t.Error("PushChanges() on unstarted instance should return error, got nil")
	}
}

func TestPushChanges_started_delegatesNilError(t *testing.T) {
	mock := &mockWorkspace{pushChangesErr: nil}
	i := &Instance{started: true, workspace: mock}
	if got := i.PushChanges("msg", false); got != nil {
		t.Errorf("PushChanges() = %v, want nil", got)
	}
}

func TestPushChanges_started_delegatesError(t *testing.T) {
	want := errors.New("push failed")
	mock := &mockWorkspace{pushChangesErr: want}
	i := &Instance{started: true, workspace: mock}
	if got := i.PushChanges("msg", false); got != want {
		t.Errorf("PushChanges() = %v, want %v", got, want)
	}
}

func TestCheckoutInMainRepo_notStarted(t *testing.T) {
	i := &Instance{}
	if err := i.CheckoutInMainRepo(); err == nil {
		t.Error("CheckoutInMainRepo() on unstarted instance should return error, got nil")
	}
}

func TestCheckoutInMainRepo_delegatesSentinel(t *testing.T) {
	mock := &mockWorkspace{checkoutErr: vcs.ErrCheckoutRequiresPause}
	i := &Instance{started: true, workspace: mock}
	got := i.CheckoutInMainRepo()
	if !errors.Is(got, vcs.ErrCheckoutRequiresPause) {
		t.Errorf("CheckoutInMainRepo() = %v, want ErrCheckoutRequiresPause", got)
	}
}

func TestCheckoutInMainRepo_delegatesNilError(t *testing.T) {
	mock := &mockWorkspace{checkoutErr: nil}
	i := &Instance{started: true, workspace: mock}
	if got := i.CheckoutInMainRepo(); got != nil {
		t.Errorf("CheckoutInMainRepo() = %v, want nil", got)
	}
}

func TestCheckoutInMainRepo_delegatesArbitraryError(t *testing.T) {
	want := errors.New("bookmark not found")
	mock := &mockWorkspace{checkoutErr: want}
	i := &Instance{started: true, workspace: mock}
	if got := i.CheckoutInMainRepo(); got != want {
		t.Errorf("CheckoutInMainRepo() = %v, want %v", got, want)
	}
}
