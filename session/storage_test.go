package session

import (
	"encoding/json"
	"testing"

	"claude-squad/session/git"
)

func TestToInstanceData_VCSTypeGit(t *testing.T) {
	gw := git.NewGitWorktreeFromStorage("/repo", "/worktree", "my-session", "main", "abc123", false)
	i := &Instance{
		Title:     "my-session",
		workspace: gw,
	}
	data := i.ToInstanceData()
	if data.VCSType != "git" {
		t.Errorf("VCSType = %q, want %q", data.VCSType, "git")
	}
}

func TestToInstanceData_WorktreeIsValidJSON(t *testing.T) {
	gw := git.NewGitWorktreeFromStorage("/repo", "/worktree", "my-session", "feat/x", "abc123", true)
	i := &Instance{
		Title:     "my-session",
		workspace: gw,
	}
	data := i.ToInstanceData()

	if len(data.Worktree) == 0 {
		t.Fatal("Worktree should not be empty when workspace is set")
	}

	var wt GitWorktreeData
	if err := json.Unmarshal(data.Worktree, &wt); err != nil {
		t.Fatalf("Worktree is not valid JSON: %v", err)
	}
	if wt.RepoPath != "/repo" {
		t.Errorf("RepoPath = %q, want %q", wt.RepoPath, "/repo")
	}
	if wt.BranchName != "feat/x" {
		t.Errorf("BranchName = %q, want %q", wt.BranchName, "feat/x")
	}
	if wt.BaseCommitSHA != "abc123" {
		t.Errorf("BaseCommitSHA = %q, want %q", wt.BaseCommitSHA, "abc123")
	}
	if !wt.IsExistingBranch {
		t.Error("IsExistingBranch = false, want true")
	}
}

func TestToInstanceData_NoWorkspace_EmptyVCSType(t *testing.T) {
	i := &Instance{Title: "no-workspace"}
	data := i.ToInstanceData()
	if data.VCSType != "" {
		t.Errorf("VCSType = %q, want empty when workspace is nil", data.VCSType)
	}
}

func TestInstanceDataJSON_VCSTypeRoundtrip(t *testing.T) {
	original := InstanceData{
		Title:   "test",
		VCSType: "git",
		Worktree: json.RawMessage(`{"repo_path":"/r","worktree_path":"/w","session_name":"s","branch_name":"b","base_commit_sha":"","is_existing_branch":false}`),
	}

	jsonBytes, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("json.Marshal failed: %v", err)
	}

	var decoded InstanceData
	if err := json.Unmarshal(jsonBytes, &decoded); err != nil {
		t.Fatalf("json.Unmarshal failed: %v", err)
	}
	if decoded.VCSType != "git" {
		t.Errorf("decoded VCSType = %q, want %q", decoded.VCSType, "git")
	}
	if decoded.Title != "test" {
		t.Errorf("decoded Title = %q, want %q", decoded.Title, "test")
	}
}

func TestInstanceDataJSON_OmittedVCSType(t *testing.T) {
	// Simulate old JSON without vcs_type — decoded VCSType should be empty string
	// (backward compat: FromInstanceData defaults to "git" when empty)
	raw := `{"title":"old","path":"","branch":"","status":0,"height":0,"width":0,` +
		`"created_at":"0001-01-01T00:00:00Z","updated_at":"0001-01-01T00:00:00Z",` +
		`"auto_yes":false,"program":"","diff_stats":{"added":0,"removed":0,"content":""}}`

	var data InstanceData
	if err := json.Unmarshal([]byte(raw), &data); err != nil {
		t.Fatalf("json.Unmarshal failed: %v", err)
	}
	if data.VCSType != "" {
		t.Errorf("VCSType = %q, want empty for old JSON without vcs_type field", data.VCSType)
	}
}
