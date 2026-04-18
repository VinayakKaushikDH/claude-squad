package config

import (
	"claude-squad/log"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

const (
	StateFileName = "state.json"
	LockFileName  = "state.lock"
)

// InstanceStorage handles instance-related operations
type InstanceStorage interface {
	// SaveInstances saves the raw instance data
	SaveInstances(instancesJSON json.RawMessage) error
	// GetInstances returns the raw instance data
	GetInstances() json.RawMessage
	// ReloadInstances re-reads instance data from disk, bypassing cache
	ReloadInstances() (json.RawMessage, error)
	// DeleteAllInstances removes all stored instances
	DeleteAllInstances() error
}

// AppState handles application-level state
type AppState interface {
	// GetHelpScreensSeen returns the bitmask of seen help screens
	GetHelpScreensSeen() uint32
	// SetHelpScreensSeen updates the bitmask of seen help screens
	SetHelpScreensSeen(seen uint32) error
}

// StateManager combines instance storage and app state management
type StateManager interface {
	InstanceStorage
	AppState
}

// State represents the application state that persists between sessions
type State struct {
	// HelpScreensSeen is a bitmask tracking which help screens have been shown
	HelpScreensSeen uint32 `json:"help_screens_seen"`
	// Instances stores the serialized instance data as raw JSON
	InstancesData json.RawMessage `json:"instances"`
}

// DefaultState returns the default state
func DefaultState() *State {
	return &State{
		HelpScreensSeen: 0,
		InstancesData:   json.RawMessage("[]"),
	}
}

// withStateLock acquires an exclusive advisory lock on the state lock file
// and runs fn while holding it. The lock is released when fn returns.
func withStateLock(fn func() error) error {
	configDir, err := GetConfigDir()
	if err != nil {
		return fmt.Errorf("failed to get config directory: %w", err)
	}

	if err := os.MkdirAll(configDir, 0755); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	lockPath := filepath.Join(configDir, LockFileName)
	lockFile, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0644)
	if err != nil {
		return fmt.Errorf("failed to open lock file: %w", err)
	}
	defer lockFile.Close()

	if err := syscall.Flock(int(lockFile.Fd()), syscall.LOCK_EX); err != nil {
		return fmt.Errorf("failed to acquire lock: %w", err)
	}
	defer syscall.Flock(int(lockFile.Fd()), syscall.LOCK_UN)

	return fn()
}

// LoadState loads the state from disk. If it cannot be done, we return the default state.
func LoadState() *State {
	var state *State
	err := withStateLock(func() error {
		var loadErr error
		state, loadErr = loadStateUnlocked()
		if loadErr != nil {
			return loadErr
		}
		// If state was nil (new file), save default state inline to avoid
		// re-entering the lock via SaveState.
		if state == nil {
			state = DefaultState()
			return saveStateUnlocked(state)
		}
		return nil
	})
	if err != nil {
		log.ErrorLog.Printf("failed to load state: %v", err)
		return DefaultState()
	}
	return state
}

// loadStateUnlocked reads state from disk without acquiring the lock.
// Returns nil state (not error) if the file doesn't exist yet.
func loadStateUnlocked() (*State, error) {
	configDir, err := GetConfigDir()
	if err != nil {
		return nil, fmt.Errorf("failed to get config directory: %w", err)
	}

	statePath := filepath.Join(configDir, StateFileName)
	data, err := os.ReadFile(statePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to read state file: %w", err)
	}

	var state State
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("failed to parse state file: %w", err)
	}

	return &state, nil
}

// SaveState saves the state to disk using atomic write (temp file + rename).
func SaveState(state *State) error {
	return withStateLock(func() error {
		return saveStateUnlocked(state)
	})
}

// saveStateUnlocked writes state to disk without acquiring the lock.
// Uses atomic write: write to temp file then rename.
func saveStateUnlocked(state *State) error {
	configDir, err := GetConfigDir()
	if err != nil {
		return fmt.Errorf("failed to get config directory: %w", err)
	}

	if err := os.MkdirAll(configDir, 0755); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	statePath := filepath.Join(configDir, StateFileName)
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal state: %w", err)
	}

	// Atomic write: write to temp file, then rename.
	tmpFile, err := os.CreateTemp(configDir, "state-*.tmp")
	if err != nil {
		return fmt.Errorf("failed to create temp file: %w", err)
	}
	tmpPath := tmpFile.Name()

	if _, err := tmpFile.Write(data); err != nil {
		tmpFile.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("failed to write temp file: %w", err)
	}
	if err := tmpFile.Close(); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("failed to close temp file: %w", err)
	}

	if err := os.Rename(tmpPath, statePath); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("failed to rename temp file: %w", err)
	}

	return nil
}

// InstanceStorage interface implementation

// SaveInstances saves the raw instance data
func (s *State) SaveInstances(instancesJSON json.RawMessage) error {
	s.InstancesData = instancesJSON
	return SaveState(s)
}

// GetInstances returns the cached raw instance data
func (s *State) GetInstances() json.RawMessage {
	return s.InstancesData
}

// ReloadInstances re-reads instance data from disk, bypassing the in-memory cache.
func (s *State) ReloadInstances() (json.RawMessage, error) {
	var result json.RawMessage
	err := withStateLock(func() error {
		state, loadErr := loadStateUnlocked()
		if loadErr != nil {
			return loadErr
		}
		if state == nil {
			result = json.RawMessage("[]")
			return nil
		}
		result = state.InstancesData
		// Update our cache too
		s.InstancesData = result
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

// DeleteAllInstances removes all stored instances
func (s *State) DeleteAllInstances() error {
	s.InstancesData = json.RawMessage("[]")
	return SaveState(s)
}

// AppState interface implementation

// GetHelpScreensSeen returns the bitmask of seen help screens
func (s *State) GetHelpScreensSeen() uint32 {
	return s.HelpScreensSeen
}

// SetHelpScreensSeen updates the bitmask of seen help screens
func (s *State) SetHelpScreensSeen(seen uint32) error {
	s.HelpScreensSeen = seen
	return SaveState(s)
}
