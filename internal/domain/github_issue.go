package domain

import (
	"fmt"
	"time"
)

// IssueStatus represents the lifecycle state of a GitHub issue
type IssueStatus string

const (
	IssuePending         IssueStatus = "pending"
	IssueReady           IssueStatus = "ready"
	IssueNeedsRefinement IssueStatus = "needs_refinement"
	IssueImplemented     IssueStatus = "implemented"
)

// GitHubIssue represents a GitHub issue being tracked for implementation
type GitHubIssue struct {
	IssueNumber int
	Repo        string
	Title       string
	Status      IssueStatus
	GroupName   string     // From area:X label, empty if none
	AnalyzedAt  *time.Time
	PlanPath    string // Path to generated markdown
	ClosedAt    *time.Time
	PRNumber    *int
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// TaskGroup returns the group path for tasks from this issue.
// If GroupName is set (from area label), returns "{group}/issue-{N}".
// Otherwise returns "issue-{N}".
func (i *GitHubIssue) TaskGroup() string {
	if i.GroupName != "" {
		return fmt.Sprintf("%s/issue-%d", i.GroupName, i.IssueNumber)
	}
	return fmt.Sprintf("issue-%d", i.IssueNumber)
}
