package jj

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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

// getChangeID returns the change ID for a revision in the given repo path.
func getChangeID(t *testing.T, repoPath, revision string) string {
	t.Helper()
	output, err := runJJCommand(repoPath, "log", "-r", revision, "--no-graph", "-T", "change_id")
	if err != nil {
		t.Fatalf("failed to get change ID for %s: %v", revision, err)
	}
	return strings.TrimSpace(output)
}

// TestCheckoutInMainRepo_DirtyWorkspace verifies that checkout works even when
// the workspace has uncommitted changes and no prior bookmark exists.
// This catches bug #1 (bookmark not found) and bug #3 (checkout doesn't move main repo).
func TestCheckoutInMainRepo_DirtyWorkspace(t *testing.T) {
	if !jjAvailable() {
		t.Skip("jj not installed")
	}

	repoDir, cleanup := setupTestJJRepo(t)
	defer cleanup()

	ws, _, err := NewJJWorkspace(repoDir, "checkout-dirty-test")
	if err != nil {
		t.Fatalf("NewJJWorkspace failed: %v", err)
	}
	if err := ws.Setup(); err != nil {
		t.Fatalf("Setup failed: %v", err)
	}
	defer ws.Cleanup()

	// Create a file but do NOT commit — workspace is dirty, no bookmark exists yet
	testFile := filepath.Join(ws.GetWorktreePath(), "dirty-file.txt")
	if err := os.WriteFile(testFile, []byte("dirty content\n"), 0644); err != nil {
		t.Fatal(err)
	}

	// CheckoutInMainRepo should succeed (snapshot + create bookmark + jj edit)
	if err := ws.CheckoutInMainRepo(); err != nil {
		t.Fatalf("CheckoutInMainRepo() on dirty workspace = %v, want nil", err)
	}

	// Bug #1: bookmark must exist after checkout
	output, err := runJJCommand(repoDir, "bookmark", "list", ws.GetBranchName(), "--ignore-working-copy")
	if err != nil {
		t.Fatalf("failed to list bookmarks: %v", err)
	}
	if !bookmarkExists(output, ws.GetBranchName()) {
		t.Errorf("bookmark %q should exist after CheckoutInMainRepo", ws.GetBranchName())
	}

	// Bug #3: the dirty file should be visible in the main repo after checkout
	mainRepoFile := filepath.Join(repoDir, "dirty-file.txt")
	if _, err := os.Stat(mainRepoFile); os.IsNotExist(err) {
		t.Error("dirty-file.txt should be visible in main repo after CheckoutInMainRepo")
	}
}

