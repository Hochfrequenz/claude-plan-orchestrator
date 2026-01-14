package sync

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hochfrequenz/claude-plan-orchestrator/internal/domain"
)

func TestUpdateREADMEStatus(t *testing.T) {
	// Create directory structure: {root}/docs/plans/
	root := t.TempDir()
	plansDir := filepath.Join(root, "docs", "plans")
	os.MkdirAll(plansDir, 0755)

	// README.md is at project root
	readmePath := filepath.Join(root, "README.md")

	// Use the actual format from energyerp README with links
	content := `# Project

### Technical Module

| Epic | Description | Status |
|------|-------------|:------:|
| [E00](docs/plans/technical-module/epic-00.md) | Scaffolding | 🔴 |
| [E01](docs/plans/technical-module/epic-01.md) | Entities | 🔴 |
| [E02](docs/plans/technical-module/epic-02.md) | Validators | 🔴 |

### Billing Module

| Epic | Description | Status |
|------|-------------|:------:|
| [E00](docs/plans/billing-module/epic-00.md) | Setup | 🔴 |
`
	os.WriteFile(readmePath, []byte(content), 0644)

	syncer := New(plansDir)

	// Update E00 to in_progress
	err := syncer.UpdateTaskStatus(domain.TaskID{Module: "technical", EpicNum: 0}, domain.StatusInProgress)
	if err != nil {
		t.Fatal(err)
	}

	// Read back
	updated, _ := os.ReadFile(readmePath)
	if !containsString(string(updated), "| 🟡 |") {
		t.Errorf("E00 should be updated to 🟡, got:\n%s", string(updated))
	}

	// Update E00 to complete
	err = syncer.UpdateTaskStatus(domain.TaskID{Module: "technical", EpicNum: 0}, domain.StatusComplete)
	if err != nil {
		t.Fatal(err)
	}

	updated, _ = os.ReadFile(readmePath)
	if !containsString(string(updated), "| 🟢 |") {
		t.Errorf("E00 should be updated to 🟢, got:\n%s", string(updated))
	}

	// Verify billing module E00 is still 🔴
	lines := strings.Split(string(updated), "\n")
	inBilling := false
	for _, line := range lines {
		if strings.Contains(strings.ToLower(line), "billing") {
			inBilling = true
		}
		if inBilling && strings.Contains(line, "[E00]") {
			if !strings.Contains(line, "🔴") {
				t.Error("Billing E00 should still be 🔴")
			}
			break
		}
	}
}

func TestUpdateEpicFile(t *testing.T) {
	// Create directory structure: {root}/docs/plans/technical-module/
	root := t.TempDir()
	plansDir := filepath.Join(root, "docs", "plans")
	moduleDir := filepath.Join(plansDir, "technical-module")
	os.MkdirAll(moduleDir, 0755)

	epicPath := filepath.Join(moduleDir, "epic-05-validators.md")
	content := `# Epic 05: Validators

Implement input validation.
`
	os.WriteFile(epicPath, []byte(content), 0644)

	syncer := New(plansDir)
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
	// Create directory structure: {root}/docs/plans/
	root := t.TempDir()
	plansDir := filepath.Join(root, "docs", "plans")
	os.MkdirAll(plansDir, 0755)
	syncer := New(plansDir)

	t.Run("updates existing status", func(t *testing.T) {
		epicPath := filepath.Join(plansDir, "epic-01.md")
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
		epicPath := filepath.Join(plansDir, "epic-02.md")
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

	t.Run("adds frontmatter when missing", func(t *testing.T) {
		epicPath := filepath.Join(plansDir, "epic-03.md")
		content := `# Epic 03: No Frontmatter
`
		os.WriteFile(epicPath, []byte(content), 0644)

		err := syncer.UpdateEpicFrontmatter(epicPath, domain.StatusInProgress)
		if err != nil {
			t.Fatal(err)
		}

		updated, _ := os.ReadFile(epicPath)
		updatedStr := string(updated)
		if !containsString(updatedStr, "status: in_progress") {
			t.Error("Status should be added in new frontmatter")
		}
		if !containsString(updatedStr, "# Epic 03: No Frontmatter") {
			t.Error("Original content should be preserved")
		}
		// Verify frontmatter structure
		if !strings.HasPrefix(updatedStr, "---\n") {
			t.Error("Should start with frontmatter delimiter")
		}
		if !containsString(updatedStr, "\n---\n") {
			t.Error("Should have closing frontmatter delimiter")
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
