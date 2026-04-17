package jj

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func jjAvailable() bool {
	_, err := exec.LookPath("jj")
	return err == nil
}

func TestSanitizeBookmarkName(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"My Session Name", "my-session-name"},
		{"feature/branch..name", "feature/branch.name"},
		{"---leading---", "leading"},
		{"UPPER_CASE_123", "upper_case_123"},
		{"a/b/c", "a/b/c"},
		{"hello world!", "hello-world"},
		{"..dotdot..", "dotdot"},
		{".leading.dot", "leading.dot"},
		{"trailing.dot.", "trailing.dot"},
		{"multiple---dashes", "multiple-dashes"},
		{"back\\slash", "backslash"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := sanitizeBookmarkName(tt.input)
			if got != tt.expected {
				t.Errorf("sanitizeBookmarkName(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}

func TestIsJJRepo(t *testing.T) {
	if !jjAvailable() {
		t.Skip("jj not installed")
	}

	// Non-repo directory
	tmpDir, err := os.MkdirTemp("", "jj-test-nonrepo-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	if IsJJRepo(tmpDir) {
		t.Error("IsJJRepo should return false for non-repo directory")
	}

	// Real jj repo
	repoDir, err := os.MkdirTemp("", "jj-test-repo-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(repoDir)

	cmd := exec.Command("jj", "git", "init")
	cmd.Dir = repoDir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("failed to init jj repo: %s (%v)", out, err)
	}

	if !IsJJRepo(repoDir) {
		t.Error("IsJJRepo should return true for jj repository")
	}
}

func TestFindJJRepoRoot(t *testing.T) {
	if !jjAvailable() {
		t.Skip("jj not installed")
	}

	repoDir, err := os.MkdirTemp("", "jj-test-root-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(repoDir)

	cmd := exec.Command("jj", "git", "init")
	cmd.Dir = repoDir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("failed to init jj repo: %s (%v)", out, err)
	}

	root, err := findJJRepoRoot(repoDir)
	if err != nil {
		t.Fatalf("findJJRepoRoot failed: %v", err)
	}
	// Resolve symlinks (macOS /var -> /private/var) for comparison
	resolvedRepo, _ := filepath.EvalSymlinks(repoDir)
	resolvedRoot, _ := filepath.EvalSymlinks(root)
	if resolvedRoot != resolvedRepo {
		t.Errorf("findJJRepoRoot = %q, want %q", root, repoDir)
	}
}

func TestRunJJCommand(t *testing.T) {
	if !jjAvailable() {
		t.Skip("jj not installed")
	}

	repoDir, err := os.MkdirTemp("", "jj-test-cmd-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(repoDir)

	cmd := exec.Command("jj", "git", "init")
	cmd.Dir = repoDir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("failed to init jj repo: %s (%v)", out, err)
	}

	output, err := runJJCommand(repoDir, "status")
	if err != nil {
		t.Fatalf("runJJCommand failed: %v", err)
	}
	// jj status on a fresh repo should succeed (may have output about working copy)
	_ = output
}

func TestRetryBackoff(t *testing.T) {
	d0 := retryBackoff(0)
	d1 := retryBackoff(1)
	d2 := retryBackoff(2)

	if d0 != 100_000_000 { // 100ms
		t.Errorf("retryBackoff(0) = %v, want 100ms", d0)
	}
	if d1 != 200_000_000 { // 200ms
		t.Errorf("retryBackoff(1) = %v, want 200ms", d1)
	}
	if d2 != 400_000_000 { // 400ms
		t.Errorf("retryBackoff(2) = %v, want 400ms", d2)
	}
}
