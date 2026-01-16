# GitHub Issues Integration Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Enable GitHub issues as an external task source with LLM-powered triage, plan generation, and automatic issue closure after implementation.

**Architecture:** Issues labeled `orchestrator-candidate` are fetched during sync. Claude Code agents analyze each issue for readiness, generate implementation plans as markdown files, and integrate them into the existing task queue. After PR merge, issues are automatically closed with a summary.

**Tech Stack:** Go, SQLite, Claude Code CLI, GitHub MCP tools, `gh` CLI

**Reference Design:** `docs/plans/2026-01-16-github-issues-integration-design.md`

---

## Task 1: Add GitHubIssue Domain Type

**Files:**
- Create: `internal/domain/github_issue.go`
- Test: `internal/domain/github_issue_test.go`

**Step 1: Write the failing test**

```go
// internal/domain/github_issue_test.go
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
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/domain/... -run TestIssueStatus -v`
Expected: FAIL - file does not exist

**Step 3: Write minimal implementation**

```go
// internal/domain/github_issue.go
package domain

import (
	"fmt"
	"time"
)

type IssueStatus string

const (
	IssuePending         IssueStatus = "pending"
	IssueReady           IssueStatus = "ready"
	IssueNeedsRefinement IssueStatus = "needs_refinement"
	IssueImplemented     IssueStatus = "implemented"
)

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
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/domain/... -run "TestIssueStatus|TestGitHubIssue_TaskGroup" -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/domain/github_issue.go internal/domain/github_issue_test.go
git commit -m "feat(domain): add GitHubIssue type and IssueStatus constants"
```

---

## Task 2: Add github_issue Field to Task Domain

**Files:**
- Modify: `internal/domain/task.go`
- Test: `internal/domain/task_test.go` (if exists, else create)

**Step 1: Write the failing test**

```go
// Add to internal/domain/task_test.go (create if needed)
package domain

import "testing"

func TestTask_HasGitHubIssue(t *testing.T) {
	issueNum := 42
	taskWithIssue := Task{GitHubIssue: &issueNum}
	taskWithoutIssue := Task{}

	if !taskWithIssue.HasGitHubIssue() {
		t.Error("expected HasGitHubIssue() = true for task with issue")
	}
	if taskWithoutIssue.HasGitHubIssue() {
		t.Error("expected HasGitHubIssue() = false for task without issue")
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/domain/... -run TestTask_HasGitHubIssue -v`
Expected: FAIL - GitHubIssue field does not exist

**Step 3: Write minimal implementation**

Add to `internal/domain/task.go` in the Task struct:

```go
// Add this field to the Task struct (find the struct definition)
GitHubIssue *int // Source GitHub issue number, nil if not from issue
```

Add this method:

```go
// HasGitHubIssue returns true if this task originated from a GitHub issue.
func (t *Task) HasGitHubIssue() bool {
	return t.GitHubIssue != nil
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/domain/... -run TestTask_HasGitHubIssue -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/domain/task.go internal/domain/task_test.go
git commit -m "feat(domain): add GitHubIssue field to Task"
```

---

## Task 3: Add github_issue Field to Frontmatter Parser

**Files:**
- Modify: `internal/parser/frontmatter.go`
- Modify: `internal/parser/parser.go`
- Test: `internal/parser/parser_test.go`

**Step 1: Write the failing test**

```go
// Add to internal/parser/parser_test.go
func TestParseFrontmatter_GitHubIssue(t *testing.T) {
	content := []byte(`---
status: not_started
priority: medium
github_issue: 42
---
# Test Epic
`)
	fm, _, err := ParseFrontmatter(content)
	if err != nil {
		t.Fatalf("ParseFrontmatter() error = %v", err)
	}
	if fm.GitHubIssue == nil || *fm.GitHubIssue != 42 {
		t.Errorf("GitHubIssue = %v, want 42", fm.GitHubIssue)
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/parser/... -run TestParseFrontmatter_GitHubIssue -v`
Expected: FAIL - GitHubIssue field does not exist in Frontmatter

**Step 3: Write minimal implementation**

Add to the Frontmatter struct in `internal/parser/frontmatter.go`:

```go
GitHubIssue *int `yaml:"github_issue"`
```

Update `ParseEpicFile` in `internal/parser/parser.go` to pass through the field:

```go
// In ParseEpicFile, after creating the task, add:
task.GitHubIssue = fm.GitHubIssue
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/parser/... -run TestParseFrontmatter_GitHubIssue -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/parser/frontmatter.go internal/parser/parser.go internal/parser/parser_test.go
git commit -m "feat(parser): support github_issue field in frontmatter"
```

---

## Task 4: Add Database Migration for github_issues Table

**Files:**
- Modify: `internal/taskstore/migrations.go`
- Test: `internal/taskstore/store_test.go`

**Step 1: Write the failing test**

```go
// Add to internal/taskstore/store_test.go
func TestStore_GitHubIssuesTableExists(t *testing.T) {
	store := setupTestStore(t)
	defer store.Close()

	// Check table exists by attempting a query
	_, err := store.db.Exec("SELECT issue_number FROM github_issues LIMIT 1")
	if err != nil {
		t.Errorf("github_issues table should exist: %v", err)
	}
}

func TestStore_TasksGitHubIssueColumn(t *testing.T) {
	store := setupTestStore(t)
	defer store.Close()

	// Check column exists
	_, err := store.db.Exec("SELECT github_issue FROM tasks LIMIT 1")
	if err != nil {
		t.Errorf("tasks.github_issue column should exist: %v", err)
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/taskstore/... -run "TestStore_GitHubIssuesTableExists|TestStore_TasksGitHubIssueColumn" -v`
Expected: FAIL - table/column does not exist

