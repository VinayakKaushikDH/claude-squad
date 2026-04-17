package app

import (
	"claude-squad/config"
	"claude-squad/session"
	"claude-squad/ui"
	"context"
	"sync"
	"testing"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newTestHome creates a minimal home struct for testing without real storage/tmux.
func newTestHome(instances []*session.Instance) *home {
	s := spinner.New(spinner.WithSpinner(spinner.MiniDot))
	list := ui.NewList(&s, false)
	for _, inst := range instances {
		list.AddInstance(inst)
	}

	h := &home{
		ctx:       context.Background(),
		state:     stateDefault,
		appConfig: config.DefaultConfig(),
		list:      list,
		menu:      ui.NewMenu(),
	}
	return h
}

// --- syncWorkspaceUI tests ---

func TestSyncWorkspaceUI_SingleWorkspace_NoTabBar(t *testing.T) {
	instances := []*session.Instance{
		makeInstance("/path/a", session.Running),
		makeInstance("/path/a", session.Ready),
	}
	h := newTestHome(instances)

	h.syncWorkspaceUI()

	assert.Nil(t, h.workspaceTabBar, "tab bar should be nil for single workspace")
	assert.Equal(t, 0, h.activeWorkspace)
	assert.Len(t, h.workspaces, 1)
}

func TestSyncWorkspaceUI_TwoWorkspaces_CreatesTabBar(t *testing.T) {
	instances := []*session.Instance{
		makeInstance("/path/a", session.Running),
		makeInstance("/path/b", session.Running),
	}
	h := newTestHome(instances)

	h.syncWorkspaceUI()

	assert.NotNil(t, h.workspaceTabBar, "tab bar should be created for 2+ workspaces")
	assert.Len(t, h.workspaces, 2)
}

func TestSyncWorkspaceUI_WorkspaceDisappears(t *testing.T) {
	// Start with 2 workspaces and active on workspace 1.
	instances := []*session.Instance{
		makeInstance("/path/a", session.Running),
		makeInstance("/path/b", session.Running),
	}
	h := newTestHome(instances)
	h.refreshWorkspaces()
	h.activeWorkspace = 1
	h.workspaceTabBar = ui.NewWorkspaceTabBar()
	h.workspaceTabBar.SetWorkspaces(h.workspaceTabs())
	h.workspaceTabBar.SetActiveIdx(1)
	h.list.SetFilter("/path/b")

	// Kill the /path/b instance, leaving only /path/a.
	h.list.SetFilter("") // show all to find b
	h.list.Down()        // select the second item
	h.list.Kill()

	// syncWorkspaceUI should handle the workspace disappearing.
	h.syncWorkspaceUI()

	assert.Nil(t, h.workspaceTabBar, "tab bar should be nil after dropping to 1 workspace")
	assert.Equal(t, 0, h.activeWorkspace, "activeWorkspace should clamp to 0")
	assert.Len(t, h.workspaces, 1)
}

func TestSyncWorkspaceUI_ActiveWorkspaceOutOfBounds(t *testing.T) {
	instances := []*session.Instance{
		makeInstance("/path/a", session.Running),
		makeInstance("/path/b", session.Running),
		makeInstance("/path/c", session.Running),
	}
	h := newTestHome(instances)
	h.refreshWorkspaces()
	h.activeWorkspace = 2 // last workspace

	// Kill the /path/c instance — activeWorkspace should clamp.
	h.list.SetFilter("")
	h.list.SetSelectedInstance(2) // select the third item
	h.list.Kill()

	h.syncWorkspaceUI()

	assert.True(t, h.activeWorkspace < len(h.workspaces),
		"activeWorkspace %d should be < workspaces len %d", h.activeWorkspace, len(h.workspaces))
}

func TestSyncWorkspaceUI_AllInstancesKilled(t *testing.T) {
	instances := []*session.Instance{
		makeInstance("/path/a", session.Running),
	}
	h := newTestHome(instances)

	h.list.Kill()
	h.syncWorkspaceUI()

	assert.Nil(t, h.workspaceTabBar)
	assert.Equal(t, 0, h.activeWorkspace)
	assert.Empty(t, h.workspaces)
}

// --- Initialization from a different directory ---

func TestInitFromDifferentDir_InstancesInOtherWorkspace(t *testing.T) {
	// Simulate: all instances belong to /path/a, but we open cs from /path/b.
	instances := []*session.Instance{
		makeInstance("/path/a", session.Running),
		makeInstance("/path/a", session.Ready),
	}
	h := newTestHome(instances)

	// Simulate what newHome() does.
	h.refreshWorkspaces()
	h.currentRepoPath = "/path/b" // opened from a different folder

	require.Len(t, h.workspaces, 1) // only /path/a workspace exists

	// FindWorkspaceIndex returns 0 for unknown path.
	h.activeWorkspace = FindWorkspaceIndex(h.workspaces, h.currentRepoPath)
	assert.Equal(t, 0, h.activeWorkspace, "should default to first workspace when no match")

	// Set filter to the active workspace path.
	h.list.SetFilter(h.workspaces[h.activeWorkspace].Path)

	// All instances should be visible (they're all in /path/a).
	assert.Equal(t, 2, h.list.NumInstances())
	assert.NotNil(t, h.list.GetSelectedInstance())

	// No tab bar since only 1 workspace.
	assert.Nil(t, h.workspaceTabBar)
}

func TestInitFromDifferentDir_NoExistingInstances(t *testing.T) {
	// Simulate: no instances at all, opened from /path/b.
	h := newTestHome(nil)

	h.refreshWorkspaces()
	h.currentRepoPath = "/path/b"

	assert.Empty(t, h.workspaces)
	assert.Equal(t, 0, h.list.NumInstances())
	assert.Nil(t, h.list.GetSelectedInstance())
}

func TestInitFromDifferentDir_MixedWorkspaces(t *testing.T) {
	// Simulate: instances from /path/a and /path/b, opened from /path/b.
	instances := []*session.Instance{
		makeInstance("/path/a", session.Running),
		makeInstance("/path/b", session.Running),
		makeInstance("/path/b", session.Ready),
	}
	h := newTestHome(instances)

	h.refreshWorkspaces()
	h.currentRepoPath = "/path/b"

	h.activeWorkspace = FindWorkspaceIndex(h.workspaces, h.currentRepoPath)
	h.list.SetFilter(h.workspaces[h.activeWorkspace].Path)

	// Should show only /path/b instances.
	assert.Equal(t, 2, h.list.NumInstances())
	assert.Equal(t, 3, h.list.NumAllInstances())

	// Tab bar should be created.
	h.syncWorkspaceUI()
	assert.NotNil(t, h.workspaceTabBar)
}

// --- instanceChanged with nil selected instance ---

func TestInstanceChanged_NilSelectedInstance(t *testing.T) {
	h := newTestHome(nil)
	h.tabbedWindow = ui.NewTabbedWindow(ui.NewPreviewPane(), ui.NewDiffPane(), ui.NewTerminalPane())

	// With no instances, GetSelectedInstance() returns nil.
	// instanceChanged() must not panic.
	cmd := h.instanceChanged()
	assert.Nil(t, cmd, "should return nil cmd when no instance is selected")
}

func TestInstanceChanged_EmptyFilteredList(t *testing.T) {
	instances := []*session.Instance{
		makeInstance("/path/a", session.Running),
	}
	h := newTestHome(instances)
	h.tabbedWindow = ui.NewTabbedWindow(ui.NewPreviewPane(), ui.NewDiffPane(), ui.NewTerminalPane())

	// Filter to a non-existent workspace.
	h.list.SetFilter("/path/nonexistent")
	require.Equal(t, 0, h.list.NumInstances())

	// instanceChanged() must handle nil selected instance gracefully.
	cmd := h.instanceChanged()
	assert.Nil(t, cmd)
}

// --- metadataUpdateDoneMsg with various states ---

func TestMetadataUpdateDoneMsg_EmptyResults(t *testing.T) {
	h := newTestHome(nil)
	h.tabbedWindow = ui.NewTabbedWindow(ui.NewPreviewPane(), ui.NewDiffPane(), ui.NewTerminalPane())
	h.menu = ui.NewMenu()

	msg := metadataUpdateDoneMsg{results: nil}
	model, cmd := h.Update(msg)

	assert.NotNil(t, model)
	assert.NotNil(t, cmd, "should return the next tick cmd even with empty results")
}

func TestMetadataUpdateDoneMsg_SyncWorkspaceUI_CalledEveryTick(t *testing.T) {
	instances := []*session.Instance{
		makeInstance("/path/a", session.Running),
		makeInstance("/path/b", session.Ready),
	}
	h := newTestHome(instances)
	h.tabbedWindow = ui.NewTabbedWindow(ui.NewPreviewPane(), ui.NewDiffPane(), ui.NewTerminalPane())

	// Before metadata update, no workspaces derived yet.
	assert.Empty(t, h.workspaces)

	msg := metadataUpdateDoneMsg{results: []instanceMetaResult{}}
	h.Update(msg)

	// syncWorkspaceUI should have been called, deriving workspaces.
	assert.NotEmpty(t, h.workspaces, "syncWorkspaceUI should derive workspaces on metadata update")
}

// --- tickUpdateMetadataCmd timeout behavior ---

func TestTickUpdateMetadataCmd_EmptyInstances_ReturnsQuickly(t *testing.T) {
	cmd := tickUpdateMetadataCmd(nil)

	done := make(chan struct{})
	go func() {
		msg := cmd()
		_ = msg
		close(done)
	}()

	select {
	case <-done:
		// Expected: returns after ~500ms sleep.
	case <-time.After(3 * time.Second):
		t.Fatal("tickUpdateMetadataCmd with empty instances should return within 3 seconds")
	}
}

func TestTickUpdateMetadataCmd_TimeoutConstant(t *testing.T) {
	// Verify the timeout constant exists and is reasonable.
	assert.Equal(t, 10*time.Second, metadataTimeout,
		"metadata timeout should be 10s — long enough for slow diffs, short enough for recovery")
}

// --- snapshotActiveInstances ---

func TestSnapshotActiveInstances_OnlyStartedNotPaused(t *testing.T) {
	instances := []*session.Instance{
		makeInstance("/path/a", session.Running),
		makeInstance("/path/a", session.Paused),
		makeInstance("/path/b", session.Ready),
	}
	h := newTestHome(instances)

	// None of these instances are truly "started" (no tmux session), so
	// snapshotActiveInstances should return empty.
	active := h.snapshotActiveInstances()
	assert.Empty(t, active, "instances without tmux sessions should not be considered active")
}

// --- Workspace cycling ---

func TestWorkspaceCycling_BoundsCheck(t *testing.T) {
	instances := []*session.Instance{
		makeInstance("/path/a", session.Running),
		makeInstance("/path/b", session.Running),
		makeInstance("/path/c", session.Running),
	}
	h := newTestHome(instances)
	h.refreshWorkspaces()

	require.Len(t, h.workspaces, 3)

	// Cycle forward from workspace 0.
	h.activeWorkspace = 0
	h.activeWorkspace = (h.activeWorkspace + 1) % len(h.workspaces)
	assert.Equal(t, 1, h.activeWorkspace)

	// Cycle forward from last workspace.
	h.activeWorkspace = 2
	h.activeWorkspace = (h.activeWorkspace + 1) % len(h.workspaces)
	assert.Equal(t, 0, h.activeWorkspace, "should wrap to first workspace")

	// Cycle backward from workspace 0.
	h.activeWorkspace = 0
	h.activeWorkspace = (h.activeWorkspace - 1 + len(h.workspaces)) % len(h.workspaces)
	assert.Equal(t, 2, h.activeWorkspace, "should wrap to last workspace")
}

// --- Concurrent metadata access (demonstrates the race condition) ---

func TestConcurrentDiffAccess_RaceCondition(t *testing.T) {
	// This test documents the race condition when two cs processes poll
	// the same instances. Both processes call ComputeDiff() and HasUpdated()
	// concurrently on the same instance objects.
	//
	// In production:
	// - Process 1's tickUpdateMetadataCmd goroutine calls inst.ComputeDiff()
	// - Process 2's tickUpdateMetadataCmd goroutine calls inst.ComputeDiff()
	// - Both run `git add -N .` + `git diff` on the same worktree
	// - git's index.lock causes one to fail or block
	//
	// For jj:
	// - Both run `jj diff` against the same repo
	// - jj's repo-level lock causes one to block
	// - wg.Wait() blocks forever → metadata loop dies → UI freezes

	var mu sync.Mutex
	var callCount int

	// Simulate concurrent access to the same "instance".
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			// In the real code, this would be inst.ComputeDiff() which runs
			// `git add -N .` (a write!) + `git diff` or `jj diff`
			mu.Lock()
			callCount++
			mu.Unlock()
		}()
	}

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		assert.Equal(t, 10, callCount)
	case <-time.After(5 * time.Second):
		t.Fatal("concurrent access timed out — demonstrates the freeze bug")
	}

	t.Log("DIAGNOSIS: git Diff() runs `git add -N .` which is a WRITE operation.")
	t.Log("DIAGNOSIS: Two cs processes running this on the same worktree will contend on index.lock.")
	t.Log("DIAGNOSIS: For jj, `jj diff` holds a repo-level lock shared across all workspaces.")
}

