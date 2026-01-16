package executor

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/hochfrequenz/claude-plan-orchestrator/internal/domain"
)

// WorktreeManager handles git worktree operations
type WorktreeManager struct {
	repoDir     string
	worktreeDir string
}

// NewWorktreeManager creates a new WorktreeManager
func NewWorktreeManager(repoDir, worktreeDir string) *WorktreeManager {
	return &WorktreeManager{
		repoDir:     repoDir,
		worktreeDir: worktreeDir,
	}
}

// Create creates a new worktree for a task
// If an existing worktree or branch exists for this task, it will be cleaned up first
func (m *WorktreeManager) Create(taskID domain.TaskID) (string, error) {
	// Ensure worktree directory exists
	if err := os.MkdirAll(m.worktreeDir, 0755); err != nil {
		return "", fmt.Errorf("creating worktree dir: %w", err)
	}

	// Branch name
	branch := BranchName(taskID)

	// Check if branch already exists (either as worktree or just branch)
	// If so, clean it up first
	if err := m.cleanupExistingBranch(branch); err != nil {
		return "", fmt.Errorf("cleaning up existing branch: %w", err)
	}

	// Generate unique suffix
	suffix := randomSuffix()

	// Worktree path - include prefix if present (e.g., CLI02 vs E02)
	var epicPart string
	if taskID.Prefix != "" {
		epicPart = fmt.Sprintf("%s%02d", taskID.Prefix, taskID.EpicNum)
	} else {
		epicPart = fmt.Sprintf("E%02d", taskID.EpicNum)
	}
	dirName := fmt.Sprintf("%s-%s-%s", taskID.Module, epicPart, suffix)
	wtPath := filepath.Join(m.worktreeDir, dirName)

	// Fetch latest from origin first (if remote exists)
	fetchCmd := exec.Command("git", "fetch", "origin", "main")
	fetchCmd.Dir = m.repoDir
	fetchCmd.Run() // Ignore error - remote might not exist in tests

	// Try to create worktree from origin/main first, fall back to HEAD
	baseBranch := "origin/main"
	checkCmd := exec.Command("git", "rev-parse", "--verify", "origin/main")
	checkCmd.Dir = m.repoDir
	if checkCmd.Run() != nil {
		baseBranch = "HEAD" // Fall back if origin/main doesn't exist
	}

	// Create worktree with new branch
	cmd := exec.Command("git", "worktree", "add", "-b", branch, wtPath, baseBranch)
	cmd.Dir = m.repoDir
	if out, err := cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("git worktree add: %s: %w", out, err)
	}

	return wtPath, nil
}

// cleanupExistingBranch removes any existing worktree and branch for the given branch name
func (m *WorktreeManager) cleanupExistingBranch(branch string) error {
	// Prune any stale worktree entries first
	cmd := exec.Command("git", "worktree", "prune")
	cmd.Dir = m.repoDir
	cmd.Run()

	// Check if there's a worktree using this branch
	cmd = exec.Command("git", "worktree", "list", "--porcelain")
	cmd.Dir = m.repoDir
	out, _ := cmd.Output()

	// Parse worktree list to find one with our branch
	lines := strings.Split(string(out), "\n")
	for i, line := range lines {
		if strings.HasPrefix(line, "worktree ") {
			wtPath := strings.TrimPrefix(line, "worktree ")
			// Check if this worktree has our branch (branch line is 2 lines after worktree)
			for j := i + 1; j < len(lines) && j < i+4; j++ {
				if strings.TrimSpace(lines[j]) == "branch refs/heads/"+branch {
					// Found worktree with our branch - remove it
					rmCmd := exec.Command("git", "worktree", "remove", "--force", wtPath)
					rmCmd.Dir = m.repoDir
					rmCmd.Run() // Ignore error
					break
				}
			}
		}
	}

	// Always try to delete the branch (even if no worktree found)
	// This handles orphan branches from previous runs
	cmd = exec.Command("git", "branch", "-D", branch)
	cmd.Dir = m.repoDir
	cmd.Run() // Ignore error - branch might not exist

	return nil
}

// Remove removes a worktree
func (m *WorktreeManager) Remove(wtPath string) error {
	// Get branch name before removing
	cmd := exec.Command("git", "rev-parse", "--abbrev-ref", "HEAD")
	cmd.Dir = wtPath
	branchOut, _ := cmd.Output()
	branch := strings.TrimSpace(string(branchOut))

	// Remove worktree
	cmd = exec.Command("git", "worktree", "remove", "--force", wtPath)
	cmd.Dir = m.repoDir
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git worktree remove: %s: %w", out, err)
	}

	// Optionally delete the branch if not merged
	if branch != "" && branch != "HEAD" {
		cmd = exec.Command("git", "branch", "-D", branch)
		cmd.Dir = m.repoDir
		cmd.Run() // Ignore error if branch doesn't exist
	}

	return nil
}

// List returns all active worktree paths
func (m *WorktreeManager) List() ([]string, error) {
	cmd := exec.Command("git", "worktree", "list", "--porcelain")
	cmd.Dir = m.repoDir
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}

	var paths []string
	for _, line := range strings.Split(string(out), "\n") {
		if strings.HasPrefix(line, "worktree ") {
			path := strings.TrimPrefix(line, "worktree ")
			// Only include worktrees in our worktree directory
			if strings.HasPrefix(path, m.worktreeDir) {
				paths = append(paths, path)
			}
		}
	}

	return paths, nil
}

// BranchName returns the branch name for a task
func BranchName(taskID domain.TaskID) string {
	// Include prefix if present (e.g., feat/module-CLI02 vs feat/module-E02)
	if taskID.Prefix != "" {
		return fmt.Sprintf("feat/%s-%s%02d", taskID.Module, taskID.Prefix, taskID.EpicNum)
	}
	return fmt.Sprintf("feat/%s-E%02d", taskID.Module, taskID.EpicNum)
}

func randomSuffix() string {
	b := make([]byte, 3)
	rand.Read(b)
	return hex.EncodeToString(b)
}