**Step 3: Write minimal implementation**

Add to `internal/taskstore/migrations.go`:

```go
const migrationGitHubIssues = `
CREATE TABLE IF NOT EXISTS github_issues (
    issue_number  INTEGER PRIMARY KEY,
    repo          TEXT NOT NULL,
    title         TEXT NOT NULL,
    status        TEXT NOT NULL DEFAULT 'pending',
    group_name    TEXT,
    analyzed_at   TIMESTAMP,
    plan_path     TEXT,
    closed_at     TIMESTAMP,
    pr_number     INTEGER,
    created_at    TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at    TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

-- Add github_issue column to tasks if not exists
-- SQLite doesn't have IF NOT EXISTS for ALTER TABLE, so we handle errors
`

const migrationTasksGitHubIssue = `
ALTER TABLE tasks ADD COLUMN github_issue INTEGER REFERENCES github_issues(issue_number);
`
```

In the `New()` function (or migration runner), add after existing migrations:

```go
// Run github_issues migration
if _, err := db.Exec(migrationGitHubIssues); err != nil {
    return nil, fmt.Errorf("github_issues migration: %w", err)
}

// Add github_issue column to tasks (ignore error if already exists)
db.Exec(migrationTasksGitHubIssue)
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/taskstore/... -run "TestStore_GitHubIssuesTableExists|TestStore_TasksGitHubIssueColumn" -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/taskstore/migrations.go
git commit -m "feat(taskstore): add github_issues table and tasks.github_issue column"
```

---

## Task 5: Add GitHubIssue Store Methods

**Files:**
- Modify: `internal/taskstore/store.go`
- Test: `internal/taskstore/store_test.go`

**Step 1: Write the failing test**

```go
// Add to internal/taskstore/store_test.go
func TestStore_UpsertAndGetGitHubIssue(t *testing.T) {
	store := setupTestStore(t)
	defer store.Close()

	issue := &domain.GitHubIssue{
		IssueNumber: 42,
		Repo:        "owner/repo",
		Title:       "Test Issue",
		Status:      domain.IssuePending,
		GroupName:   "billing",
	}

	// Upsert
	err := store.UpsertGitHubIssue(issue)
	if err != nil {
		t.Fatalf("UpsertGitHubIssue() error = %v", err)
	}

	// Get
	got, err := store.GetGitHubIssue(42)
	if err != nil {
		t.Fatalf("GetGitHubIssue() error = %v", err)
	}
	if got.Title != "Test Issue" {
		t.Errorf("Title = %v, want %v", got.Title, "Test Issue")
	}
	if got.GroupName != "billing" {
		t.Errorf("GroupName = %v, want %v", got.GroupName, "billing")
	}
}

func TestStore_ListPendingIssues(t *testing.T) {
	store := setupTestStore(t)
	defer store.Close()

	// Insert test issues
	store.UpsertGitHubIssue(&domain.GitHubIssue{
		IssueNumber: 1, Repo: "owner/repo", Title: "Pending", Status: domain.IssuePending,
	})
	store.UpsertGitHubIssue(&domain.GitHubIssue{
		IssueNumber: 2, Repo: "owner/repo", Title: "Ready", Status: domain.IssueReady,
	})
	store.UpsertGitHubIssue(&domain.GitHubIssue{
		IssueNumber: 3, Repo: "other/repo", Title: "Other Repo", Status: domain.IssuePending,
	})

	issues, err := store.ListPendingIssues("owner/repo")
	if err != nil {
		t.Fatalf("ListPendingIssues() error = %v", err)
	}
	if len(issues) != 1 {
		t.Errorf("got %d pending issues, want 1", len(issues))
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/taskstore/... -run "TestStore_UpsertAndGetGitHubIssue|TestStore_ListPendingIssues" -v`
Expected: FAIL - methods do not exist

**Step 3: Write minimal implementation**

Add to `internal/taskstore/store.go`:

```go
func (s *Store) UpsertGitHubIssue(issue *domain.GitHubIssue) error {
	now := time.Now()
	_, err := s.db.Exec(`
		INSERT INTO github_issues (issue_number, repo, title, status, group_name, analyzed_at, plan_path, closed_at, pr_number, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(issue_number) DO UPDATE SET
			repo = excluded.repo,
			title = excluded.title,
			status = excluded.status,
			group_name = excluded.group_name,
			analyzed_at = excluded.analyzed_at,
			plan_path = excluded.plan_path,
			closed_at = excluded.closed_at,
			pr_number = excluded.pr_number,
			updated_at = excluded.updated_at
	`, issue.IssueNumber, issue.Repo, issue.Title, string(issue.Status), issue.GroupName,
		issue.AnalyzedAt, issue.PlanPath, issue.ClosedAt, issue.PRNumber, now, now)
	return err
}

func (s *Store) GetGitHubIssue(issueNumber int) (*domain.GitHubIssue, error) {
	row := s.db.QueryRow(`
		SELECT issue_number, repo, title, status, group_name, analyzed_at, plan_path, closed_at, pr_number, created_at, updated_at
		FROM github_issues WHERE issue_number = ?
	`, issueNumber)

	var issue domain.GitHubIssue
	var status string
	err := row.Scan(&issue.IssueNumber, &issue.Repo, &issue.Title, &status, &issue.GroupName,
		&issue.AnalyzedAt, &issue.PlanPath, &issue.ClosedAt, &issue.PRNumber, &issue.CreatedAt, &issue.UpdatedAt)
	if err != nil {
		return nil, err
	}
	issue.Status = domain.IssueStatus(status)
	return &issue, nil
}

func (s *Store) ListPendingIssues(repo string) ([]*domain.GitHubIssue, error) {
	rows, err := s.db.Query(`
		SELECT issue_number, repo, title, status, group_name, analyzed_at, plan_path, closed_at, pr_number, created_at, updated_at
		FROM github_issues WHERE repo = ? AND status = ?
	`, repo, string(domain.IssuePending))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var issues []*domain.GitHubIssue
	for rows.Next() {
		var issue domain.GitHubIssue
		var status string
		if err := rows.Scan(&issue.IssueNumber, &issue.Repo, &issue.Title, &status, &issue.GroupName,
			&issue.AnalyzedAt, &issue.PlanPath, &issue.ClosedAt, &issue.PRNumber, &issue.CreatedAt, &issue.UpdatedAt); err != nil {
			return nil, err
		}
		issue.Status = domain.IssueStatus(status)
		issues = append(issues, &issue)
	}
	return issues, rows.Err()
}

func (s *Store) UpdateIssueStatus(issueNumber int, status domain.IssueStatus) error {
	_, err := s.db.Exec(`UPDATE github_issues SET status = ?, updated_at = ? WHERE issue_number = ?`,
		string(status), time.Now(), issueNumber)
	return err
}

func (s *Store) MarkIssueClosed(issueNumber int, prNumber int) error {
	now := time.Now()
	_, err := s.db.Exec(`UPDATE github_issues SET status = ?, closed_at = ?, pr_number = ?, updated_at = ? WHERE issue_number = ?`,
		string(domain.IssueImplemented), now, prNumber, now, issueNumber)
	return err
}

func (s *Store) GetTasksByGitHubIssue(issueNumber int) ([]*domain.Task, error) {
	return s.ListTasks(ListOptions{GitHubIssue: &issueNumber})
}

func (s *Store) GetIncompleteEpicsForIssue(issueNumber int) ([]*domain.Task, error) {
	rows, err := s.db.Query(`
		SELECT id, module, epic_num, title, description, status, priority, depends_on, needs_review, file_path, github_issue, created_at, updated_at
		FROM tasks WHERE github_issue = ? AND status != ?
	`, issueNumber, string(domain.StatusComplete))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return s.scanTasks(rows)
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/taskstore/... -run "TestStore_UpsertAndGetGitHubIssue|TestStore_ListPendingIssues" -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/taskstore/store.go internal/taskstore/store_test.go
git commit -m "feat(taskstore): add GitHubIssue CRUD methods"
```

---

## Task 6: Add GitHubIssues Config Section

**Files:**
- Modify: `internal/config/config.go`
- Test: `internal/config/config_test.go`

**Step 1: Write the failing test**

```go
// Add to internal/config/config_test.go
func TestConfig_GitHubIssues(t *testing.T) {
	tomlContent := `
[general]
project_root = "/tmp/test"

[github_issues]
enabled = true
repo = "owner/repo"
candidate_label = "orchestrator-candidate"
ready_label = "implementation-ready"
refinement_label = "needs-refinement"
implemented_label = "implemented"
area_label_prefix = "area:"

[github_issues.priority_labels]
high = "priority:high"
medium = "priority:medium"
low = "priority:low"
`
	tmpFile := writeTempConfig(t, tomlContent)
	cfg, err := Load(tmpFile)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if !cfg.GitHubIssues.Enabled {
		t.Error("expected GitHubIssues.Enabled = true")
	}
	if cfg.GitHubIssues.Repo != "owner/repo" {
		t.Errorf("Repo = %v, want owner/repo", cfg.GitHubIssues.Repo)
	}
	if cfg.GitHubIssues.CandidateLabel != "orchestrator-candidate" {
		t.Errorf("CandidateLabel = %v, want orchestrator-candidate", cfg.GitHubIssues.CandidateLabel)
	}
	if cfg.GitHubIssues.AreaLabelPrefix != "area:" {
		t.Errorf("AreaLabelPrefix = %v, want area:", cfg.GitHubIssues.AreaLabelPrefix)
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/config/... -run TestConfig_GitHubIssues -v`
Expected: FAIL - GitHubIssues field does not exist

**Step 3: Write minimal implementation**

Add to `internal/config/config.go`:

```go
type GitHubIssuesConfig struct {
	Enabled          bool              `toml:"enabled"`
	Repo             string            `toml:"repo"`
	CandidateLabel   string            `toml:"candidate_label"`
	ReadyLabel       string            `toml:"ready_label"`
	RefinementLabel  string            `toml:"refinement_label"`
	ImplementedLabel string            `toml:"implemented_label"`
	AreaLabelPrefix  string            `toml:"area_label_prefix"`
	PriorityLabels   map[string]string `toml:"priority_labels"`
}

// Add to Config struct:
GitHubIssues GitHubIssuesConfig `toml:"github_issues"`
```

Add defaults in `Default()`:

```go
GitHubIssues: GitHubIssuesConfig{
	Enabled:          false,
	CandidateLabel:   "orchestrator-candidate",
	ReadyLabel:       "implementation-ready",
	RefinementLabel:  "needs-refinement",
	ImplementedLabel: "implemented",
	AreaLabelPrefix:  "area:",
	PriorityLabels:   map[string]string{},
},
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/config/... -run TestConfig_GitHubIssues -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/config/config.go internal/config/config_test.go
git commit -m "feat(config): add GitHubIssues configuration section"
```

---

## Task 7: Create issues Package - Fetcher

**Files:**
- Create: `internal/issues/fetcher.go`
- Test: `internal/issues/fetcher_test.go`

**Step 1: Write the failing test**

```go
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
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/issues/... -run "TestParseIssueFromGH|TestExtractAreaLabel" -v`
Expected: FAIL - package does not exist

**Step 3: Write minimal implementation**

```go
// internal/issues/fetcher.go
package issues

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"

	"github.com/anthropics/claude-orch/internal/config"
	"github.com/anthropics/claude-orch/internal/domain"
)

type Fetcher struct {
	config *config.GitHubIssuesConfig
}

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
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/issues/... -run "TestParseIssueFromGH|TestExtractAreaLabel" -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/issues/fetcher.go internal/issues/fetcher_test.go
git commit -m "feat(issues): add Fetcher for GitHub issue operations"
```

---

## Task 8: Create issues Package - Prompt Templates

**Files:**
- Create: `internal/issues/prompt.go`
- Test: `internal/issues/prompt_test.go`

**Step 1: Write the failing test**

```go
// internal/issues/prompt_test.go
package issues

import (
	"strings"
	"testing"
)

func TestBuildAnalysisPrompt(t *testing.T) {
	prompt := BuildAnalysisPrompt(42, "owner/repo", "/path/to/plans")

	if !strings.Contains(prompt, "42") {
		t.Error("prompt should contain issue number")
	}
	if !strings.Contains(prompt, "owner/repo") {
		t.Error("prompt should contain repo")
	}
	if !strings.Contains(prompt, "problem_statement") {
		t.Error("prompt should mention checklist items")
	}
	if !strings.Contains(prompt, "JSON") {
		t.Error("prompt should request JSON output")
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/issues/... -run TestBuildAnalysisPrompt -v`
Expected: FAIL - function does not exist

**Step 3: Write minimal implementation**

```go
// internal/issues/prompt.go
package issues

import "fmt"

const analysisPromptTemplate = `You are analyzing GitHub issue #%d from repository %s for implementation readiness.

## Your Task

1. Use the GitHub MCP tools to fetch the full issue details (title, body, comments, labels)
2. Evaluate against the readiness checklist below
3. Scan existing plans in %s to identify potential dependencies
4. Output your analysis as JSON

## Readiness Checklist

Evaluate each criterion:
- problem_statement: Is there a clear description of what problem needs to be solved?
- acceptance_criteria: Are there defined success criteria or expected outcomes?
- bounded_scope: Is the scope limited and achievable (not open-ended)?
- no_blocking_questions: Are there unanswered questions that block implementation?
- files_identified: Can you identify which files/areas of code are affected?

## Output Format

Return a JSON object with this exact structure:
` + "```json" + `
{
  "issue_number": %d,
  "ready": true/false,
  "checklist": {
    "problem_statement": { "pass": true/false, "notes": "explanation" },
    "acceptance_criteria": { "pass": true/false, "notes": "explanation" },
    "bounded_scope": { "pass": true/false, "notes": "explanation" },
    "no_blocking_questions": { "pass": true/false, "notes": "explanation" },
    "files_identified": { "pass": true/false, "notes": "list of files or areas" }
  },
  "group": "area-label-value or empty string",
  "plan_files": ["path/to/epic.md"],
  "dependencies": ["module/E##"],
  "comment_posted": true,
  "labels_updated": true,
  "refinement_suggestions": ["suggestion 1", "suggestion 2"]
}
` + "```" + `

## If NOT Ready

- Post a comment explaining what information is missing
- Include specific suggestions for improvement
- Add label: needs-refinement
- Remove label: orchestrator-candidate

## If Ready

- Generate an implementation plan as a markdown file
- Write it to: docs/plans/{group}/issue-%d/epic-00-{slug}.md
- The group comes from area:X label, or use "issue-%d" if no area label
- Post a comment confirming the plan was created
- Add label: implementation-ready
- Remove label: orchestrator-candidate

## Plan Format

Use this frontmatter:
` + "```yaml" + `
---
status: not_started
priority: medium
depends_on:
  - module/E## (if any dependencies found)
needs_review: false
github_issue: %d
---
` + "```" + `

Now analyze issue #%d and produce your output.
`

func BuildAnalysisPrompt(issueNumber int, repo, plansDir string) string {
	return fmt.Sprintf(analysisPromptTemplate,
		issueNumber, repo, plansDir,
		issueNumber,
		issueNumber, issueNumber,
		issueNumber,
		issueNumber)
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/issues/... -run TestBuildAnalysisPrompt -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/issues/prompt.go internal/issues/prompt_test.go
git commit -m "feat(issues): add analysis agent prompt template"
```

---

## Task 9: Create issues Package - Analyzer

**Files:**
- Create: `internal/issues/analyzer.go`
- Test: `internal/issues/analyzer_test.go`

**Step 1: Write the failing test**

```go
// internal/issues/analyzer_test.go
package issues

import (
	"encoding/json"
	"testing"
)

func TestParseAnalysisResult(t *testing.T) {
	output := `{
		"issue_number": 42,
		"ready": true,
		"checklist": {
			"problem_statement": {"pass": true, "notes": ""},
			"acceptance_criteria": {"pass": true, "notes": ""},
			"bounded_scope": {"pass": true, "notes": ""},
			"no_blocking_questions": {"pass": true, "notes": ""},
			"files_identified": {"pass": true, "notes": "src/api/billing.go"}
		},
		"group": "billing",
		"plan_files": ["docs/plans/billing/issue-42/epic-00-add-retry.md"],
		"dependencies": ["billing/E03"],
		"comment_posted": true,
		"labels_updated": true
	}`

	result, err := ParseAnalysisResult([]byte(output))
	if err != nil {
		t.Fatalf("ParseAnalysisResult() error = %v", err)
	}

	if !result.Ready {
		t.Error("expected Ready = true")
	}
	if result.Group != "billing" {
		t.Errorf("Group = %v, want billing", result.Group)
	}
	if len(result.PlanFiles) != 1 {
		t.Errorf("PlanFiles count = %d, want 1", len(result.PlanFiles))
	}
}

func TestAnalysisResult_AllChecksPassed(t *testing.T) {
	result := &AnalysisResult{
		Checklist: map[string]ChecklistItem{
			"problem_statement":      {Pass: true},
			"acceptance_criteria":    {Pass: true},
			"bounded_scope":          {Pass: true},
			"no_blocking_questions":  {Pass: true},
			"files_identified":       {Pass: true},
		},
	}
	if !result.AllChecksPassed() {
		t.Error("expected AllChecksPassed() = true")
	}

	result.Checklist["bounded_scope"] = ChecklistItem{Pass: false}
	if result.AllChecksPassed() {
		t.Error("expected AllChecksPassed() = false when one fails")
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/issues/... -run "TestParseAnalysisResult|TestAnalysisResult_AllChecksPassed" -v`
Expected: FAIL - types do not exist

**Step 3: Write minimal implementation**

```go
// internal/issues/analyzer.go
package issues

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/anthropics/claude-orch/internal/config"
	"github.com/anthropics/claude-orch/internal/domain"
	"github.com/anthropics/claude-orch/internal/parser"
	"github.com/anthropics/claude-orch/internal/taskstore"
)

type ChecklistItem struct {
	Pass  bool   `json:"pass"`
	Notes string `json:"notes"`
}

type AnalysisResult struct {
	IssueNumber           int                      `json:"issue_number"`
	Ready                 bool                     `json:"ready"`
	Checklist             map[string]ChecklistItem `json:"checklist"`
	Group                 string                   `json:"group"`
	PlanFiles             []string                 `json:"plan_files"`
	Dependencies          []string                 `json:"dependencies"`
	CommentPosted         bool                     `json:"comment_posted"`
	LabelsUpdated         bool                     `json:"labels_updated"`
	RefinementSuggestions []string                 `json:"refinement_suggestions,omitempty"`
}

func ParseAnalysisResult(data []byte) (*AnalysisResult, error) {
	var result AnalysisResult
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

func (r *AnalysisResult) AllChecksPassed() bool {
	for _, item := range r.Checklist {
		if !item.Pass {
			return false
		}
	}
	return true
}

type Analyzer struct {
	store   *taskstore.Store
	fetcher *Fetcher
	config  *config.GitHubIssuesConfig
	plansDir string
}

func NewAnalyzer(store *taskstore.Store, cfg *config.GitHubIssuesConfig, plansDir string) *Analyzer {
	return &Analyzer{
		store:    store,
		fetcher:  NewFetcher(cfg),
		config:   cfg,
		plansDir: plansDir,
	}
}

// AnalyzeCandidates fetches and analyzes all candidate issues.
func (a *Analyzer) AnalyzeCandidates(ctx context.Context, maxParallel int) error {
	issues, err := a.fetcher.FetchCandidateIssues()
	if err != nil {
		return fmt.Errorf("fetch candidates: %w", err)
	}

	// Filter out already-analyzed issues
	var toAnalyze []*domain.GitHubIssue
	for _, issue := range issues {
		existing, err := a.store.GetGitHubIssue(issue.IssueNumber)
		if err != nil || existing == nil || existing.Status == domain.IssuePending {
			toAnalyze = append(toAnalyze, issue)
		}
	}

	// Analyze with concurrency limit
	sem := make(chan struct{}, maxParallel)
	errCh := make(chan error, len(toAnalyze))

	for _, issue := range toAnalyze {
		sem <- struct{}{}
		go func(iss *domain.GitHubIssue) {
			defer func() { <-sem }()
			if err := a.analyzeIssue(ctx, iss); err != nil {
				errCh <- fmt.Errorf("issue #%d: %w", iss.IssueNumber, err)
			}
		}(issue)
	}

	// Wait for all to complete
	for i := 0; i < cap(sem); i++ {
		sem <- struct{}{}
	}
	close(errCh)

	// Collect errors
	var errs []error
	for err := range errCh {
		errs = append(errs, err)
	}
	if len(errs) > 0 {
		return fmt.Errorf("analysis errors: %v", errs)
	}
	return nil
}

func (a *Analyzer) analyzeIssue(ctx context.Context, issue *domain.GitHubIssue) error {
	// Save as pending
	issue.Status = domain.IssuePending
	if err := a.store.UpsertGitHubIssue(issue); err != nil {
		return err
	}

	// Build prompt
	prompt := BuildAnalysisPrompt(issue.IssueNumber, a.config.Repo, a.plansDir)

	// Spawn Claude Code agent
	cmd := exec.CommandContext(ctx, "claude",
		"--print",
		"--dangerously-skip-permissions",
		"--output-format", "text",
		"-p", prompt)
	cmd.Dir = filepath.Dir(a.plansDir) // project root

	output, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("claude agent: %w", err)
	}

	// Parse result from output (agent should output JSON)
	result, err := extractJSONFromOutput(output)
	if err != nil {
		return fmt.Errorf("parse result: %w", err)
	}

	// Update issue status based on result
	now := time.Now()
	issue.AnalyzedAt = &now
	if result.Ready {
		issue.Status = domain.IssueReady
		issue.GroupName = result.Group
		if len(result.PlanFiles) > 0 {
			issue.PlanPath = result.PlanFiles[0]
		}
	} else {
		issue.Status = domain.IssueNeedsRefinement
	}

	if err := a.store.UpsertGitHubIssue(issue); err != nil {
		return err
	}

	// If ready, parse and upsert the generated task
	if result.Ready && len(result.PlanFiles) > 0 {
		for _, planPath := range result.PlanFiles {
			task, err := parser.ParseEpicFile(planPath)
			if err != nil {
				return fmt.Errorf("parse plan: %w", err)
			}
			if err := a.store.UpsertTask(task); err != nil {
				return fmt.Errorf("upsert task: %w", err)
			}
		}
	}

	return nil
}

func extractJSONFromOutput(output []byte) (*AnalysisResult, error) {
	// Try to find JSON in the output (may be wrapped in markdown code blocks)
	str := string(output)

	// Look for JSON object
	start := -1
	depth := 0
	for i, c := range str {
		if c == '{' {
			if start == -1 {
				start = i
			}
			depth++
		} else if c == '}' {
			depth--
			if depth == 0 && start != -1 {
				return ParseAnalysisResult([]byte(str[start : i+1]))
			}
		}
	}
	return nil, fmt.Errorf("no JSON object found in output")
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/issues/... -run "TestParseAnalysisResult|TestAnalysisResult_AllChecksPassed" -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/issues/analyzer.go internal/issues/analyzer_test.go
git commit -m "feat(issues): add Analyzer for issue analysis orchestration"
```

---

## Task 10: Create issues Package - Closer

**Files:**
- Create: `internal/issues/closer.go`
- Test: `internal/issues/closer_test.go`

**Step 1: Write the failing test**

```go
// internal/issues/closer_test.go
package issues

import (
	"testing"
)

func TestBuildClosureComment(t *testing.T) {
	comment := BuildClosureComment(123, "Added retry logic", []string{"src/api.go", "src/api_test.go"})

	if comment == "" {
		t.Error("expected non-empty comment")
	}
	// Check key elements
	tests := []string{"123", "retry logic", "src/api.go", "Claude Plan Orchestrator"}
	for _, want := range tests {
		if !contains(comment, want) {
			t.Errorf("comment missing %q", want)
		}
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsHelper(s, substr))
}

func containsHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/issues/... -run TestBuildClosureComment -v`
Expected: FAIL - function does not exist

**Step 3: Write minimal implementation**

```go
// internal/issues/closer.go
package issues

import (
	"context"
	"fmt"
	"strings"

	"github.com/anthropics/claude-orch/internal/config"
	"github.com/anthropics/claude-orch/internal/domain"
	"github.com/anthropics/claude-orch/internal/taskstore"
)

type Closer struct {
	store   *taskstore.Store
	fetcher *Fetcher
	config  *config.GitHubIssuesConfig
}

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

func BuildClosureComment(prNumber int, summary string, changedFiles []string) string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("✅ **Implemented in PR #%d**\n\n", prNumber))
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
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/issues/... -run TestBuildClosureComment -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/issues/closer.go internal/issues/closer_test.go
git commit -m "feat(issues): add Closer for post-merge issue closure"
```

---

## Task 11: Integrate Analyzer into Sync Command

**Files:**
- Modify: `cmd/claude-orch/commands.go` (or `sync.go`)

**Step 1: Write the failing test**

This is an integration point - test manually with:

```bash
go build -o claude-orch ./cmd/claude-orch && ./claude-orch sync --help
```

Expected: Should show `--skip-issues` and `--issues-only` flags (will fail until implemented)

**Step 2: Identify the sync command handler**

Look for `runSync` function in `cmd/claude-orch/commands.go`

**Step 3: Write minimal implementation**

Add flags to sync command:

```go
func init() {
	syncCmd.Flags().Bool("skip-issues", false, "Skip GitHub issue analysis")
	syncCmd.Flags().Bool("issues-only", false, "Only analyze issues, skip markdown sync")
}
```

Update `runSync`:

```go
func runSync(cmd *cobra.Command, args []string) error {
	skipIssues, _ := cmd.Flags().GetBool("skip-issues")
	issuesOnly, _ := cmd.Flags().GetBool("issues-only")

	cfg, err := config.LoadWithLocalFallback("")
	if err != nil {
		return err
	}

	store, err := taskstore.New(cfg.General.DatabasePath)
	if err != nil {
		return err
	}
	defer store.Close()

	// Markdown sync (unless --issues-only)
	if !issuesOnly {
		syncer := sync.New(filepath.Join(cfg.General.ProjectRoot, "docs", "plans"))
		result, err := syncer.TwoWaySync(store)
		if err != nil {
			return fmt.Errorf("sync: %w", err)
		}
		fmt.Printf("Synced %d tasks from markdown, %d to markdown\n",
			result.MarkdownToDBCount, result.DBToMarkdownCount)
	}

	// Issue analysis (unless --skip-issues or disabled)
	if !skipIssues && cfg.GitHubIssues.Enabled {
		analyzer := issues.NewAnalyzer(store, &cfg.GitHubIssues,
			filepath.Join(cfg.General.ProjectRoot, "docs", "plans"))
		if err := analyzer.AnalyzeCandidates(cmd.Context(), cfg.General.MaxParallelAgents); err != nil {
			return fmt.Errorf("issue analysis: %w", err)
		}
		fmt.Println("Issue analysis complete")
	}

	return nil
}
```

**Step 4: Build and verify**

Run: `go build -o claude-orch ./cmd/claude-orch && ./claude-orch sync --help`
Expected: Shows `--skip-issues` and `--issues-only` flags

**Step 5: Commit**

```bash
git add cmd/claude-orch/commands.go
git commit -m "feat(cli): integrate issue analysis into sync command"
```

---

## Task 12: Add Issues Subcommand

**Files:**
- Create: `cmd/claude-orch/issues.go`

**Step 1: Write the subcommand file**

```go
// cmd/claude-orch/issues.go
package main

import (
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/anthropics/claude-orch/internal/config"
	"github.com/anthropics/claude-orch/internal/domain"
	"github.com/anthropics/claude-orch/internal/issues"
	"github.com/anthropics/claude-orch/internal/taskstore"
	"github.com/spf13/cobra"
)

var issuesCmd = &cobra.Command{
	Use:   "issues",
	Short: "Manage GitHub issue integration",
}

var issuesListCmd = &cobra.Command{
	Use:   "list",
	Short: "List tracked GitHub issues",
	RunE:  runIssuesList,
}

var issuesAnalyzeCmd = &cobra.Command{
	Use:   "analyze [issue-number]",
	Short: "Manually trigger analysis for a specific issue",
	Args:  cobra.ExactArgs(1),
	RunE:  runIssuesAnalyze,
}

func init() {
	issuesCmd.AddCommand(issuesListCmd)
	issuesCmd.AddCommand(issuesAnalyzeCmd)
	rootCmd.AddCommand(issuesCmd)
}

func runIssuesList(cmd *cobra.Command, args []string) error {
	cfg, err := config.LoadWithLocalFallback("")
	if err != nil {
		return err
	}

	store, err := taskstore.New(cfg.General.DatabasePath)
	if err != nil {
		return err
	}
	defer store.Close()

	// List all issues from DB
	rows, err := store.DB().Query(`
		SELECT issue_number, repo, title, status, group_name, analyzed_at
		FROM github_issues ORDER BY issue_number DESC
	`)
	if err != nil {
		return err
	}
	defer rows.Close()

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "#\tStatus\tGroup\tTitle")
	fmt.Fprintln(w, "-\t------\t-----\t-----")

	for rows.Next() {
		var num int
		var repo, title, status string
		var group *string
		var analyzedAt *string
		rows.Scan(&num, &repo, &title, &status, &group, &analyzedAt)

		groupStr := "-"
		if group != nil && *group != "" {
			groupStr = *group
		}

		fmt.Fprintf(w, "%d\t%s\t%s\t%s\n", num, status, groupStr, truncate(title, 50))
	}
	w.Flush()

	return nil
}

func runIssuesAnalyze(cmd *cobra.Command, args []string) error {
	var issueNum int
	fmt.Sscanf(args[0], "%d", &issueNum)

	cfg, err := config.LoadWithLocalFallback("")
	if err != nil {
		return err
	}

	if !cfg.GitHubIssues.Enabled {
		return fmt.Errorf("github_issues not enabled in config")
	}

	store, err := taskstore.New(cfg.General.DatabasePath)
	if err != nil {
		return err
	}
	defer store.Close()

	// Create issue record
	issue := &domain.GitHubIssue{
		IssueNumber: issueNum,
		Repo:        cfg.GitHubIssues.Repo,
		Status:      domain.IssuePending,
	}

	analyzer := issues.NewAnalyzer(store, &cfg.GitHubIssues,
		filepath.Join(cfg.General.ProjectRoot, "docs", "plans"))

	fmt.Printf("Analyzing issue #%d...\n", issueNum)
	if err := analyzer.AnalyzeOne(cmd.Context(), issue); err != nil {
		return err
	}

	fmt.Println("Done")
	return nil
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max-3] + "..."
}
```

**Step 2: Add AnalyzeOne method to Analyzer**

Add to `internal/issues/analyzer.go`:

```go
// AnalyzeOne analyzes a single issue (for manual triggering).
func (a *Analyzer) AnalyzeOne(ctx context.Context, issue *domain.GitHubIssue) error {
	return a.analyzeIssue(ctx, issue)
}
```

**Step 3: Build and verify**

Run: `go build -o claude-orch ./cmd/claude-orch && ./claude-orch issues --help`
Expected: Shows `list` and `analyze` subcommands

**Step 4: Commit**

```bash
git add cmd/claude-orch/issues.go internal/issues/analyzer.go
git commit -m "feat(cli): add issues subcommand for manual issue management"
```

---

## Task 13: Integrate Closer into PRBot

**Files:**
- Modify: `internal/prbot/prbot.go`

**Step 1: Identify merge handler**

Find where PR merge is handled and add closer call.

**Step 2: Write integration**

Add to PRBot or create hook:

```go
// In the merge flow, after successful merge:
func (p *PRBot) OnMerge(task *domain.Task, prNumber int) error {
	if p.issueCloser != nil && task.GitHubIssue != nil {
		if err := p.issueCloser.CloseIfComplete(context.Background(), task, prNumber); err != nil {
			// Log but don't fail - PR is already merged
			log.Printf("warning: failed to close issue: %v", err)
		}
	}
	return nil
}
```

**Step 3: Wire up closer in PRBot initialization**

```go
type PRBot struct {
	repoDir      string
	issueCloser  *issues.Closer
}

func NewPRBot(repoDir string, issueCloser *issues.Closer) *PRBot {
	return &PRBot{
		repoDir:     repoDir,
		issueCloser: issueCloser,
	}
}
```

**Step 4: Update callers to pass closer**

Update TUI and other places that create PRBot.

**Step 5: Commit**

```bash
git add internal/prbot/prbot.go
git commit -m "feat(prbot): integrate issue closer for post-merge closure"
```

---

## Task 14: Add TUI Issue Indicator

**Files:**
- Modify: `tui/view.go`

**Step 1: Find task rendering**

Look for where task list items are rendered.

**Step 2: Add issue number display**

```go
// In task list rendering, add issue indicator:
func (m Model) renderTaskItem(task *domain.Task) string {
	var issueStr string
	if task.GitHubIssue != nil {
		issueStr = fmt.Sprintf(" #%d", *task.GitHubIssue)
	}
	return fmt.Sprintf("%s%s - %s", task.ID, issueStr, task.Title)
}
```

**Step 3: Build and test TUI**

Run: `go build -o claude-orch ./cmd/claude-orch && ./claude-orch tui`
Expected: Tasks from issues show `#N` indicator

**Step 4: Commit**

```bash
git add tui/view.go
git commit -m "feat(tui): show GitHub issue number on issue-sourced tasks"
```

---

## Task 15: Add Config Validation

**Files:**
- Modify: `internal/config/config.go`
- Test: `internal/config/config_test.go`

**Step 1: Write the failing test**

```go
func TestConfig_ValidateGitHubIssues(t *testing.T) {
	tests := []struct {
		name    string
		cfg     GitHubIssuesConfig
		wantErr bool
	}{
		{
			name:    "disabled is valid",
			cfg:     GitHubIssuesConfig{Enabled: false},
			wantErr: false,
		},
		{
			name:    "enabled without repo is invalid",
			cfg:     GitHubIssuesConfig{Enabled: true, Repo: ""},
			wantErr: true,
		},
		{
			name:    "enabled with repo is valid",
			cfg:     GitHubIssuesConfig{Enabled: true, Repo: "owner/repo"},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &Config{GitHubIssues: tt.cfg}
			err := cfg.ValidateGitHubIssues()
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateGitHubIssues() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/config/... -run TestConfig_ValidateGitHubIssues -v`
Expected: FAIL - method does not exist

**Step 3: Write minimal implementation**

```go
func (c *Config) ValidateGitHubIssues() error {
	if !c.GitHubIssues.Enabled {
		return nil
	}
	if c.GitHubIssues.Repo == "" {
		return fmt.Errorf("github_issues.repo is required when enabled")
	}
	return nil
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/config/... -run TestConfig_ValidateGitHubIssues -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/config/config.go internal/config/config_test.go
git commit -m "feat(config): add ValidateGitHubIssues method"
```

---

## Task 16: Run Full Test Suite

**Step 1: Run all tests**

```bash
go test ./... -v
```

**Step 2: Fix any failures**

Address any test failures that arise from the integration.

**Step 3: Run linter**

```bash
golangci-lint run
```

**Step 4: Fix any lint issues**

**Step 5: Final commit**

```bash
git add -A
git commit -m "test: ensure all tests pass after GitHub issues integration"
```

---

## Summary

This plan implements GitHub issues integration in 16 tasks:

1. **Tasks 1-3**: Domain model changes (GitHubIssue type, Task field, parser)
2. **Tasks 4-5**: Database schema and store methods
3. **Task 6**: Configuration section
4. **Tasks 7-10**: Core `internal/issues/` package (fetcher, prompt, analyzer, closer)
5. **Tasks 11-12**: CLI integration (sync flags, issues subcommand)
6. **Task 13**: PRBot integration for auto-closure
7. **Task 14**: TUI indicator
8. **Task 15**: Config validation
9. **Task 16**: Full test suite verification

Each task follows TDD: write failing test → implement → verify → commit.
