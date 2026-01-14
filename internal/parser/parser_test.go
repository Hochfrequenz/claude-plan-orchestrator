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
	epicPath := filepath.Join(dir, "technical-module", "epic-05-validators.md")
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

func TestParseModuleDir(t *testing.T) {
	dir := t.TempDir()
	moduleDir := filepath.Join(dir, "technical-module")
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
	moduleDir := filepath.Join(dir, "pm-tool-module")
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

func TestExtractTaskIDFromPath(t *testing.T) {
	tests := []struct {
		path       string
		wantModule string
		wantEpic   int
		wantErr    bool
	}{
		{"/plans/technical-module/epic-05-validators.md", "technical", 5, false},
		{"/plans/billing-module/epic-00-setup.md", "billing", 0, false},
		{"/plans/some-module/00-overview.md", "", 0, true},
		{"/plans/invalid/file.txt", "", 0, true},
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
