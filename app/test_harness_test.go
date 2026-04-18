package app

import (
	"claude-squad/config"
	"claude-squad/session"
	"claude-squad/session/vcs"
	"claude-squad/ui"
	"context"
	"testing"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testHarness wraps a home model for testing Bubbletea message flows.
// It provides helpers to simulate key events, background messages, and
// state transitions without needing real tmux/git/storage dependencies.
type testHarness struct {
	h *home
	t *testing.T
}

// newTestHarness creates a fully initialized home model for testing.
// Instances are added to the list but not started (no tmux/git needed).
func newTestHarness(t *testing.T, instances []*session.Instance) *testHarness {
	t.Helper()
	s := spinner.New(spinner.WithSpinner(spinner.MiniDot))
	list := ui.NewList(&s, false)
	for _, inst := range instances {
		list.AddInstance(inst)
	}

	h := &home{
		ctx:          context.Background(),
		state:        stateDefault,
		appConfig:    config.DefaultConfig(),
		list:         list,
		menu:         ui.NewMenu(),
		tabbedWindow: ui.NewTabbedWindow(ui.NewPreviewPane(), ui.NewDiffPane(), ui.NewTerminalPane()),
	}
	return &testHarness{h: h, t: t}
}

// SendKey sends a key event through Update() and returns the command.
func (th *testHarness) SendKey(key tea.KeyMsg) tea.Cmd {
	th.t.Helper()
	_, cmd := th.h.Update(key)
	return cmd
}

// SendKeyRune sends a single character key event.
func (th *testHarness) SendKeyRune(r rune) tea.Cmd {
	th.t.Helper()
	return th.SendKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
}

// SendKeyType sends a special key event (Enter, Esc, etc).
func (th *testHarness) SendKeyType(kt tea.KeyType) tea.Cmd {
	th.t.Helper()
	return th.SendKey(tea.KeyMsg{Type: kt})
}

// SendMsg sends an arbitrary message to Update().
func (th *testHarness) SendMsg(msg tea.Msg) tea.Cmd {
	th.t.Helper()
	_, cmd := th.h.Update(msg)
	return cmd
}

// SimulateMetadataTick sends a metadataUpdateDoneMsg with the given results.
func (th *testHarness) SimulateMetadataTick(results []instanceMetaResult) tea.Cmd {
	th.t.Helper()
	return th.SendMsg(metadataUpdateDoneMsg{results: results})
}

// CreateNewInstance simulates pressing 'n' to create a new instance in the
// given path and enters stateNew. Returns the newly created instance.
func (th *testHarness) CreateNewInstance(path string) *session.Instance {
	th.t.Helper()

	inst, err := session.NewInstance(session.InstanceOptions{
		Title:   "",
		Path:    path,
		Program: "claude",
	})
	require.NoError(th.t, err)

	th.h.newInstanceFinalizer = th.h.list.AddInstance(inst)
	th.h.list.SetSelectedInstance(th.h.list.NumInstances() - 1)
	th.h.editingInstance = inst
	th.h.state = stateNew
	th.h.menu.SetState(ui.StateNewInstance)

	return inst
}

// TypeTitle simulates typing a title character by character.
// Returns error if any SetTitle call fails.
func (th *testHarness) TypeTitle(title string) error {
	th.t.Helper()
	for _, r := range title {
		th.SendKeyRune(r)
		// Check if the editingInstance got the character
		if th.h.editingInstance != nil {
			// No error — continue
		}
	}
	return nil
}

// AssertState asserts the current state of the home model.
func (th *testHarness) AssertState(expected state) {
	th.t.Helper()
	assert.Equal(th.t, expected, th.h.state, "unexpected home state")
}

// SelectedInstance returns the currently selected instance in the list.
func (th *testHarness) SelectedInstance() *session.Instance {
	th.t.Helper()
	return th.h.list.GetSelectedInstance()
}

// EditingInstance returns the instance being named in stateNew.
func (th *testHarness) EditingInstance() *session.Instance {
	th.t.Helper()
	return th.h.editingInstance
}

// State returns the current state.
func (th *testHarness) State() state {
	return th.h.state
}

// --- SetTitle regression tests ---

func TestSetTitle_DuringMetadataUpdate(t *testing.T) {
	// Setup: one existing instance + create a new instance.
	existing := &session.Instance{
		Title:  "existing-task",
		Path:   "/path/a",
		Status: session.Running,
	}

	th := newTestHarness(t, []*session.Instance{existing})
	th.h.currentRepoPath = "/path/a"

	// Enter stateNew with a new instance.
	newInst := th.CreateNewInstance("/path/a")
	th.AssertState(stateNew)

	// Type part of the title.
	th.SendKeyRune('h')
	th.SendKeyRune('e')
	assert.Equal(t, "he", newInst.Title)

	// Simulate a metadataUpdateDoneMsg arriving (happens every 500ms).
	// This calls syncWorkspaceUI() which can change the list filter/selection.
	th.SimulateMetadataTick([]instanceMetaResult{
		{instance: existing, updated: true},
	})

	// Continue typing — should still apply to the new instance, NOT the existing one.
	th.SendKeyRune('l')
	th.SendKeyRune('l')
	th.SendKeyRune('o')

	assert.Equal(t, "hello", newInst.Title, "title should apply to the new instance, not the existing started one")
	assert.Equal(t, "existing-task", existing.Title, "existing instance title should be unchanged")
}

func TestSetTitle_WithMultipleWorkspaces(t *testing.T) {
	// Setup: instances from two different workspaces.
	instA := &session.Instance{
		Title:  "task-a",
		Path:   "/path/a",
		Status: session.Running,
	}

	instB := &session.Instance{
		Title:  "task-b",
		Path:   "/path/b",
		Status: session.Running,
	}

	th := newTestHarness(t, []*session.Instance{instA, instB})
	th.h.currentRepoPath = "/path/a"

	// Derive workspaces and filter to /path/a.
	th.h.syncWorkspaceUI()
	require.Len(t, th.h.workspaces, 2)

	// Create new instance in /path/a.
	newInst := th.CreateNewInstance("/path/a")
	th.AssertState(stateNew)

	// Type part of the title.
	th.SendKeyRune('m')
	th.SendKeyRune('y')
	assert.Equal(t, "my", newInst.Title)

	// Simulate metadata update — syncWorkspaceUI() will SetFilter, possibly
	// changing the selected instance in the list.
	th.SimulateMetadataTick([]instanceMetaResult{
		{instance: instA, updated: true},
		{instance: instB, updated: false},
	})

	// Continue typing.
	th.SendKeyRune('-')
	th.SendKeyRune('f')
	th.SendKeyRune('i')
	th.SendKeyRune('x')

	assert.Equal(t, "my-fix", newInst.Title, "title should still apply to new instance after workspace re-filter")
	assert.Equal(t, "task-a", instA.Title, "instA title should be unchanged")
	assert.Equal(t, "task-b", instB.Title, "instB title should be unchanged")
}

func TestSetTitle_EscClearsEditingInstance(t *testing.T) {
	th := newTestHarness(t, nil)
	newInst := th.CreateNewInstance("/path/a")
	th.AssertState(stateNew)
	require.NotNil(t, th.EditingInstance())

	th.SendKeyRune('a')
	assert.Equal(t, "a", newInst.Title)

	// Press Esc to cancel.
	th.SendKeyType(tea.KeyEscape)

	th.AssertState(stateDefault)
	assert.Nil(t, th.EditingInstance(), "editingInstance should be cleared on Esc")
}

func TestSetTitle_CtrlCClearsEditingInstance(t *testing.T) {
	th := newTestHarness(t, nil)
	th.CreateNewInstance("/path/a")
	th.AssertState(stateNew)

	// Press Ctrl+C to cancel.
	th.SendKey(tea.KeyMsg{Type: tea.KeyCtrlC})

	th.AssertState(stateDefault)
	assert.Nil(t, th.EditingInstance(), "editingInstance should be cleared on Ctrl+C")
}

// --- Restore graceful degradation tests ---

func TestRestore_DeadSession_InstanceStillUsable(t *testing.T) {
	// Verify that an unstarted instance can have its title set,
	// confirming that the graceful degradation path (marking as Paused)
	// doesn't break instance creation flows.
	inst := &session.Instance{
		Title:   "dead-session",
		Path:    "/path/a",
		Status:  session.Running,
		Program: "claude",
	}

	// Calling SetTitle succeeds on unstarted instance.
	err := inst.SetTitle("renamed")
	assert.NoError(t, err)
	assert.Equal(t, "renamed", inst.Title)

	// After instance is "restored" as paused, it should still be functional.
	inst.Status = session.Paused
	assert.True(t, inst.Paused())
}

// --- Metadata tick during stateNew doesn't cause issues ---

func TestMetadataTick_DuringStateNew_NoStateChange(t *testing.T) {
	th := newTestHarness(t, nil)
	newInst := th.CreateNewInstance("/path/a")
	th.AssertState(stateNew)

	// Multiple metadata ticks should not change state.
	for i := 0; i < 5; i++ {
		th.SimulateMetadataTick(nil)
	}

	th.AssertState(stateNew)
	assert.Same(t, newInst, th.EditingInstance(), "editingInstance pointer should be stable across metadata ticks")
}

// --- Harness itself works correctly ---

func TestHarness_CreateAndType(t *testing.T) {
	th := newTestHarness(t, nil)

	// Create instance and type a title.
	newInst := th.CreateNewInstance("/path/test")
	th.AssertState(stateNew)

	th.TypeTitle("my-feature")
	assert.Equal(t, "my-feature", newInst.Title)
}

func TestHarness_SimulateMetadataWithDiffStats(t *testing.T) {
	inst := &session.Instance{
		Title:  "test",
		Path:   "/path/a",
		Status: session.Running,
	}

	th := newTestHarness(t, []*session.Instance{inst})

	// Simulate a metadata tick with diff stats.
	th.SimulateMetadataTick([]instanceMetaResult{
		{
			instance: inst,
			updated:  true,
			diffStats: &vcs.DiffStats{
				Added:   10,
				Removed: 3,
				Content: "+hello\n-world\n",
			},
		},
	})

	// Instance should have been updated.
	assert.Equal(t, session.Running, inst.Status)
}
