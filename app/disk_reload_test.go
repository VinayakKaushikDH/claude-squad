package app

import (
	"claude-squad/session"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// makeDiskInstance creates an InstanceData suitable for merge tests.
// It includes valid git worktree JSON so FromInstanceData won't fail.
func makeDiskInstance(id, title, path string) session.InstanceData {
	wt := session.GitWorktreeData{
		RepoPath:     path,
		WorktreePath: path + "/.worktrees/" + title,
		SessionName:  "cs-" + title,
		BranchName:   "cs/" + title,
	}
	wtJSON, _ := json.Marshal(wt)
	return session.InstanceData{
		ID:       id,
		Title:    title,
		Path:     path,
		Branch:   "cs/" + title,
		Status:   session.Paused,
		VCSType:  "git",
		Worktree: wtJSON,
		Program:  "claude",
	}
}

func TestDiskReload_NewInstanceAppearsInList(t *testing.T) {
	th := newTestHarness(t, nil)
	th.h.locallyDeleted = make(map[string]bool)

	require.Equal(t, 0, th.h.list.NumInstances())

	// Simulate disk reload with a new instance.
	diskData := []session.InstanceData{makeDiskInstance("id-1", "remote-task", "/path/a")}
	th.h.mergeReloadedInstances(diskData)

	assert.Equal(t, 1, th.h.list.NumInstances(), "new instance from disk should appear in list")
	inst := th.h.list.GetInstanceByID("id-1")
	assert.NotNil(t, inst)
	assert.Equal(t, "remote-task", inst.Title)
}

func TestDiskReload_DeletedInstanceRemovedFromList(t *testing.T) {
	// Start with one in-memory instance marked as started.
	inst := &session.Instance{
		ID:     "id-1",
		Title:  "local-task",
		Path:   "/path/a",
		Status: session.Paused,
	}
	inst.SetStartedForTest()
	th := newTestHarness(t, []*session.Instance{inst})
	th.h.locallyDeleted = make(map[string]bool)

	require.Equal(t, 1, th.h.list.NumInstances())
	require.True(t, inst.Started())

	// Simulate disk reload with empty data (instance deleted by another process).
	// Rule 3: in memory + not on disk + Started() + not editingInstance → remove.
	th.h.mergeReloadedInstances(nil)

	assert.Equal(t, 0, th.h.list.NumInstances(), "started instance not on disk should be removed")
}

func TestDiskReload_UnstartedInstanceKept(t *testing.T) {
	// Rule 4: in memory + not on disk + !Started() → keep (still being created).
	inst := &session.Instance{
		ID:     "id-1",
		Title:  "local-task",
		Path:   "/path/a",
		Status: session.Paused,
	}
	th := newTestHarness(t, []*session.Instance{inst})
	th.h.locallyDeleted = make(map[string]bool)

	require.False(t, inst.Started())
	th.h.mergeReloadedInstances(nil)

	assert.Equal(t, 1, th.h.list.NumInstances(), "unstarted instances should be kept even if not on disk")
}

func TestDiskReload_LocallyDeletedNotReadded(t *testing.T) {
	th := newTestHarness(t, nil)
	th.h.locallyDeleted = map[string]bool{"id-deleted": true}

	// Simulate disk reload with an instance that was locally deleted.
	diskData := []session.InstanceData{makeDiskInstance("id-deleted", "deleted-task", "/path/a")}
	th.h.mergeReloadedInstances(diskData)

	assert.Equal(t, 0, th.h.list.NumInstances(), "locally deleted instance should not be re-added")
}

func TestDiskReload_EditingInstancePreserved(t *testing.T) {
	th := newTestHarness(t, nil)
	th.h.locallyDeleted = make(map[string]bool)

	// Create a new instance being edited.
	newInst := th.CreateNewInstance("/path/a")
	th.AssertState(stateNew)
	require.Equal(t, 1, th.h.list.NumInstances())

	// Simulate disk reload with no instances on disk.
	th.h.mergeReloadedInstances(nil)

	// The instance being edited should be preserved (rule 4: not started).
	assert.Equal(t, 1, th.h.list.NumInstances(), "instance being created should survive reload")
	assert.Same(t, newInst, th.EditingInstance())
}

func TestDiskReload_MergeUpdatesMetadataOnly(t *testing.T) {
	inst := &session.Instance{
		ID:     "id-1",
		Title:  "my-task",
		Path:   "/path/a",
		Status: session.Running,
	}
	th := newTestHarness(t, []*session.Instance{inst})
	th.h.locallyDeleted = make(map[string]bool)

	// Simulate disk reload with updated diff stats.
	d := makeDiskInstance("id-1", "my-task", "/path/a")
	d.DiffStats = session.DiffStatsData{Added: 15, Removed: 3, Content: "+new\n-old\n"}
	diskData := []session.InstanceData{d}
	th.h.mergeReloadedInstances(diskData)

	// The same instance object should be reused (not replaced).
	assert.Equal(t, 1, th.h.list.NumInstances())
	found := th.h.list.GetInstanceByID("id-1")
	assert.Same(t, inst, found, "should reuse existing instance, not create a new one")

	// Diff stats should be updated.
	stats := inst.GetDiffStats()
	require.NotNil(t, stats)
	assert.Equal(t, 15, stats.Added)
	assert.Equal(t, 3, stats.Removed)
}

func TestDiskReload_BackwardCompat_FallbackToTitle(t *testing.T) {
	// Instance with empty ID (old state file format).
	inst := &session.Instance{
		ID:     "",
		Title:  "legacy-task",
		Path:   "/path/a",
		Status: session.Running,
	}
	th := newTestHarness(t, []*session.Instance{inst})
	th.h.locallyDeleted = make(map[string]bool)

	// Disk data also has empty ID — should match by title.
	d := makeDiskInstance("", "legacy-task", "/path/a")
	d.DiffStats = session.DiffStatsData{Added: 5, Removed: 2}
	diskData := []session.InstanceData{d}
	th.h.mergeReloadedInstances(diskData)

	// Should not create a duplicate.
	assert.Equal(t, 1, th.h.list.NumInstances())
	stats := inst.GetDiffStats()
	require.NotNil(t, stats)
	assert.Equal(t, 5, stats.Added)
}

func TestDiskReload_MultipleNewInstances(t *testing.T) {
	th := newTestHarness(t, nil)
	th.h.locallyDeleted = make(map[string]bool)

	diskData := []session.InstanceData{
		makeDiskInstance("id-1", "task-1", "/path/a"),
		makeDiskInstance("id-2", "task-2", "/path/b"),
	}
	th.h.mergeReloadedInstances(diskData)

	assert.Equal(t, 2, th.h.list.NumInstances())
	assert.NotNil(t, th.h.list.GetInstanceByID("id-1"))
	assert.NotNil(t, th.h.list.GetInstanceByID("id-2"))
}

func TestDiskReload_ReloadTickCounter(t *testing.T) {
	th := newTestHarness(t, nil)
	th.h.locallyDeleted = make(map[string]bool)

	// Simulate 9 ticks — no reload should trigger.
	for i := 0; i < 9; i++ {
		th.SimulateMetadataTick(nil)
	}
	assert.Equal(t, 9, th.h.reloadTickCounter, "counter should be 9 after 9 ticks")

	// 10th tick should reset counter to 0.
	th.SimulateMetadataTick(nil)
	assert.Equal(t, 0, th.h.reloadTickCounter, "counter should reset after 10 ticks")
}

func TestTabBar_SingleWorkspace(t *testing.T) {
	instances := []*session.Instance{
		makeInstance("/path/a", session.Running),
	}
	th := newTestHarness(t, instances)

	th.h.syncWorkspaceUI()

	assert.NotNil(t, th.h.workspaceTabBar, "tab bar should be visible with single workspace")
	assert.Len(t, th.h.workspaces, 1)
}

func TestTabBar_ZeroWorkspaces(t *testing.T) {
	th := newTestHarness(t, nil)

	th.h.syncWorkspaceUI()

	assert.Nil(t, th.h.workspaceTabBar, "tab bar should be nil with zero workspaces")
	assert.Empty(t, th.h.workspaces)
}
