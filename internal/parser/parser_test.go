package parser

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/hochfrequenz/claude-plan-orchestrator/internal/domain"
)

func TestParseEpicFile(t *testing.T) {
	content := `---
priority: high
depends_on: [billing/E01, technical/E03]
needs_review: true
---
# Epic 05: Validators

Implement input validation for all user-facing forms.

## Requirements

- Validate email format
- Validate phone numbers
`
	dir := t.TempDir()
	epicPath := filepath.Join(dir, "technical", "epic-05-validators.md")
	os.MkdirAll(filepath.Dir(epicPath), 0755)
	os.WriteFile(epicPath, []byte(content), 0644)

	task, err := ParseEpicFile(epicPath)
	if err != nil {
		t.Fatal(err)
	}

	if task.ID.String() != "technical/E05" {
		t.Errorf("ID = %q, want technical/E05", task.ID.String())
	}
	if task.Title != "Epic 05: Validators" {
		t.Errorf("Title = %q, want 'Epic 05: Validators'", task.Title)
	}
	if task.Priority != domain.PriorityHigh {
		t.Errorf("Priority = %q, want high", task.Priority)
	}
	if !task.NeedsReview {
		t.Error("NeedsReview should be true")
	}
	if len(task.DependsOn) != 2 {
		t.Errorf("DependsOn count = %d, want 2", len(task.DependsOn))
	}
}

func TestMatchEpicFile(t *testing.T) {
	tests := []struct {
		filename   string
		wantPrefix string
		wantNum    int
		wantOk     bool
	}{
		// Standard pattern: epic-01-name.md -> E01
		{"epic-00-setup.md", "", 0, true},
		{"epic-01-entities.md", "", 1, true},
		{"epic-99-final.md", "", 99, true},

		// Number prefix pattern: 01-epic-name.md -> E01
		{"01-epic-setup.md", "", 1, true},
		{"02-epic-foundation.md", "", 2, true},
		{"10-epic-testing.md", "", 10, true},

		// Subsystem prefix pattern: epic-cli-02-name.md -> CLI02
		{"epic-cli-02-customer.md", "CLI", 2, true},
		{"epic-tui-05-dashboard.md", "TUI", 5, true},
		{"epic-api-00-scaffolding.md", "API", 0, true},

		// Dot notation pattern: epic-1.2-name.md -> E2 (phase.epic)
		{"epic-1.1-workspace-setup.md", "", 1, true},
		{"epic-1.2-ci-cd-pipeline.md", "", 2, true},
		{"epic-2.3-something.md", "", 3, true},
		{"epic-10.15-large-numbers.md", "", 15, true},

		// Non-matching patterns
		{"README.md", "", 0, false},
		{"00-overview.md", "", 0, false},
		{"epic.md", "", 0, false},
		{"epic-name.md", "", 0, false},
		{"some-file.txt", "", 0, false},
	}

	for _, tt := range tests {
		t.Run(tt.filename, func(t *testing.T) {
			prefix, num, ok := matchEpicFile(tt.filename)
			if ok != tt.wantOk {
				t.Errorf("matchEpicFile(%q) ok = %v, want %v", tt.filename, ok, tt.wantOk)
			}
			if prefix != tt.wantPrefix {
				t.Errorf("matchEpicFile(%q) prefix = %q, want %q", tt.filename, prefix, tt.wantPrefix)
			}
			if num != tt.wantNum {
				t.Errorf("matchEpicFile(%q) num = %d, want %d", tt.filename, num, tt.wantNum)
			}
		})
	}
}

func TestParseModuleDir(t *testing.T) {
	dir := t.TempDir()
	moduleDir := filepath.Join(dir, "technical")
	os.MkdirAll(moduleDir, 0755)

	// Create overview
	os.WriteFile(filepath.Join(moduleDir, "00-overview.md"), []byte("# Technical Module\n\nOverview content."), 0644)

	// Create epics
	os.WriteFile(filepath.Join(moduleDir, "epic-00-scaffolding.md"), []byte("# Epic 00: Scaffolding\n\nSetup project."), 0644)
	os.WriteFile(filepath.Join(moduleDir, "epic-01-entities.md"), []byte("# Epic 01: Entities\n\nCreate entities."), 0644)

	tasks, err := ParseModuleDir(moduleDir)
	if err != nil {
		t.Fatal(err)
	}

	if len(tasks) != 2 {
		t.Errorf("Task count = %d, want 2", len(tasks))
	}

	// Check implicit dependency
	if tasks[1].ID.EpicNum == 1 {
		dep := tasks[1].ImplicitDependency()
		if dep == nil || dep.EpicNum != 0 {
			t.Error("Epic 01 should have implicit dependency on Epic 00")
		}
	}
}

