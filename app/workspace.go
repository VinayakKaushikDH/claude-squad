package app

import (
	"claude-squad/session"
	"path/filepath"
	"sort"
)

// Workspace represents a group of instances from the same repository.
type Workspace struct {
	Name     string // Display name (basename, or parent/basename if disambiguated)
	Path     string // Absolute repo path (grouping key)
	HasReady bool   // True if any instance in this workspace has Status == session.Ready
}

// DeriveWorkspaces groups instances by repo path and returns sorted workspaces.
// When two workspaces share the same basename, the parent directory name is prepended
// to disambiguate (e.g., "work/api" and "personal/api").
func DeriveWorkspaces(instances []*session.Instance) []Workspace {
	if len(instances) == 0 {
		return []Workspace{}
	}

	type entry struct {
		path     string
		hasReady bool
	}
	seen := make(map[string]*entry)
	var paths []string // preserves insertion order before sort

	for _, inst := range instances {
		if inst == nil {
			continue
		}
		p := inst.Path
		if _, ok := seen[p]; !ok {
			seen[p] = &entry{path: p}
			paths = append(paths, p)
		}
		if inst.Status == session.Ready && !inst.ReadyAcknowledged {
			seen[p].hasReady = true
		}
	}

	if len(paths) == 0 {
		return []Workspace{}
	}

	// Detect basename collisions for disambiguation.
	baseCount := make(map[string]int)
	for _, p := range paths {
		base := filepath.Base(p)
		baseCount[base]++
	}

	workspaces := make([]Workspace, 0, len(paths))
	for _, p := range paths {
		base := filepath.Base(p)
		name := base
		if baseCount[base] > 1 {
			parent := filepath.Base(filepath.Dir(p))
			name = parent + "/" + base
		}
		workspaces = append(workspaces, Workspace{
			Name:     name,
			Path:     p,
			HasReady: seen[p].hasReady,
		})
	}

	sort.Slice(workspaces, func(i, j int) bool {
		return workspaces[i].Name < workspaces[j].Name
	})

	return workspaces
}

// FindWorkspaceIndex returns the index of the workspace matching path.
// Returns 0 if not found.
func FindWorkspaceIndex(workspaces []Workspace, path string) int {
	for i, ws := range workspaces {
		if ws.Path == path {
			return i
		}
	}
	return 0
}
