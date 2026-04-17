package ui

import (
	"claude-squad/log"
	"claude-squad/session"
	"os"
	"testing"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMain(m *testing.M) {
	log.Initialize(false)
	defer log.Close()
	os.Exit(m.Run())
}

func newTestList() *List {
	s := spinner.New(spinner.WithSpinner(spinner.MiniDot))
	return NewList(&s, false)
}

func addTestInstance(l *List, title, path string, status session.Status) *session.Instance {
	inst := &session.Instance{
		Title:  title,
		Path:   path,
		Status: status,
	}
	l.AddInstance(inst)
	return inst
}

// --- SetFilter edge cases ---

func TestSetFilter_NoMatchingInstances(t *testing.T) {
	l := newTestList()
	addTestInstance(l, "inst1", "/path/a", session.Running)
	addTestInstance(l, "inst2", "/path/a", session.Ready)

	// Filter to a path that has no instances.
	l.SetFilter("/path/b")

	assert.Equal(t, 0, l.NumInstances(), "visible count should be 0 when no instances match filter")
	assert.Equal(t, 2, l.NumAllInstances(), "total count should still be 2")
	assert.Nil(t, l.GetSelectedInstance(), "selected instance should be nil when filteredIdxs is empty")
	assert.Empty(t, l.GetVisibleInstances(), "visible instances should be empty")
}

func TestSetFilter_EmptyPath_ShowsAll(t *testing.T) {
	l := newTestList()
	addTestInstance(l, "inst1", "/path/a", session.Running)
	addTestInstance(l, "inst2", "/path/b", session.Running)

	l.SetFilter("")

	assert.Equal(t, 2, l.NumInstances(), "empty filter should show all instances")
	assert.NotNil(t, l.GetSelectedInstance())
}

func TestSetFilter_SwitchBetweenWorkspaces_PreservesSelection(t *testing.T) {
	l := newTestList()
	addTestInstance(l, "a1", "/path/a", session.Running)
	addTestInstance(l, "a2", "/path/a", session.Running)
	addTestInstance(l, "b1", "/path/b", session.Running)

	// Filter to /path/a and select second item.
	l.SetFilter("/path/a")
	require.Equal(t, 2, l.NumInstances())
	l.Down()
	assert.Equal(t, "a2", l.GetSelectedInstance().Title)

	// Switch to /path/b.
	l.SetFilter("/path/b")
	assert.Equal(t, 1, l.NumInstances())
	assert.Equal(t, "b1", l.GetSelectedInstance().Title)

	// Switch back to /path/a — selection should be restored to idx 1.
	l.SetFilter("/path/a")
	assert.Equal(t, 2, l.NumInstances())
	assert.Equal(t, "a2", l.GetSelectedInstance().Title, "selection should be restored via selectionMemo")
}

func TestSetFilter_ToEmptyThenBack(t *testing.T) {
	l := newTestList()
	addTestInstance(l, "a1", "/path/a", session.Running)
	addTestInstance(l, "b1", "/path/b", session.Running)

	// Filter to a non-existent path (empty visible list).
	l.SetFilter("/path/c")
	assert.Equal(t, 0, l.NumInstances())
	assert.Nil(t, l.GetSelectedInstance())

	// Switch back to all.
	l.SetFilter("")
	assert.Equal(t, 2, l.NumInstances())
	assert.NotNil(t, l.GetSelectedInstance())
}

// --- Navigation with empty filtered list ---

func TestNavigation_EmptyFilteredList(t *testing.T) {
	l := newTestList()
	addTestInstance(l, "inst1", "/path/a", session.Running)

	l.SetFilter("/path/nonexistent")
	require.Equal(t, 0, l.NumInstances())

	// Up and Down should not panic or change state.
	l.Up()
	l.Down()
	assert.Nil(t, l.GetSelectedInstance())
}

func TestNavigation_SingleItem(t *testing.T) {
	l := newTestList()
	addTestInstance(l, "inst1", "/path/a", session.Running)

	l.SetFilter("/path/a")
	require.Equal(t, 1, l.NumInstances())

	// Down should not go past the single item.
	l.Down()
	assert.Equal(t, "inst1", l.GetSelectedInstance().Title)

	// Up should stay at the same item.
	l.Up()
	assert.Equal(t, "inst1", l.GetSelectedInstance().Title)
}

// --- Kill with filtered view ---

func TestKill_RemovesFromFilteredView(t *testing.T) {
	l := newTestList()
	addTestInstance(l, "a1", "/path/a", session.Running)
	addTestInstance(l, "b1", "/path/b", session.Running)
	addTestInstance(l, "a2", "/path/a", session.Running)

	l.SetFilter("/path/a")
	require.Equal(t, 2, l.NumInstances())

	// Kill the first visible item (a1).
	l.Kill()

	// Should still show 1 item for /path/a.
	assert.Equal(t, 1, l.NumInstances(), "filtered count should be 1 after kill")
	assert.Equal(t, 2, l.NumAllInstances(), "total count should be 2 after kill")
	assert.Equal(t, "a2", l.GetSelectedInstance().Title)
}

func TestKill_LastInWorkspace_LeavesEmptyFiltered(t *testing.T) {
	l := newTestList()
	addTestInstance(l, "a1", "/path/a", session.Running)
	addTestInstance(l, "b1", "/path/b", session.Running)

	l.SetFilter("/path/a")
	require.Equal(t, 1, l.NumInstances())

	// Kill the only item in workspace a.
	l.Kill()

	assert.Equal(t, 0, l.NumInstances(), "no visible instances after killing the last one")
	assert.Equal(t, 1, l.NumAllInstances(), "b1 should still exist in the total list")
	assert.Nil(t, l.GetSelectedInstance())
}

// --- AddInstance with filter ---

func TestAddInstance_RecomputesFilter(t *testing.T) {
	l := newTestList()
	addTestInstance(l, "a1", "/path/a", session.Running)

	l.SetFilter("/path/b")
	require.Equal(t, 0, l.NumInstances())

	// Add an instance to /path/b — filter should now show it.
	addTestInstance(l, "b1", "/path/b", session.Running)
	assert.Equal(t, 1, l.NumInstances())
	assert.Equal(t, "b1", l.GetSelectedInstance().Title)
}

func TestAddInstance_DifferentWorkspace_NotVisible(t *testing.T) {
	l := newTestList()
	addTestInstance(l, "a1", "/path/a", session.Running)

	l.SetFilter("/path/a")
	require.Equal(t, 1, l.NumInstances())

	// Add an instance to a different path — shouldn't appear.
	addTestInstance(l, "b1", "/path/b", session.Running)
	assert.Equal(t, 1, l.NumInstances())
	assert.Equal(t, 2, l.NumAllInstances())
	assert.Equal(t, "a1", l.GetSelectedInstance().Title)
}

// --- String() rendering with empty filtered list ---

func TestString_EmptyFilteredList_NoPanic(t *testing.T) {
	l := newTestList()
	l.SetSize(80, 40)
	addTestInstance(l, "a1", "/path/a", session.Running)

	l.SetFilter("/path/nonexistent")
	require.Equal(t, 0, l.NumInstances())

	// String() should not panic on an empty filtered list.
	output := l.String()
	assert.NotEmpty(t, output, "should render something even with no visible instances")
	assert.Contains(t, output, "Instances", "should still show the title")
}

// --- SelectInstance with filtered view ---

func TestSelectInstance_InDifferentWorkspace(t *testing.T) {
	l := newTestList()
	a1 := addTestInstance(l, "a1", "/path/a", session.Running)
	b1 := addTestInstance(l, "b1", "/path/b", session.Running)

	l.SetFilter("/path/a")
	require.Equal(t, "a1", l.GetSelectedInstance().Title)

	// Try to select b1, which is not visible.
	l.SelectInstance(b1)
	// Selection should not change because b1 is not in the filtered view.
	assert.Equal(t, "a1", l.GetSelectedInstance().Title)

	// Select a1 explicitly — should work.
	l.SelectInstance(a1)
	assert.Equal(t, "a1", l.GetSelectedInstance().Title)
}

// --- SetSelectedInstance bounds ---

func TestSetSelectedInstance_OutOfBounds(t *testing.T) {
	l := newTestList()
	addTestInstance(l, "a1", "/path/a", session.Running)

	l.SetFilter("/path/a")
	require.Equal(t, 1, l.NumInstances())

	// Setting an index beyond the filtered list should be a no-op.
	l.SetSelectedInstance(5)
	assert.Equal(t, "a1", l.GetSelectedInstance().Title)
}
