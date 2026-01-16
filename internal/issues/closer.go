// internal/issues/closer.go
package issues

import (
	"context"
	"fmt"
	"strings"

	"github.com/hochfrequenz/claude-plan-orchestrator/internal/config"
	"github.com/hochfrequenz/claude-plan-orchestrator/internal/domain"
	"github.com/hochfrequenz/claude-plan-orchestrator/internal/taskstore"
)

// Closer handles closing GitHub issues after all tasks are complete.
type Closer struct {
	store   *taskstore.Store
	fetcher *Fetcher
	config  *config.GitHubIssuesConfig
}

// NewCloser creates a new Closer with the given store and config.
func NewCloser(store *taskstore.Store, cfg *config.GitHubIssuesConfig) *Closer {
	return &Closer{
		store:   store,
		fetcher: NewFetcher(cfg),
		config:  cfg,
	}
}

// CloseIfComplete checks if all epics for an issue are complete and closes the issue.
func (c *Closer) CloseIfComplete(ctx context.Context, task *domain.Task, prNumber int) error {
	if task.GitHubIssue == nil {
		return nil // Not from a GitHub issue
	}

	issueNumber := *task.GitHubIssue

	// Check if all epics for this issue are complete
	incomplete, err := c.store.GetIncompleteEpicsForIssue(issueNumber)
	if err != nil {
		return fmt.Errorf("get incomplete epics: %w", err)
	}

	if len(incomplete) > 0 {
		// Still have incomplete epics
		return nil
	}

	// Get the issue
	issue, err := c.store.GetGitHubIssue(issueNumber)
	if err != nil {
		return fmt.Errorf("get issue: %w", err)
	}

	if issue.Status == domain.IssueImplemented {
		return nil // Already closed
	}

	// Build closure comment
	// TODO: Get actual changed files from PR
	comment := BuildClosureComment(prNumber, task.Title, []string{})

	// Post comment and close
	if err := c.fetcher.PostComment(issueNumber, comment); err != nil {
		return fmt.Errorf("post comment: %w", err)
	}

	if err := c.fetcher.UpdateLabels(issueNumber, []string{c.config.ImplementedLabel}, nil); err != nil {
		return fmt.Errorf("update labels: %w", err)
	}

	if err := c.fetcher.CloseIssue(issueNumber); err != nil {
		return fmt.Errorf("close issue: %w", err)
	}

	// Update DB
	return c.store.MarkIssueClosed(issueNumber, prNumber)
}

// BuildClosureComment creates a formatted comment for closing a GitHub issue.
func BuildClosureComment(prNumber int, summary string, changedFiles []string) string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("\u2705 **Implemented in PR #%d**\n\n", prNumber))
	sb.WriteString(fmt.Sprintf("**Summary:** %s\n\n", summary))

	if len(changedFiles) > 0 {
		sb.WriteString("**Changed files:**\n")
		for _, f := range changedFiles {
			sb.WriteString(fmt.Sprintf("- `%s`\n", f))
		}
		sb.WriteString("\n")
	}

	sb.WriteString("---\n")
	sb.WriteString("*Implemented by Claude Plan Orchestrator*\n")

	return sb.String()
}
