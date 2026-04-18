package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setupTestState creates a temporary config directory and returns a cleanup function.
func setupTestState(t *testing.T) func() {
	t.Helper()
	tmpDir := t.TempDir()
	origGetConfigDir := getConfigDir
	getConfigDir = func() (string, error) { return tmpDir, nil }
	return func() { getConfigDir = origGetConfigDir }
}

func TestFileLocking_ConcurrentWrites(t *testing.T) {
	cleanup := setupTestState(t)
	defer cleanup()

	// Two goroutines writing state.json simultaneously should not corrupt.
	var wg sync.WaitGroup
	errs := make([]error, 20)

	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			state := DefaultState()
			data := []byte(`[{"title":"inst-` + string(rune('A'+idx%26)) + `"}]`)
			state.InstancesData = json.RawMessage(data)
			errs[idx] = SaveState(state)
		}(i)
	}
	wg.Wait()

	for i, err := range errs {
		assert.NoError(t, err, "goroutine %d should not error", i)
	}

	// Final state should be valid JSON (not corrupted).
	loaded := LoadState()
	assert.NotNil(t, loaded)
	var instances []json.RawMessage
	err := json.Unmarshal(loaded.InstancesData, &instances)
	assert.NoError(t, err, "loaded instances should be valid JSON after concurrent writes")
}

func TestReloadInstances_FreshFromDisk(t *testing.T) {
	cleanup := setupTestState(t)
	defer cleanup()

	// Save initial state.
	state := DefaultState()
	state.InstancesData = json.RawMessage(`[{"title":"alpha"}]`)
	require.NoError(t, SaveState(state))

	// The in-memory cache still has "alpha".
	assert.Contains(t, string(state.GetInstances()), "alpha")

	// Simulate another process writing a different value directly to disk.
	configDir, _ := GetConfigDir()
	statePath := filepath.Join(configDir, StateFileName)
	newData := `{"help_screens_seen":0,"instances":[{"title":"beta"}]}`
	require.NoError(t, os.WriteFile(statePath, []byte(newData), 0644))

	// ReloadInstances should return the disk state, not the cache.
	reloaded, err := state.ReloadInstances()
	require.NoError(t, err)
	assert.Contains(t, string(reloaded), "beta", "should return fresh disk data")
	assert.NotContains(t, string(reloaded), "alpha", "should not return cached data")

	// The in-memory cache should also be updated.
	assert.Contains(t, string(state.GetInstances()), "beta")
}

func TestAtomicWrite_CrashSafe(t *testing.T) {
	cleanup := setupTestState(t)
	defer cleanup()

	// Write valid state.
	state := DefaultState()
	state.InstancesData = json.RawMessage(`[{"title":"original"}]`)
	require.NoError(t, SaveState(state))

	// Verify the file exists and is valid.
	configDir, _ := GetConfigDir()
	statePath := filepath.Join(configDir, StateFileName)
	data, err := os.ReadFile(statePath)
	require.NoError(t, err)

	var loaded State
	require.NoError(t, json.Unmarshal(data, &loaded))
	assert.Contains(t, string(loaded.InstancesData), "original")

	// No temp files should be left behind.
	entries, _ := os.ReadDir(configDir)
	for _, e := range entries {
		assert.NotContains(t, e.Name(), ".tmp", "no temp files should be left behind")
	}
}

func TestDeleteAllInstances(t *testing.T) {
	cleanup := setupTestState(t)
	defer cleanup()

	state := DefaultState()
	state.InstancesData = json.RawMessage(`[{"title":"to-delete"}]`)
	require.NoError(t, SaveState(state))

	require.NoError(t, state.DeleteAllInstances())
	assert.Equal(t, `[]`, string(state.GetInstances()))

	// Reload from disk confirms it's persisted.
	reloaded, err := state.ReloadInstances()
	require.NoError(t, err)
	assert.Equal(t, `[]`, string(reloaded))
}
