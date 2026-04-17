package jj

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// setupTestJJRepo creates a temporary jj repo with an initial commit for testing.
// Returns the repo path and a cleanup function.
func setupTestJJRepo(t *testing.T) (string, func()) {
	t.Helper()
	if !jjAvailable() {
		t.Skip("jj not installed")
	}

	repoDir, err := os.MkdirTemp("", "jj-workspace-test-*")
	if err != nil {
		t.Fatal(err)
	}
	cleanup := func() { os.RemoveAll(repoDir) }

	// Init jj repo with git backend
	cmd := exec.Command("jj", "git", "init")
	cmd.Dir = repoDir
	if out, err := cmd.CombinedOutput(); err != nil {
		cleanup()
		t.Fatalf("failed to init jj repo: %s (%v)", out, err)
	}

	// Create an initial file and commit
	if err := os.WriteFile(filepath.Join(repoDir, "README.md"), []byte("# Test\n"), 0644); err != nil {
		cleanup()
		t.Fatal(err)
	}

	cmd = exec.Command("jj", "describe", "-m", "initial commit")
	cmd.Dir = repoDir
	if out, err := cmd.CombinedOutput(); err != nil {
		cleanup()
		t.Fatalf("failed to describe: %s (%v)", out, err)
	}

	cmd = exec.Command("jj", "new")
	cmd.Dir = repoDir
	if out, err := cmd.CombinedOutput(); err != nil {
		cleanup()
		t.Fatalf("failed to jj new: %s (%v)", out, err)
	}

	return repoDir, cleanup
}

func TestNewJJWorkspace(t *testing.T) {
	if !jjAvailable() {
		t.Skip("jj not installed")
	}

	repoDir, cleanup := setupTestJJRepo(t)
	defer cleanup()

	ws, bookmarkName, err := NewJJWorkspace(repoDir, "test-session")
	if err != nil {
		t.Fatalf("NewJJWorkspace failed: %v", err)
	}

	if bookmarkName == "" {
		t.Error("bookmark name should not be empty")
	}
	if ws.GetBranchName() != bookmarkName {
		t.Errorf("GetBranchName() = %q, want %q", ws.GetBranchName(), bookmarkName)
	}
	if ws.GetWorktreePath() == "" {
		t.Error("workspace path should not be empty")
	}
	if ws.IsExistingBranch() {
		t.Error("should not be an existing branch")
	}

	// Resolve symlinks for comparison (macOS /var -> /private/var)
	resolvedRepo, _ := filepath.EvalSymlinks(repoDir)
	resolvedWsRepo, _ := filepath.EvalSymlinks(ws.GetRepoPath())
	if resolvedWsRepo != resolvedRepo {
		t.Errorf("GetRepoPath() = %q, want %q", ws.GetRepoPath(), repoDir)
	}
}

func TestNewJJWorkspaceFromStorage(t *testing.T) {
	ws := NewJJWorkspaceFromStorage(
		"/repo", "/workspace", "session", "ws-name", "bookmark", "abc123", true,
	)

	if ws.GetRepoPath() != "/repo" {
		t.Errorf("GetRepoPath() = %q", ws.GetRepoPath())
	}
	if ws.GetWorktreePath() != "/workspace" {
		t.Errorf("GetWorktreePath() = %q", ws.GetWorktreePath())
	}
	if ws.GetBranchName() != "bookmark" {
		t.Errorf("GetBranchName() = %q", ws.GetBranchName())
	}
	if ws.GetBaseCommitSHA() != "abc123" {
		t.Errorf("GetBaseCommitSHA() = %q", ws.GetBaseCommitSHA())
	}
	if !ws.IsExistingBranch() {
		t.Error("should be existing branch")
	}
	if ws.GetRepoName() != "repo" {
		t.Errorf("GetRepoName() = %q", ws.GetRepoName())
	}
}

func TestWorkspaceSetupAndCleanup(t *testing.T) {
	if !jjAvailable() {
		t.Skip("jj not installed")
	}

	repoDir, cleanup := setupTestJJRepo(t)
	defer cleanup()

	ws, _, err := NewJJWorkspace(repoDir, "lifecycle-test")
	if err != nil {
		t.Fatalf("NewJJWorkspace failed: %v", err)
	}

	// Setup should create the workspace directory
	if err := ws.Setup(); err != nil {
		t.Fatalf("Setup failed: %v", err)
	}
	defer ws.Cleanup()

	// Workspace directory should exist with files
	if _, err := os.Stat(ws.GetWorktreePath()); os.IsNotExist(err) {
		t.Fatal("workspace directory should exist after Setup")
	}

	// README.md from the repo should be materialized
	readmePath := filepath.Join(ws.GetWorktreePath(), "README.md")
	if _, err := os.Stat(readmePath); os.IsNotExist(err) {
		t.Fatal("README.md should be materialized in workspace")
	}

	// Base change ID should be captured
	if ws.GetBaseCommitSHA() == "" {
		t.Error("base change ID should be set after Setup")
	}

	// Cleanup should remove the directory
	if err := ws.Cleanup(); err != nil {
		t.Fatalf("Cleanup failed: %v", err)
	}

	if _, err := os.Stat(ws.GetWorktreePath()); !os.IsNotExist(err) {
		t.Error("workspace directory should not exist after Cleanup")
	}
}

