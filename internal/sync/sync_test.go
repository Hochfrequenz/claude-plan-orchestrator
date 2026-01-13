package sync

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/hochfrequenz/claude-plan-orchestrator/internal/domain"
)

func TestUpdateREADMEStatus(t *testing.T) {
	dir := t.TempDir()
	readmePath := filepath.Join(dir, "README.md")

	content := `# Project

## Technical Module

| Epic | Status | Description |
|------|--------|-------------|
| E00 | 🔴 | Scaffolding |
| E01 | 🔴 | Entities |
| E02 | 🔴 | Validators |

## Billing Module

| Epic | Status | Description |
|------|--------|-------------|
| E00 | 🔴 | Setup |
`
	os.WriteFile(readmePath, []byte(content), 0644)

	syncer := New(dir)

	// Update E00 to in_progress
	err := syncer.UpdateTaskStatus(domain.TaskID{Module: "technical", EpicNum: 0}, domain.StatusInProgress)
	if err != nil {
		t.Fatal(err)
	}

	// Read back
	updated, _ := os.ReadFile(readmePath)
	if !containsString(string(updated), "| E00 | 🟡 |") {
		t.Error("E00 should be updated to 🟡")
	}

	// Update E00 to complete
	err = syncer.UpdateTaskStatus(domain.TaskID{Module: "technical", EpicNum: 0}, domain.StatusComplete)
	if err != nil {
		t.Fatal(err)
	}

	updated, _ = os.ReadFile(readmePath)
	if !containsString(string(updated), "| E00 | 🟢 |") {
		t.Error("E00 should be updated to 🟢")
	}
}

func TestUpdateEpicFile(t *testing.T) {
	dir := t.TempDir()
	moduleDir := filepath.Join(dir, "technical-module")
	os.MkdirAll(moduleDir, 0755)

	epicPath := filepath.Join(moduleDir, "epic-05-validators.md")
	content := `# Epic 05: Validators

Implement input validation.
`
	os.WriteFile(epicPath, []byte(content), 0644)

	syncer := New(dir)
	err := syncer.UpdateEpicStatus(epicPath, domain.StatusComplete, 142, "2026-01-12")
	if err != nil {
		t.Fatal(err)
	}

	updated, _ := os.ReadFile(epicPath)
	if !containsString(string(updated), "status=complete") {
		t.Error("Epic should have status comment")
	}
	if !containsString(string(updated), "pr=#142") {
		t.Error("Epic should have PR reference")
	}
}

func TestParseStatusEmoji(t *testing.T) {
	tests := []struct {
		status domain.TaskStatus
		want   string
	}{
		{domain.StatusNotStarted, "🔴"},
		{domain.StatusInProgress, "🟡"},
		{domain.StatusComplete, "🟢"},
	}

	for _, tt := range tests {
		got := StatusEmoji(tt.status)
		if got != tt.want {
			t.Errorf("StatusEmoji(%s) = %s, want %s", tt.status, got, tt.want)
		}
	}
}

func TestUpdateEpicFrontmatter(t *testing.T) {
	dir := t.TempDir()
	syncer := New(dir)

	t.Run("updates existing status", func(t *testing.T) {
		epicPath := filepath.Join(dir, "epic-01.md")
		content := `---
status: todo
priority: high
---

# Epic 01: Setup
`
		os.WriteFile(epicPath, []byte(content), 0644)

		err := syncer.UpdateEpicFrontmatter(epicPath, domain.StatusInProgress)
		if err != nil {
			t.Fatal(err)
		}

		updated, _ := os.ReadFile(epicPath)
		if !containsString(string(updated), "status: in_progress") {
			t.Error("Status should be updated to in_progress")
		}
		if !containsString(string(updated), "priority: high") {
			t.Error("Other frontmatter fields should be preserved")
		}
	})

	t.Run("adds status if missing", func(t *testing.T) {
		epicPath := filepath.Join(dir, "epic-02.md")
		content := `---
priority: low
---

# Epic 02: Feature
`
		os.WriteFile(epicPath, []byte(content), 0644)

		err := syncer.UpdateEpicFrontmatter(epicPath, domain.StatusComplete)
		if err != nil {
			t.Fatal(err)
		}

		updated, _ := os.ReadFile(epicPath)
		if !containsString(string(updated), "status: complete") {
			t.Error("Status should be added")
		}
	})

	t.Run("errors on missing frontmatter", func(t *testing.T) {
		epicPath := filepath.Join(dir, "epic-03.md")
		content := `# Epic 03: No Frontmatter
`
		os.WriteFile(epicPath, []byte(content), 0644)

		err := syncer.UpdateEpicFrontmatter(epicPath, domain.StatusInProgress)
		if err == nil {
			t.Error("Should error on missing frontmatter")
		}
	})
}

func containsString(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