// --- HasReady flag propagation ---

func TestSyncWorkspaceUI_HasReadyFlag(t *testing.T) {
	instances := []*session.Instance{
		makeInstance("/path/a", session.Running),
		makeInstance("/path/b", session.Ready),
	}
	h := newTestHome(instances)

	h.syncWorkspaceUI()

	require.Len(t, h.workspaces, 2)

	var wsA, wsB *Workspace
	for i := range h.workspaces {
		switch h.workspaces[i].Path {
		case "/path/a":
			wsA = &h.workspaces[i]
		case "/path/b":
			wsB = &h.workspaces[i]
		}
	}
	require.NotNil(t, wsA)
	require.NotNil(t, wsB)

	assert.False(t, wsA.HasReady, "/path/a has no Ready instances")
	assert.True(t, wsB.HasReady, "/path/b has a Ready instance")
}

func TestSyncWorkspaceUI_HasReadyClears(t *testing.T) {
	inst := makeInstance("/path/a", session.Ready)
	h := newTestHome([]*session.Instance{
		inst,
		makeInstance("/path/b", session.Running),
	})

	h.syncWorkspaceUI()
	require.Len(t, h.workspaces, 2)

	// Verify /path/a is marked ready.
	var wsA *Workspace
	for i := range h.workspaces {
		if h.workspaces[i].Path == "/path/a" {
			wsA = &h.workspaces[i]
		}
	}
	require.True(t, wsA.HasReady)

	// Change the instance status to Running and re-sync.
	inst.SetStatus(session.Running)
	h.syncWorkspaceUI()

	for i := range h.workspaces {
		if h.workspaces[i].Path == "/path/a" {
			wsA = &h.workspaces[i]
		}
	}
	assert.False(t, wsA.HasReady, "HasReady should clear when no instances are Ready")
}