func TestWorkspaceRemoveKeepsBookmark(t *testing.T) {
	if !jjAvailable() {
		t.Skip("jj not installed")
	}

	repoDir, cleanup := setupTestJJRepo(t)
	defer cleanup()

	ws, _, err := NewJJWorkspace(repoDir, "pause-test")
	if err != nil {
		t.Fatalf("NewJJWorkspace failed: %v", err)
	}

	if err := ws.Setup(); err != nil {
		t.Fatalf("Setup failed: %v", err)
	}

	// Create a file and commit so the bookmark exists
	testFile := filepath.Join(ws.GetWorktreePath(), "test.txt")
	if err := os.WriteFile(testFile, []byte("hello\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := ws.CommitChanges("test commit"); err != nil {
		t.Fatalf("CommitChanges failed: %v", err)
	}

	// Remove (pause) — should keep bookmark
	if err := ws.Remove(); err != nil {
		t.Fatalf("Remove failed: %v", err)
	}

	// Directory should be gone
	if _, err := os.Stat(ws.GetWorktreePath()); !os.IsNotExist(err) {
		t.Error("workspace directory should not exist after Remove")
	}

	// Bookmark should still exist
	output, err := runJJCommand(repoDir, "bookmark", "list", ws.GetBranchName(), "--ignore-working-copy")
	if err != nil {
		t.Fatalf("failed to list bookmarks: %v", err)
	}
	if !bookmarkExists(output, ws.GetBranchName()) {
		t.Errorf("bookmark %q should still exist after Remove, got: %s", ws.GetBranchName(), output)
	}
}

func TestIsDirtyAndCommit(t *testing.T) {
	if !jjAvailable() {
		t.Skip("jj not installed")
	}

	repoDir, cleanup := setupTestJJRepo(t)
	defer cleanup()

	ws, _, err := NewJJWorkspace(repoDir, "dirty-test")
	if err != nil {
		t.Fatalf("NewJJWorkspace failed: %v", err)
	}

	if err := ws.Setup(); err != nil {
		t.Fatalf("Setup failed: %v", err)
	}
	defer ws.Cleanup()

	// Fresh workspace should not be dirty
	dirty, err := ws.IsDirty()
	if err != nil {
		t.Fatalf("IsDirty failed: %v", err)
	}
	if dirty {
		t.Error("fresh workspace should not be dirty")
	}

	// Modify a file
	testFile := filepath.Join(ws.GetWorktreePath(), "new-file.txt")
	if err := os.WriteFile(testFile, []byte("new content\n"), 0644); err != nil {
		t.Fatal(err)
	}

	// Should be dirty now
	dirty, err = ws.IsDirty()
	if err != nil {
		t.Fatalf("IsDirty failed: %v", err)
	}
	if !dirty {
		t.Error("workspace should be dirty after file modification")
	}

	// Commit should clear dirty state
	if err := ws.CommitChanges("test commit"); err != nil {
		t.Fatalf("CommitChanges failed: %v", err)
	}

	dirty, err = ws.IsDirty()
	if err != nil {
		t.Fatalf("IsDirty failed: %v", err)
	}
	if dirty {
		t.Error("workspace should not be dirty after CommitChanges")
	}
}

func TestCanResumeAndCanRemove(t *testing.T) {
	ws := &JJWorkspace{}
	if err := ws.CanResume(); err != nil {
		t.Errorf("CanResume should always return nil, got: %v", err)
	}
	if err := ws.CanRemove(); err != nil {
		t.Errorf("CanRemove should always return nil, got: %v", err)
	}
}

func TestDiff(t *testing.T) {
	if !jjAvailable() {
		t.Skip("jj not installed")
	}

	repoDir, cleanup := setupTestJJRepo(t)
	defer cleanup()

	ws, _, err := NewJJWorkspace(repoDir, "diff-test")
	if err != nil {
		t.Fatalf("NewJJWorkspace failed: %v", err)
	}

	if err := ws.Setup(); err != nil {
		t.Fatalf("Setup failed: %v", err)
	}
	defer ws.Cleanup()

	// Diff on clean workspace should be empty
	stats := ws.Diff()
	if stats.Error != nil {
		t.Fatalf("Diff on clean workspace failed: %v", stats.Error)
	}
	if !stats.IsEmpty() {
		t.Errorf("Diff on clean workspace should be empty, got Added=%d Removed=%d", stats.Added, stats.Removed)
	}

	// Add a new file
	newFile := filepath.Join(ws.GetWorktreePath(), "added.txt")
	if err := os.WriteFile(newFile, []byte("line1\nline2\nline3\n"), 0644); err != nil {
		t.Fatal(err)
	}

	// Diff should show additions
	stats = ws.Diff()
	if stats.Error != nil {
		t.Fatalf("Diff after add failed: %v", stats.Error)
	}
	if stats.Added == 0 {
		t.Error("Diff should show added lines after creating a file")
	}
	if stats.Content == "" {
		t.Error("Diff content should not be empty")
	}

	// Modify existing file
	readmePath := filepath.Join(ws.GetWorktreePath(), "README.md")
	if err := os.WriteFile(readmePath, []byte("# Modified\nNew line\n"), 0644); err != nil {
		t.Fatal(err)
	}

	stats = ws.Diff()
	if stats.Error != nil {
		t.Fatalf("Diff after modify failed: %v", stats.Error)
	}
	if stats.Added == 0 || stats.Removed == 0 {
		t.Errorf("Diff should show both added and removed lines, got Added=%d Removed=%d", stats.Added, stats.Removed)
	}
}

func TestDiffNoBaseChangeID(t *testing.T) {
	ws := &JJWorkspace{}
	stats := ws.Diff()
	if stats.Error == nil {
		t.Error("Diff without base change ID should return error")
	}
}
