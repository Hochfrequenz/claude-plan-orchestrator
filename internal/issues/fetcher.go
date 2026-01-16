// internal/issues/fetcher.go
package issues

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"

	"github.com/hochfrequenz/claude-plan-orchestrator/internal/config"
	"github.com/hochfrequenz/claude-plan-orchestrator/internal/domain"
)

// Fetcher handles fetching and updating GitHub issues via gh CLI.
type Fetcher struct {
	config *config.GitHubIssuesConfig
}

// NewFetcher creates a new Fetcher with the given config.
func NewFetcher(cfg *config.GitHubIssuesConfig) *Fetcher {
	return &Fetcher{config: cfg}
}

type ghIssue struct {
	Number int    `json:"number"`
	Title  string `json:"title"`
	Body   string `json:"body"`
	Labels []struct {
		Name string `json:"name"`
	} `json:"labels"`
}

func parseIssueFromJSON(data []byte) (*domain.GitHubIssue, error) {
	var gh ghIssue
	if err := json.Unmarshal(data, &gh); err != nil {
		return nil, err
	}

	labels := make([]string, len(gh.Labels))
	for i, l := range gh.Labels {
		labels[i] = l.Name
	}

	return &domain.GitHubIssue{
		IssueNumber: gh.Number,
		Title:       gh.Title,
		Status:      domain.IssuePending,
	}, nil
}

func extractAreaLabel(labels []string, prefix string) string {
	for _, label := range labels {
		if strings.HasPrefix(label, prefix) {
			return strings.TrimPrefix(label, prefix)
		}
	}
	return ""
}

// FetchCandidateIssues returns issues with the candidate label that haven't been processed.
func (f *Fetcher) FetchCandidateIssues() ([]*domain.GitHubIssue, error) {
	// gh issue list --repo owner/repo --label "orchestrator-candidate" --json number,title,body,labels
	cmd := exec.Command("gh", "issue", "list",
		"--repo", f.config.Repo,
		"--label", f.config.CandidateLabel,
		"--json", "number,title,body,labels",
		"--limit", "100")

	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("gh issue list: %w", err)
	}

	var ghIssues []ghIssue
	if err := json.Unmarshal(output, &ghIssues); err != nil {
		return nil, fmt.Errorf("parse gh output: %w", err)
	}

	var issues []*domain.GitHubIssue
	for _, gh := range ghIssues {
		labels := make([]string, len(gh.Labels))
		for i, l := range gh.Labels {
			labels[i] = l.Name
		}

		// Skip if already has ready or refinement label
		if hasLabel(labels, f.config.ReadyLabel) || hasLabel(labels, f.config.RefinementLabel) {
			continue
		}

		issue := &domain.GitHubIssue{
			IssueNumber: gh.Number,
			Title:       gh.Title,
			Repo:        f.config.Repo,
			Status:      domain.IssuePending,
			GroupName:   extractAreaLabel(labels, f.config.AreaLabelPrefix),
		}
		issues = append(issues, issue)
	}

	return issues, nil
}

func hasLabel(labels []string, target string) bool {
	for _, l := range labels {
		if l == target {
			return true
		}
	}
	return false
}

// UpdateLabels adds and removes labels on an issue.
func (f *Fetcher) UpdateLabels(issueNumber int, add, remove []string) error {
	args := []string{"issue", "edit", fmt.Sprintf("%d", issueNumber), "--repo", f.config.Repo}
	for _, l := range add {
		args = append(args, "--add-label", l)
	}
	for _, l := range remove {
		args = append(args, "--remove-label", l)
	}
	return exec.Command("gh", args...).Run()
}

// PostComment posts a comment on an issue.
func (f *Fetcher) PostComment(issueNumber int, body string) error {
	cmd := exec.Command("gh", "issue", "comment", fmt.Sprintf("%d", issueNumber),
		"--repo", f.config.Repo, "--body", body)
	return cmd.Run()
}

// CloseIssue closes an issue as completed.
func (f *Fetcher) CloseIssue(issueNumber int) error {
	cmd := exec.Command("gh", "issue", "close", fmt.Sprintf("%d", issueNumber),
		"--repo", f.config.Repo, "--reason", "completed")
	return cmd.Run()
}