func TestParseModuleDir_MissingPredecessor(t *testing.T) {
	// Test that implicit dependencies are NOT added when predecessor doesn't exist
	dir := t.TempDir()
	moduleDir := filepath.Join(dir, "pm-tool")
	os.MkdirAll(moduleDir, 0755)

	// Create E01 without E00 - simulates the user's scenario
	os.WriteFile(filepath.Join(moduleDir, "epic-01-foundation.md"), []byte("# Epic 01: Foundation\n\nSetup foundation."), 0644)
	os.WriteFile(filepath.Join(moduleDir, "epic-02-domain.md"), []byte("# Epic 02: Domain\n\nCore domain."), 0644)

	tasks, err := ParseModuleDir(moduleDir)
	if err != nil {
		t.Fatal(err)
	}

	if len(tasks) != 2 {
		t.Fatalf("Task count = %d, want 2", len(tasks))
	}

	// Find E01
	var e01 *domain.Task
	for _, task := range tasks {
		if task.ID.EpicNum == 1 {
			e01 = task
			break
		}
	}

	if e01 == nil {
		t.Fatal("E01 not found")
	}

	// E01 should have NO dependencies since E00 doesn't exist
	if len(e01.DependsOn) != 0 {
		t.Errorf("E01 should have no dependencies (E00 doesn't exist), got %v", e01.DependsOn)
	}

	// Find E02
	var e02 *domain.Task
	for _, task := range tasks {
		if task.ID.EpicNum == 2 {
			e02 = task
			break
		}
	}

	if e02 == nil {
		t.Fatal("E02 not found")
	}

	// E02 SHOULD depend on E01 since E01 exists
	if len(e02.DependsOn) != 1 {
		t.Errorf("E02 should have 1 dependency (E01), got %d", len(e02.DependsOn))
	} else if e02.DependsOn[0].EpicNum != 1 {
		t.Errorf("E02 should depend on E01, got %v", e02.DependsOn[0])
	}
}

func TestParseReadmeStatuses(t *testing.T) {
	dir := t.TempDir()
	plansDir := filepath.Join(dir, "docs", "plans")
	os.MkdirAll(plansDir, 0755)

	// Create README at repo root
	readme := `# Project
| Epic | Description | Status |
|------|-------------|--------|
| [E00](docs/plans/billing/epic-00-setup.md) | Setup | 🟢 |
| [E01](docs/plans/auth-feature/epic-01-login.md) | Login | 🟡 |
| [E02](docs/plans/api-v2/epic-02-endpoints.md) | API | 🔴 |
`
	os.WriteFile(filepath.Join(dir, "README.md"), []byte(readme), 0644)

	statuses := ParseReadmeStatuses(plansDir)

	tests := []struct {
		taskID string
		want   domain.TaskStatus
	}{
		{"billing/E00", domain.StatusComplete},
		{"auth-feature/E01", domain.StatusInProgress},
		{"api-v2/E02", domain.StatusNotStarted},
	}

	for _, tt := range tests {
		if got := statuses[tt.taskID]; got != tt.want {
			t.Errorf("statuses[%q] = %v, want %v", tt.taskID, got, tt.want)
		}
	}
}

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

func TestExtractTaskIDFromPath(t *testing.T) {
	tests := []struct {
		path       string
		wantModule string
		wantEpic   int
		wantErr    bool
	}{
		// Existing tests - updated for new behavior (directory name used as-is)
		{"/plans/technical-module/epic-05-validators.md", "technical-module", 5, false},
		{"/plans/billing-module/epic-00-setup.md", "billing-module", 0, false},
		{"/plans/some-module/00-overview.md", "", 0, true},
		{"/plans/invalid/file.txt", "", 0, true},
		// New test cases for flexible group names
		{"docs/plans/billing/epic-00-setup.md", "billing", 0, false},
		{"docs/plans/auth-subsystem/epic-01-login.md", "auth-subsystem", 1, false},
		{"docs/plans/payment-feature/epic-02-checkout.md", "payment-feature", 2, false},
		{"docs/plans/api-v2-migration/epic-00-prep.md", "api-v2-migration", 0, false},
		{"docs/plans/technical-module/epic-05-validators.md", "technical-module", 5, false},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			tid, err := ExtractTaskIDFromPath(tt.path)
			if (err != nil) != tt.wantErr {
				t.Errorf("error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if err == nil {
				if tid.Module != tt.wantModule {
					t.Errorf("Module = %q, want %q", tid.Module, tt.wantModule)
				}
				if tid.EpicNum != tt.wantEpic {
					t.Errorf("EpicNum = %d, want %d", tid.EpicNum, tt.wantEpic)
				}
			}
		})
	}
}
