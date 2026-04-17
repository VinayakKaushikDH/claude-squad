package jj

import (
	"claude-squad/config"
	"claude-squad/log"
	"claude-squad/session/vcs"
	"fmt"
	"path/filepath"
	"time"
)

var _ vcs.Workspace = (*JJWorkspace)(nil)

// JJWorkspace manages jj workspace operations for a session.
type JJWorkspace struct {
	// Path to the repository root
	repoPath string
	// Path to the workspace directory
	workspacePath string
	// Name of the session
	sessionName string
	// jj workspace name (derived from last path component)
	workspaceName string
	// jj bookmark name (maps to git branch on push)
	bookmarkName string
	// jj change ID at workspace creation time — used as diff baseline
	baseChangeID string
	// True if the bookmark existed before this session was created
	isExistingBookmark bool
}

// NewJJWorkspace creates a new JJWorkspace for a fresh session (new bookmark from current change).
func NewJJWorkspace(repoPath string, sessionName string) (workspace *JJWorkspace, bookmarkName string, err error) {
	cfg := config.LoadConfig()
	bookmarkName = fmt.Sprintf("%s%s", cfg.BranchPrefix, sessionName)
	bookmarkName = sanitizeBookmarkName(bookmarkName)

	repoPath, workspacePath, err := resolveWorkspacePaths(repoPath, bookmarkName)
	if err != nil {
		return nil, "", err
	}

	return &JJWorkspace{
		repoPath:      repoPath,
		workspacePath: workspacePath,
		sessionName:   sessionName,
		workspaceName: filepath.Base(workspacePath),
		bookmarkName:  bookmarkName,
	}, bookmarkName, nil
}

// NewJJWorkspaceFromBookmark creates a JJWorkspace that uses an existing bookmark.
// The bookmark will not be deleted on cleanup.
func NewJJWorkspaceFromBookmark(repoPath string, bookmarkName string, sessionName string) (*JJWorkspace, error) {
	repoPath, workspacePath, err := resolveWorkspacePaths(repoPath, bookmarkName)
	if err != nil {
		return nil, err
	}

	return &JJWorkspace{
		repoPath:           repoPath,
		workspacePath:      workspacePath,
		sessionName:        sessionName,
		workspaceName:      filepath.Base(workspacePath),
		bookmarkName:       bookmarkName,
		isExistingBookmark: true,
	}, nil
}

// NewJJWorkspaceFromStorage reconstructs a JJWorkspace from persisted data.
func NewJJWorkspaceFromStorage(repoPath, workspacePath, sessionName, workspaceName, bookmarkName, baseChangeID string, isExistingBookmark bool) *JJWorkspace {
	return &JJWorkspace{
		repoPath:           repoPath,
		workspacePath:      workspacePath,
		sessionName:        sessionName,
		workspaceName:      workspaceName,
		bookmarkName:       bookmarkName,
		baseChangeID:       baseChangeID,
		isExistingBookmark: isExistingBookmark,
	}
}

// resolveWorkspacePaths resolves the repo root and generates a unique workspace path.
func resolveWorkspacePaths(repoPath string, bookmarkName string) (resolvedRepo string, workspacePath string, err error) {
	absPath, err := filepath.Abs(repoPath)
	if err != nil {
		log.ErrorLog.Printf("jj workspace path abs error, falling back to repoPath %s: %s", repoPath, err)
		absPath = repoPath
	}

	resolvedRepo, err = findJJRepoRoot(absPath)
	if err != nil {
		return "", "", err
	}

	worktreeDir, err := getWorkspaceDirectory()
	if err != nil {
		return "", "", err
	}

	// bookmarkName is expected to be already sanitized by the caller
	workspacePath = filepath.Join(worktreeDir, bookmarkName)
	workspacePath = workspacePath + "_" + fmt.Sprintf("%x", time.Now().UnixNano())

	return resolvedRepo, workspacePath, nil
}

func getWorkspaceDirectory() (string, error) {
	configDir, err := config.GetConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(configDir, "worktrees"), nil
}

// Identity methods — satisfy vcs.Workspace interface

func (j *JJWorkspace) GetWorktreePath() string  { return j.workspacePath }
func (j *JJWorkspace) GetBranchName() string     { return j.bookmarkName }
func (j *JJWorkspace) GetRepoPath() string       { return j.repoPath }
func (j *JJWorkspace) GetRepoName() string       { return filepath.Base(j.repoPath) }
func (j *JJWorkspace) GetBaseCommitSHA() string   { return j.baseChangeID }
func (j *JJWorkspace) IsExistingBranch() bool     { return j.isExistingBookmark }
