package prbot

import (
	"fmt"
	"os/exec"
	"strings"

	"github.com/hochfrequenz/claude-plan-orchestrator/internal/domain"
)

const prBodyTemplate = `## Summary
Implements %s

## Changes
%s

## Test Results
- %d tests passed
- %s

## Epic Reference
[%s](%s)

---
Autonomous implementation by ERP Orchestrator
`

// PRBot handles PR creation and management
type PRBot struct {
	repoDir string
}

// NewPRBot creates a new PRBot
func NewPRBot(repoDir string) *PRBot {
	return &PRBot{repoDir: repoDir}
}

// BuildPRBody constructs the PR body
func BuildPRBody(task *domain.Task, changeSummary string, testsPassed int, duration string) string {
	return fmt.Sprintf(prBodyTemplate,
		task.Title,
		changeSummary,
		testsPassed,
		duration,
		task.FilePath,
		task.FilePath,
	)
}

// CreatePR creates a pull request using gh CLI
func (p *PRBot) CreatePR(worktreePath string, task *domain.Task, body string) (int, string, error) {
	title := fmt.Sprintf("feat(%s): implement E%02d - %s",
		task.ID.Module,
		task.ID.EpicNum,
		task.Title,
	)

	// Push the branch first
	branch := fmt.Sprintf("feat/%s-E%02d", task.ID.Module, task.ID.EpicNum)
	pushCmd := exec.Command("git", "push", "-u", "origin", branch)
	pushCmd.Dir = worktreePath
	if out, err := pushCmd.CombinedOutput(); err != nil {
		return 0, "", fmt.Errorf("git push: %s: %w", out, err)
	}

	// Create PR
	cmd := exec.Command("gh", "pr", "create",
		"--title", title,
		"--body", body,
		"--head", branch,
	)
	cmd.Dir = worktreePath
	out, err := cmd.CombinedOutput()
	if err != nil {
		return 0, "", fmt.Errorf("gh pr create: %s: %w", out, err)
	}

	// Parse PR URL from output
	url := strings.TrimSpace(string(out))
	prNum := extractPRNumber(url)

	return prNum, url, nil
}

// AddLabels adds labels to a PR
func (p *PRBot) AddLabels(prNumber int, labels []string) error {
	args := []string{"pr", "edit", fmt.Sprintf("%d", prNumber)}
	for _, label := range labels {
		args = append(args, "--add-label", label)
	}

	cmd := exec.Command("gh", args...)
	cmd.Dir = p.repoDir
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("gh pr edit: %s: %w", out, err)
	}
	return nil
}

// MergePR merges a PR with squash
func (p *PRBot) MergePR(prNumber int) error {
	cmd := exec.Command("gh", "pr", "merge", fmt.Sprintf("%d", prNumber),
		"--squash",
		"--delete-branch",
	)
	cmd.Dir = p.repoDir
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("gh pr merge: %s: %w", out, err)
	}
	return nil
}

// GetDiff gets the diff for a PR
func (p *PRBot) GetDiff(prNumber int) (string, error) {
	cmd := exec.Command("gh", "pr", "diff", fmt.Sprintf("%d", prNumber))
	cmd.Dir = p.repoDir
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return string(out), nil
}

func extractPRNumber(url string) int {
	// URL format: https://github.com/owner/repo/pull/123
	parts := strings.Split(url, "/")
	if len(parts) > 0 {
		var num int
		fmt.Sscanf(parts[len(parts)-1], "%d", &num)
		return num
	}
	return 0
}
