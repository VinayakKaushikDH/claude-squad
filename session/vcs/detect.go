package vcs

import "os/exec"

// IsRepo checks if the given path is within a recognized VCS repository (jj or git).
// Checks jj first since colocated repos have both .jj/ and .git/.
//
// isJJRepo and isGitRepo are intentionally duplicated from their respective packages
// (session/jj and session/git) to avoid circular imports.
func IsRepo(path string) bool {
	return isJJRepo(path) || isGitRepo(path)
}

func isJJRepo(path string) bool {
	cmd := exec.Command("jj", "--repository", path, "root", "--ignore-working-copy")
	return cmd.Run() == nil
}

func isGitRepo(path string) bool {
	cmd := exec.Command("git", "-C", path, "rev-parse", "--show-toplevel")
	return cmd.Run() == nil
}
