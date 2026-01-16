package sync

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hochfrequenz/claude-plan-orchestrator/internal/domain"
	"github.com/hochfrequenz/claude-plan-orchestrator/internal/taskstore"
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

func TestUpdateREADMEStatus_NoReadme(t *testing.T) {
	// Test that UpdateTaskStatus gracefully handles missing README.md
	root := t.TempDir()
	plansDir := filepath.Join(root, "docs", "plans")
	os.MkdirAll(plansDir, 0755)

	// Don't create README.md - it should not exist
	syncer := New(plansDir)

	// UpdateTaskStatus should return nil (no-op) when README.md doesn't exist
	err := syncer.UpdateTaskStatus(domain.TaskID{Module: "technical", EpicNum: 0}, domain.StatusInProgress)
	if err != nil {
		t.Errorf("UpdateTaskStatus should succeed when README.md is missing, got error: %v", err)
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

func TestTwoWaySync_DetectsConflicts(t *testing.T) {
	// Setup: create temp dirs and files
	root := t.TempDir()
	plansDir := filepath.Join(root, "docs", "plans")
	moduleDir := filepath.Join(plansDir, "technical")
	os.MkdirAll(moduleDir, 0755)

	// Create epic file with status: complete
	epicPath := filepath.Join(moduleDir, "epic-05-validators.md")
	content := `---
status: complete
---

# E05: Validators
`
	os.WriteFile(epicPath, []byte(content), 0644)

	// Create in-memory store with status: in_progress
	store, _ := taskstore.New(":memory:")
	defer store.Close()
	store.UpsertTask(&domain.Task{
		ID:       domain.TaskID{Module: "technical", EpicNum: 5},
		Title:    "Validators",
		Status:   domain.StatusInProgress,
		FilePath: epicPath,
	})

	syncer := New(plansDir)
	result, err := syncer.TwoWaySync(store)
	if err != nil {
		t.Fatal(err)
	}

	if len(result.Conflicts) != 1 {
		t.Fatalf("expected 1 conflict, got %d", len(result.Conflicts))
	}

	c := result.Conflicts[0]
	if c.TaskID != "technical/E05" {
		t.Errorf("expected conflict for technical/E05, got %s", c.TaskID)
	}
	if c.DBStatus != "in_progress" {
		t.Errorf("expected DB status in_progress, got %s", c.DBStatus)
	}
	if c.MarkdownStatus != "complete" {
		t.Errorf("expected markdown status complete, got %s", c.MarkdownStatus)
	}
}

func TestResolveConflicts_UseDB(t *testing.T) {
	root := t.TempDir()
	plansDir := filepath.Join(root, "docs", "plans")
	moduleDir := filepath.Join(plansDir, "technical-module")
	os.MkdirAll(moduleDir, 0755)

	// Epic file says complete (use parser-expected naming: epic-NN-*.md)
	epicPath := filepath.Join(moduleDir, "epic-05-validators.md")
	content := `---
status: complete
---

# E05: Validators
`
	os.WriteFile(epicPath, []byte(content), 0644)

	// README at project root
	readmePath := filepath.Join(root, "README.md")
	readme := `# Project

### Technical Module

| Epic | Description | Status |
|------|-------------|:------:|
| [E05](docs/plans/technical-module/epic-05-validators.md) | Validators | 🟢 |
`
	os.WriteFile(readmePath, []byte(readme), 0644)

	// DB says in_progress
	store, _ := taskstore.New(":memory:")
	defer store.Close()
	store.UpsertTask(&domain.Task{
		ID:       domain.TaskID{Module: "technical", EpicNum: 5},
		Title:    "Validators",
		Status:   domain.StatusInProgress,
		FilePath: epicPath,
	})

	syncer := New(plansDir)

	// Resolve: use DB value (in_progress)
	resolutions := map[string]string{
		"technical/E05": "db",
	}
	err := syncer.ResolveConflicts(store, resolutions)
	if err != nil {
		t.Fatal(err)
	}

	// Verify markdown was updated to in_progress
	updated, _ := os.ReadFile(epicPath)
	if !strings.Contains(string(updated), "status: in_progress") {
		t.Errorf("epic should have status: in_progress, got:\n%s", string(updated))
	}

	// Verify README emoji was updated
	updatedReadme, _ := os.ReadFile(readmePath)
	if !strings.Contains(string(updatedReadme), "🟡") {
		t.Errorf("README should have 🟡, got:\n%s", string(updatedReadme))
	}
}

func TestResolveConflicts_UseMarkdown(t *testing.T) {
	root := t.TempDir()
	plansDir := filepath.Join(root, "docs", "plans")
	moduleDir := filepath.Join(plansDir, "technical-module")
	os.MkdirAll(moduleDir, 0755)

	// Epic file says complete (use parser-expected naming: epic-NN-*.md)
	epicPath := filepath.Join(moduleDir, "epic-05-validators.md")
	content := `---
status: complete
---

# E05: Validators
`
	os.WriteFile(epicPath, []byte(content), 0644)

	// DB says in_progress
	store, _ := taskstore.New(":memory:")
	defer store.Close()
	store.UpsertTask(&domain.Task{
		ID:       domain.TaskID{Module: "technical-module", EpicNum: 5},
		Title:    "Validators",
		Status:   domain.StatusInProgress,
		FilePath: epicPath,
	})

	syncer := New(plansDir)

	// Resolve: use markdown value (complete)
	resolutions := map[string]string{
		"technical-module/E05": "markdown",
	}
	err := syncer.ResolveConflicts(store, resolutions)
	if err != nil {
		t.Fatal(err)
	}

	// Verify DB was updated to complete
	task, _ := store.GetTask("technical-module/E05")
	if task.Status != domain.StatusComplete {
		t.Errorf("DB should have status complete, got %s", task.Status)
	}
}

func TestResolveConflicts_InvalidResolution(t *testing.T) {
	root := t.TempDir()
	plansDir := filepath.Join(root, "docs", "plans")
	moduleDir := filepath.Join(plansDir, "technical-module")
	os.MkdirAll(moduleDir, 0755)

	epicPath := filepath.Join(moduleDir, "epic-05-validators.md")
	content := `---
status: complete
---

# E05: Validators
`
	os.WriteFile(epicPath, []byte(content), 0644)

	store, _ := taskstore.New(":memory:")
	defer store.Close()
	store.UpsertTask(&domain.Task{
		ID:       domain.TaskID{Module: "technical-module", EpicNum: 5},
		Title:    "Validators",
		Status:   domain.StatusInProgress,
		FilePath: epicPath,
	})

	syncer := New(plansDir)

	// Try with invalid resolution value
	resolutions := map[string]string{
		"technical-module/E05": "invalid",
	}
	err := syncer.ResolveConflicts(store, resolutions)
	if err == nil {
		t.Error("expected error for invalid resolution")
	}
	if !strings.Contains(err.Error(), "invalid resolution") {
		t.Errorf("expected 'invalid resolution' error, got: %v", err)
	}
}

func TestSyncMarkdownToDB(t *testing.T) {
	root := t.TempDir()
	plansDir := filepath.Join(root, "docs", "plans")
	moduleDir := filepath.Join(plansDir, "technical")
	os.MkdirAll(moduleDir, 0755)

	// Create two epic files
	epic1 := filepath.Join(moduleDir, "epic-01-setup.md")
	os.WriteFile(epic1, []byte("---\nstatus: complete\n---\n\n# E01: Setup\n"), 0644)

	epic2 := filepath.Join(moduleDir, "epic-02-feature.md")
	os.WriteFile(epic2, []byte("---\nstatus: in_progress\n---\n\n# E02: Feature\n"), 0644)

	store, _ := taskstore.New(":memory:")
	defer store.Close()

	syncer := New(plansDir)
	count, err := syncer.SyncMarkdownToDB(store)
	if err != nil {
		t.Fatal(err)
	}

	if count != 2 {
		t.Errorf("expected 2 tasks synced, got %d", count)
	}

	// Verify tasks are in DB
	task1, _ := store.GetTask("technical/E01")
	if task1 == nil || task1.Status != domain.StatusComplete {
		t.Error("E01 should be complete in DB")
	}

	task2, _ := store.GetTask("technical/E02")
	if task2 == nil || task2.Status != domain.StatusInProgress {
		t.Error("E02 should be in_progress in DB")
	}
}

func TestTwoWaySync_UpdatesDependencies(t *testing.T) {
	// This test verifies that TwoWaySync updates dependencies in the DB
	// even when the status matches (regression test for stale dependency bug)
	root := t.TempDir()
	plansDir := filepath.Join(root, "docs", "plans")
	moduleDir := filepath.Join(plansDir, "pm-tool-module")
	os.MkdirAll(moduleDir, 0755)

	// Create E01 and E02 (no E00) - this mimics the user's scenario
	epic1 := filepath.Join(moduleDir, "epic-01-foundation.md")
	os.WriteFile(epic1, []byte("---\nstatus: not_started\n---\n\n# E01: Foundation\n"), 0644)

	epic2 := filepath.Join(moduleDir, "epic-02-domain.md")
	os.WriteFile(epic2, []byte("---\nstatus: not_started\n---\n\n# E02: Domain\n"), 0644)

	// Create DB with stale dependencies (E01 depends on non-existent E00)
	store, _ := taskstore.New(":memory:")
	defer store.Close()

	// E01 incorrectly has E00 as dependency (stale data)
	store.UpsertTask(&domain.Task{
		ID:        domain.TaskID{Module: "pm-tool-module", EpicNum: 1},
		Title:     "Foundation",
		Status:    domain.StatusNotStarted,
		DependsOn: []domain.TaskID{{Module: "pm-tool-module", EpicNum: 0}}, // Stale!
		FilePath:  epic1,
	})

	// E02 depends on E01
	store.UpsertTask(&domain.Task{
		ID:        domain.TaskID{Module: "pm-tool-module", EpicNum: 2},
		Title:     "Domain",
		Status:    domain.StatusNotStarted,
		DependsOn: []domain.TaskID{{Module: "pm-tool-module", EpicNum: 1}},
		FilePath:  epic2,
	})

	syncer := New(plansDir)
	result, err := syncer.TwoWaySync(store)
	if err != nil {
		t.Fatal(err)
	}

	// Should have no conflicts (statuses match)
	if len(result.Conflicts) != 0 {
		t.Errorf("expected no conflicts, got %d", len(result.Conflicts))
	}

	// Verify E01's dependencies were updated (should be empty since E00 doesn't exist)
	task1, _ := store.GetTask("pm-tool-module/E01")
	if task1 == nil {
		t.Fatal("E01 not found in DB")
	}
	if len(task1.DependsOn) != 0 {
		t.Errorf("E01 should have no dependencies (E00 doesn't exist), got %v", task1.DependsOn)
	}

	// Verify E02 still depends on E01 (since E01 exists)
	task2, _ := store.GetTask("pm-tool-module/E02")
	if task2 == nil {
		t.Fatal("E02 not found in DB")
	}
	if len(task2.DependsOn) != 1 || task2.DependsOn[0].EpicNum != 1 {
		t.Errorf("E02 should depend on E01, got %v", task2.DependsOn)
	}
}

func TestSyncDBToMarkdown(t *testing.T) {
	root := t.TempDir()
	plansDir := filepath.Join(root, "docs", "plans")
	moduleDir := filepath.Join(plansDir, "technical")
	os.MkdirAll(moduleDir, 0755)

	// Create epic file with not_started
	epicPath := filepath.Join(moduleDir, "epic-01-setup.md")
	os.WriteFile(epicPath, []byte("---\nstatus: not_started\n---\n\n# E01: Setup\n"), 0644)

	// Create README
	readmePath := filepath.Join(root, "README.md")
	readme := `# Project

### Technical Module

| Epic | Description | Status |
|------|-------------|:------:|
| [E01](docs/plans/technical/epic-01-setup.md) | Setup | 🔴 |
`
	os.WriteFile(readmePath, []byte(readme), 0644)

	// DB has it as complete
	store, _ := taskstore.New(":memory:")
	defer store.Close()
	store.UpsertTask(&domain.Task{
		ID:       domain.TaskID{Module: "technical", EpicNum: 1},
		Title:    "Setup",
		Status:   domain.StatusComplete,
		FilePath: epicPath,
	})

	syncer := New(plansDir)
	count, err := syncer.SyncDBToMarkdown(store)
	if err != nil {
		t.Fatal(err)
	}

	if count != 1 {
		t.Errorf("expected 1 task synced, got %d", count)
	}

	// Verify epic frontmatter updated
	updated, _ := os.ReadFile(epicPath)
	if !strings.Contains(string(updated), "status: complete") {
		t.Errorf("epic should have status: complete, got:\n%s", string(updated))
	}

	// Verify README updated
	updatedReadme, _ := os.ReadFile(readmePath)
	if !strings.Contains(string(updatedReadme), "🟢") {
		t.Errorf("README should have 🟢, got:\n%s", string(updatedReadme))
	}
}
