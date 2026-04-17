package app

import (
	"claude-squad/session"
	"testing"

	"github.com/stretchr/testify/assert"
)

func makeInstance(path string, status session.Status) *session.Instance {
	return &session.Instance{
		Title:  "test",
		Path:   path,
		Status: status,
	}
}

func TestDeriveWorkspaces(t *testing.T) {
	t.Run("nil input returns empty slice", func(t *testing.T) {
		result := DeriveWorkspaces(nil)
		assert.Equal(t, []Workspace{}, result)
	})

	t.Run("empty input returns empty slice", func(t *testing.T) {
		result := DeriveWorkspaces([]*session.Instance{})
		assert.Equal(t, []Workspace{}, result)
	})

	t.Run("single repo with multiple instances produces one workspace", func(t *testing.T) {
		instances := []*session.Instance{
			makeInstance("/home/user/projects/api", session.Running),
			makeInstance("/home/user/projects/api", session.Running),
		}
		result := DeriveWorkspaces(instances)
		assert.Len(t, result, 1)
		assert.Equal(t, "api", result[0].Name)
		assert.Equal(t, "/home/user/projects/api", result[0].Path)
	})

	t.Run("multiple repos produce multiple workspaces sorted by name", func(t *testing.T) {
		instances := []*session.Instance{
			makeInstance("/home/user/projects/zebra", session.Running),
			makeInstance("/home/user/projects/alpha", session.Running),
			makeInstance("/home/user/projects/mango", session.Running),
		}
		result := DeriveWorkspaces(instances)
		assert.Len(t, result, 3)
		assert.Equal(t, "alpha", result[0].Name)
		assert.Equal(t, "mango", result[1].Name)
		assert.Equal(t, "zebra", result[2].Name)
	})

	t.Run("basename collision disambiguates with parent directory", func(t *testing.T) {
		instances := []*session.Instance{
			makeInstance("/home/user/work/api", session.Running),
			makeInstance("/home/user/personal/api", session.Running),
		}
		result := DeriveWorkspaces(instances)
		assert.Len(t, result, 2)
		names := []string{result[0].Name, result[1].Name}
		assert.Contains(t, names, "work/api")
		assert.Contains(t, names, "personal/api")
	})

	t.Run("HasReady true when any instance is Ready", func(t *testing.T) {
		instances := []*session.Instance{
			makeInstance("/home/user/projects/api", session.Running),
			makeInstance("/home/user/projects/api", session.Ready),
			makeInstance("/home/user/projects/api", session.Running),
		}
		result := DeriveWorkspaces(instances)
		assert.Len(t, result, 1)
		assert.True(t, result[0].HasReady)
	})

	t.Run("HasReady false when no instance is Ready", func(t *testing.T) {
		instances := []*session.Instance{
			makeInstance("/home/user/projects/api", session.Running),
			makeInstance("/home/user/projects/api", session.Loading),
		}
		result := DeriveWorkspaces(instances)
		assert.Len(t, result, 1)
		assert.False(t, result[0].HasReady)
	})
}

func TestFindWorkspaceIndex(t *testing.T) {
	workspaces := []Workspace{
		{Name: "alpha", Path: "/home/user/projects/alpha"},
		{Name: "beta", Path: "/home/user/projects/beta"},
		{Name: "gamma", Path: "/home/user/projects/gamma"},
	}

	t.Run("returns correct index for known path", func(t *testing.T) {
		assert.Equal(t, 0, FindWorkspaceIndex(workspaces, "/home/user/projects/alpha"))
		assert.Equal(t, 1, FindWorkspaceIndex(workspaces, "/home/user/projects/beta"))
		assert.Equal(t, 2, FindWorkspaceIndex(workspaces, "/home/user/projects/gamma"))
	})

	t.Run("returns 0 for unknown path", func(t *testing.T) {
		assert.Equal(t, 0, FindWorkspaceIndex(workspaces, "/home/user/projects/unknown"))
	})

	t.Run("returns 0 for empty workspaces", func(t *testing.T) {
		assert.Equal(t, 0, FindWorkspaceIndex([]Workspace{}, "/some/path"))
	})
}
