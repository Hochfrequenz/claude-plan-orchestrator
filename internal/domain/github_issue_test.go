package domain

import (
	"testing"
)

func TestIssueStatus_String(t *testing.T) {
	tests := []struct {
		status IssueStatus
		want   string
	}{
		{IssuePending, "pending"},
		{IssueReady, "ready"},
		{IssueNeedsRefinement, "needs_refinement"},
		{IssueImplemented, "implemented"},
	}
	for _, tt := range tests {
		if got := string(tt.status); got != tt.want {
			t.Errorf("IssueStatus = %v, want %v", got, tt.want)
		}
	}
}

func TestGitHubIssue_TaskGroup(t *testing.T) {
	tests := []struct {
		name      string
		issue     GitHubIssue
		wantGroup string
	}{
		{
			name:      "with area label group",
			issue:     GitHubIssue{IssueNumber: 42, GroupName: "billing"},
			wantGroup: "billing/issue-42",
		},
		{
			name:      "without area label",
			issue:     GitHubIssue{IssueNumber: 105, GroupName: ""},
			wantGroup: "issue-105",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.issue.TaskGroup(); got != tt.wantGroup {
				t.Errorf("TaskGroup() = %v, want %v", got, tt.wantGroup)
			}
		})
	}
}