// TestCheckoutInMainRepo_CleanWorkspaceWithPriorCommit verifies checkout works
// when there's a prior commit and the workspace is clean.
func TestCheckoutInMainRepo_CleanWorkspaceWithPriorCommit(t *testing.T) {
	if !jjAvailable() {
		t.Skip("jj not installed")
	}

	repoDir, cleanup := setupTestJJRepo(t)
	defer cleanup()

	ws, _, err := NewJJWorkspace(repoDir, "checkout-clean-test")
	if err != nil {
		t.Fatalf("NewJJWorkspace failed: %v", err)
	}
	if err := ws.Setup(); err != nil {
		t.Fatalf("Setup failed: %v", err)
	}
	defer ws.Cleanup()

	// Create and commit a file
	testFile := filepath.Join(ws.GetWorktreePath(), "committed-file.txt")
	if err := os.WriteFile(testFile, []byte("committed\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := ws.CommitChanges("test commit"); err != nil {
		t.Fatalf("CommitChanges failed: %v", err)
	}

	// Checkout should succeed
	if err := ws.CheckoutInMainRepo(); err != nil {
		t.Fatalf("CheckoutInMainRepo() = %v, want nil", err)
	}

	// The committed file should be visible in main repo
	mainRepoFile := filepath.Join(repoDir, "committed-file.txt")
	if _, err := os.Stat(mainRepoFile); os.IsNotExist(err) {
		t.Error("committed-file.txt should be visible in main repo after CheckoutInMainRepo")
	}
}

// TestCheckoutInMainRepo_AgentContinuesAfterCheckout verifies the workspace
// remains functional after checkout — the agent can keep working.
func TestCheckoutInMainRepo_AgentContinuesAfterCheckout(t *testing.T) {
	if !jjAvailable() {
		t.Skip("jj not installed")
	}

	repoDir, cleanup := setupTestJJRepo(t)
	defer cleanup()

	ws, _, err := NewJJWorkspace(repoDir, "checkout-continue-test")
	if err != nil {
		t.Fatalf("NewJJWorkspace failed: %v", err)
	}
	if err := ws.Setup(); err != nil {
		t.Fatalf("Setup failed: %v", err)
	}
	defer ws.Cleanup()

	// Make a change and checkout
	testFile := filepath.Join(ws.GetWorktreePath(), "before-checkout.txt")
	if err := os.WriteFile(testFile, []byte("before\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := ws.CheckoutInMainRepo(); err != nil {
		t.Fatalf("CheckoutInMainRepo() = %v", err)
	}

	// Agent should still be able to work: create another file
	newFile := filepath.Join(ws.GetWorktreePath(), "after-checkout.txt")
	if err := os.WriteFile(newFile, []byte("after\n"), 0644); err != nil {
		t.Fatal(err)
	}

	dirty, err := ws.IsDirty()
	if err != nil {
		t.Fatalf("IsDirty() after checkout failed: %v", err)
	}
	if !dirty {
		t.Error("workspace should be dirty after creating a new file post-checkout")
	}

	// Agent should be able to commit new work
	if err := ws.CommitChanges("post-checkout commit"); err != nil {
		t.Errorf("CommitChanges() after checkout failed: %v", err)
	}
}

// TestCheckoutInMainRepo_BookmarkPointsToCurrentChange verifies that after
// CheckoutInMainRepo, the bookmark points to @ (the current WC change),
// NOT @- (which is what CommitChanges does).
// This catches bug #4 (agent one commit ahead of user).
func TestCheckoutInMainRepo_BookmarkPointsToCurrentChange(t *testing.T) {
	if !jjAvailable() {
		t.Skip("jj not installed")
	}

	repoDir, cleanup := setupTestJJRepo(t)
	defer cleanup()

	ws, _, err := NewJJWorkspace(repoDir, "checkout-bookmark-test")
	if err != nil {
		t.Fatalf("NewJJWorkspace failed: %v", err)
	}
	if err := ws.Setup(); err != nil {
		t.Fatalf("Setup failed: %v", err)
	}
	defer ws.Cleanup()

	// Make dirty changes
	testFile := filepath.Join(ws.GetWorktreePath(), "bookmark-test.txt")
	if err := os.WriteFile(testFile, []byte("bookmark test\n"), 0644); err != nil {
		t.Fatal(err)
	}

	if err := ws.CheckoutInMainRepo(); err != nil {
		t.Fatalf("CheckoutInMainRepo() = %v", err)
	}

	// Bookmark should point to @ in workspace (not @-)
	wcChangeID := getChangeID(t, ws.GetWorktreePath(), "@")
	bookmarkChangeID := getChangeID(t, ws.GetWorktreePath(), ws.GetBranchName())

	if bookmarkChangeID != wcChangeID {
		t.Errorf("bookmark points to change %s, but workspace @ is %s — bookmark should be on @",
			bookmarkChangeID, wcChangeID)
	}
}

// TestCommitChanges_BookmarkPointsToParent verifies that after CommitChanges,
// the bookmark points to @- (the described change) and @ is a new empty WC.
// This contrasts with CheckoutInMainRepo which keeps the bookmark on @.
func TestCommitChanges_BookmarkPointsToParent(t *testing.T) {
	if !jjAvailable() {
		t.Skip("jj not installed")
	}

	repoDir, cleanup := setupTestJJRepo(t)
	defer cleanup()

	ws, _, err := NewJJWorkspace(repoDir, "commit-bookmark-test")
	if err != nil {
		t.Fatalf("NewJJWorkspace failed: %v", err)
	}
	if err := ws.Setup(); err != nil {
		t.Fatalf("Setup failed: %v", err)
	}
	defer ws.Cleanup()

	// Make changes and commit
	testFile := filepath.Join(ws.GetWorktreePath(), "commit-test.txt")
	if err := os.WriteFile(testFile, []byte("commit test\n"), 0644); err != nil {
		t.Fatal(err)
	}

	if err := ws.CommitChanges("test commit message"); err != nil {
		t.Fatalf("CommitChanges() = %v", err)
	}

	// Bookmark should point to @- (the described change, not the new empty WC)
	parentChangeID := getChangeID(t, ws.GetWorktreePath(), "@-")
	bookmarkChangeID := getChangeID(t, ws.GetWorktreePath(), ws.GetBranchName())

	if bookmarkChangeID != parentChangeID {
		t.Errorf("after CommitChanges, bookmark points to %s but @- is %s — bookmark should be on @-",
			bookmarkChangeID, parentChangeID)
	}

	// @ should be clean (new empty working copy)
	dirty, err := ws.IsDirty()
	if err != nil {
		t.Fatalf("IsDirty() = %v", err)
	}
	if dirty {
		t.Error("workspace should be clean after CommitChanges (new empty WC)")
	}
}

// TestBookmarkNameMatchesSessionName verifies that the bookmark name is exactly
// the sanitized session name — no prefix, no random suffix.
// This catches bug #5 (random bookmark names).
func TestBookmarkNameMatchesSessionName(t *testing.T) {
	if !jjAvailable() {
		t.Skip("jj not installed")
	}

	repoDir, cleanup := setupTestJJRepo(t)
	defer cleanup()

	sessionName := "My Test Session"
	ws, bookmarkName, err := NewJJWorkspace(repoDir, sessionName)
	if err != nil {
		t.Fatalf("NewJJWorkspace failed: %v", err)
	}

	expected := sanitizeBookmarkName(sessionName) // "my-test-session"
	if bookmarkName != expected {
		t.Errorf("bookmark name = %q, want %q (sanitized session name)", bookmarkName, expected)
	}
	if ws.GetBranchName() != expected {
		t.Errorf("GetBranchName() = %q, want %q", ws.GetBranchName(), expected)
	}

	// Verify no prefix or suffix was added
	if strings.Contains(bookmarkName, "/") {
		t.Errorf("bookmark name %q contains '/' — should not have a prefix", bookmarkName)
	}
	if len(bookmarkName) != len(expected) {
		t.Errorf("bookmark name length %d != expected %d — may have a random suffix",
			len(bookmarkName), len(expected))
	}
}
