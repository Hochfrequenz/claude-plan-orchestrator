// internal/issues/fetcher_test.go
package issues

import (
	"testing"
)

func TestParseIssueFromGH(t *testing.T) {
	// Simulated gh issue view --json output
	jsonOutput := `{
		"number": 42,
		"title": "Add retry logic",
		"body": "We need retry logic for API calls",
		"labels": [{"name": "area:billing"}, {"name": "priority:high"}]
	}`

	issue, err := parseIssueFromJSON([]byte(jsonOutput))
	if err != nil {
		t.Fatalf("parseIssueFromJSON() error = %v", err)
	}

	if issue.IssueNumber != 42 {
		t.Errorf("IssueNumber = %v, want 42", issue.IssueNumber)
	}
	if issue.Title != "Add retry logic" {
		t.Errorf("Title = %v, want 'Add retry logic'", issue.Title)
	}
}

func TestExtractAreaLabel(t *testing.T) {
	tests := []struct {
		labels []string
		prefix string
		want   string
	}{
		{[]string{"area:billing", "bug"}, "area:", "billing"},
		{[]string{"bug", "enhancement"}, "area:", ""},
		{[]string{"module:auth", "area:billing"}, "area:", "billing"},
	}

	for _, tt := range tests {
		got := extractAreaLabel(tt.labels, tt.prefix)
		if got != tt.want {
			t.Errorf("extractAreaLabel(%v, %q) = %q, want %q", tt.labels, tt.prefix, got, tt.want)
		}
	}
}
