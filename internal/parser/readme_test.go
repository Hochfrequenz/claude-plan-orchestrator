package parser

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/hochfrequenz/claude-plan-orchestrator/internal/domain"
)

func TestParseReadmeStatuses_EnergyERP(t *testing.T) {
	// Test against the actual energyerp README.md format
	readmeContent := `# EnergyERP

## Module Progress

Status: 🔴 Not Started | 🟡 In Progress | 🟢 Complete

### Technical Module

| Epic | Description | Status |
|------|-------------|:------:|
| [E00](docs/plans/technical-module/epic-00-scaffolding.md) | Project Scaffolding | 🟢 |
| [E01](docs/plans/technical-module/epic-01-supporting-entities.md) | Supporting Entities | 🟢 |
| [E02](docs/plans/technical-module/epic-02-physical-hierarchy.md) | Physical Hierarchy | 🟢 |
| [E05](docs/plans/technical-module/epic-05-validators.md) | ID Validators | 🔴 |
| [E06](docs/plans/technical-module/epic-06-formula-engine.md) | Formula Engine | 🟡 |

### SubLedger Module

| Epic | Description | Status |
|------|-------------|:------:|
| [E1](docs/plans/2026-01-05-subledger-epic-1-core-foundation.md) | Core Foundation | 🟢 |
| [E2](docs/plans/2026-01-05-subledger-epic-2-payment-processing.md) | Payment Processing | 🟢 |
| [E3](docs/plans/2026-01-05-subledger-epic-3-dunning.md) | Dunning Process | 🔴 |

### Customer Module

| Epic | Description | Status |
|------|-------------|:------:|
| [E00](docs/plans/customer-module/epic-00-scaffolding.md) | Database Migrations | 🟢 |
| [E01](docs/plans/customer-module/epic-01-pii-vault.md) | PII Vault | 🟢 |
| [E05](docs/plans/customer-module/epic-05-http-api.md) | HTTP API Wiring | 🔴 |

### Task Module

| Epic | Description | Status |
|------|-------------|:------:|
| [E00](docs/plans/2026-01-07-task-module-epic-0-scaffolding.md) | Module Scaffolding | 🟢 |
| [E01](docs/plans/2026-01-07-task-module-epic-1-core-task-management.md) | Core Task Management | 🟢 |
| [E03](docs/plans/task-module/2026-01-07-task-module-epic-3-task-types-sla.md) | Task Types and SLA | 🟢 |

### Testing Strategy

| Epic | Description | Status |
|------|-------------|:------:|
| [E1](docs/plans/testing-strategy/epic-01-core-infrastructure.md) | Core Infrastructure | 🟢 |
| [E2](docs/plans/testing-strategy/epic-02-test-data-factories.md) | Test Data Factories | 🟢 |
| [E6](docs/plans/testing-strategy/epic-06-cicd-integration.md) | CI/CD Integration | 🔴 |
`

	// Create temp dir and README
	tmpDir := t.TempDir()
	plansDir := filepath.Join(tmpDir, "docs", "plans")
	if err := os.MkdirAll(plansDir, 0755); err != nil {
		t.Fatalf("Failed to create plans dir: %v", err)
	}

	readmePath := filepath.Join(tmpDir, "README.md")
	if err := os.WriteFile(readmePath, []byte(readmeContent), 0644); err != nil {
		t.Fatalf("Failed to write README: %v", err)
	}

	// Parse statuses
	statuses := ParseReadmeStatuses(plansDir)

	// Expected results - modules should match how ExtractTaskIDFromPath would parse them
	expected := map[string]domain.TaskStatus{
		// Technical module (standard xxx-module/epic-NN-... pattern)
		"technical/E00": domain.StatusComplete,
		"technical/E01": domain.StatusComplete,
		"technical/E02": domain.StatusComplete,
		"technical/E05": domain.StatusNotStarted,
		"technical/E06": domain.StatusInProgress,

		// SubLedger module (YYYY-MM-DD-xxx-epic-N-... pattern)
		"subledger/E01": domain.StatusComplete,
		"subledger/E02": domain.StatusComplete,
		"subledger/E03": domain.StatusNotStarted,

		// Customer module (standard xxx-module/epic-NN-... pattern)
		"customer/E00": domain.StatusComplete,
		"customer/E01": domain.StatusComplete,
		"customer/E05": domain.StatusNotStarted,

		// Task module (YYYY-MM-DD-task-module-epic-N-... pattern -> task)
		"task/E00": domain.StatusComplete,
		"task/E01": domain.StatusComplete,
		"task/E03": domain.StatusComplete, // nested path

		// Testing strategy (non-module directory)
		"testing-strategy/E01": domain.StatusComplete,
		"testing-strategy/E02": domain.StatusComplete,
		"testing-strategy/E06": domain.StatusNotStarted,
	}

	// Check we got results
	if len(statuses) == 0 {
		t.Fatal("ParseReadmeStatuses returned no statuses")
	}

	t.Logf("Parsed %d statuses:", len(statuses))
	for k, v := range statuses {
		t.Logf("  %s: %v", k, v)
	}

	// Check each expected status
	for taskID, expectedStatus := range expected {
		got, ok := statuses[taskID]
		if !ok {
			t.Errorf("Missing status for %s", taskID)
			continue
		}
		if got != expectedStatus {
			t.Errorf("Status for %s = %v, want %v", taskID, got, expectedStatus)
		}
	}

	// Check we didn't get unexpected statuses
	for taskID := range statuses {
		if _, ok := expected[taskID]; !ok {
			t.Errorf("Unexpected status for %s", taskID)
		}
	}
}

func TestParseReadmeStatuses_IndividualPatterns(t *testing.T) {
	tests := []struct {
		name       string
		line       string
		wantModule string
		wantEpic   int
		wantStatus domain.TaskStatus
	}{
		{
			name:       "standard xxx-module pattern",
			line:       "| [E00](docs/plans/technical-module/epic-00-scaffolding.md) | Project Scaffolding | 🟢 |",
			wantModule: "technical",
			wantEpic:   0,
			wantStatus: domain.StatusComplete,
		},
		{
			name:       "standard xxx-module pattern with E05",
			line:       "| [E05](docs/plans/customer-module/epic-05-http-api.md) | HTTP API Wiring | 🔴 |",
			wantModule: "customer",
			wantEpic:   5,
			wantStatus: domain.StatusNotStarted,
		},
		{
			name:       "date prefix subledger",
			line:       "| [E1](docs/plans/2026-01-05-subledger-epic-1-core-foundation.md) | Core Foundation | 🟢 |",
			wantModule: "subledger",
			wantEpic:   1,
			wantStatus: domain.StatusComplete,
		},
		{
			name:       "date prefix task-module",
			line:       "| [E00](docs/plans/2026-01-07-task-module-epic-0-scaffolding.md) | Module Scaffolding | 🟢 |",
			wantModule: "task",
			wantEpic:   0,
			wantStatus: domain.StatusComplete,
		},
		{
			name:       "nested task-module with date prefix file",
			line:       "| [E03](docs/plans/task-module/2026-01-07-task-module-epic-3-task-types-sla.md) | Task Types and SLA | 🟢 |",
			wantModule: "task",
			wantEpic:   3,
			wantStatus: domain.StatusComplete,
		},
		{
			name:       "in progress status",
			line:       "| [E06](docs/plans/technical-module/epic-06-formula-engine.md) | Formula Engine | 🟡 |",
			wantModule: "technical",
			wantEpic:   6,
			wantStatus: domain.StatusInProgress,
		},
		{
			name:       "testing-strategy non-module directory",
			line:       "| [E1](docs/plans/testing-strategy/epic-01-core-infrastructure.md) | Core Infrastructure | 🟢 |",
			wantModule: "testing-strategy",
			wantEpic:   1,
			wantStatus: domain.StatusComplete,
		},
		{
			name:       "workflow-module standard",
			line:       "| [E00](docs/plans/workflow-module/epic-00-scaffolding.md) | Module Scaffolding | 🟢 |",
			wantModule: "workflow",
			wantEpic:   0,
			wantStatus: domain.StatusComplete,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create temp README with just this line
			tmpDir := t.TempDir()
			plansDir := filepath.Join(tmpDir, "docs", "plans")
			if err := os.MkdirAll(plansDir, 0755); err != nil {
				t.Fatalf("Failed to create plans dir: %v", err)
			}

			readmePath := filepath.Join(tmpDir, "README.md")
			if err := os.WriteFile(readmePath, []byte(tt.line), 0644); err != nil {
				t.Fatalf("Failed to write README: %v", err)
			}

			statuses := ParseReadmeStatuses(plansDir)

			expectedKey := domain.TaskID{Module: tt.wantModule, EpicNum: tt.wantEpic}.String()

			if len(statuses) == 0 {
				t.Errorf("No statuses parsed from line: %s", tt.line)
				return
			}

			t.Logf("Parsed statuses: %v", statuses)

			got, ok := statuses[expectedKey]
			if !ok {
				t.Errorf("Expected status for %s not found, got keys: %v", expectedKey, keys(statuses))
				return
			}

			if got != tt.wantStatus {
				t.Errorf("Status = %v, want %v", got, tt.wantStatus)
			}
		})
	}
}

func TestParseReadmeStatuses_RealEnergyERPReadme(t *testing.T) {
	// Test against the actual file if it exists
	realReadmePath := "/home/claude/github/energyerp/README.md"
	if _, err := os.Stat(realReadmePath); os.IsNotExist(err) {
		t.Skip("energyerp README.md not found, skipping real file test")
	}

	// Use the actual path
	plansDir := "/home/claude/github/energyerp/docs/plans"

	statuses := ParseReadmeStatuses(plansDir)

	t.Logf("Parsed %d statuses from real README:", len(statuses))
	for k, v := range statuses {
		t.Logf("  %s: %v", k, v)
	}

	// Should have parsed at least some statuses
	if len(statuses) == 0 {
		t.Error("Failed to parse any statuses from real README")
	}

	// Check some known statuses from the README
	expectedComplete := []string{
		"technical/E00", // Project Scaffolding
		"technical/E01", // Supporting Entities
		"subledger/E01", // Core Foundation
		"customer/E00",  // Database Migrations
	}

	for _, taskID := range expectedComplete {
		if status, ok := statuses[taskID]; !ok {
			t.Errorf("Missing status for %s", taskID)
		} else if status != domain.StatusComplete {
			t.Errorf("Status for %s = %v, want Complete", taskID, status)
		}
	}
}

func keys(m map[string]domain.TaskStatus) []string {
	result := make([]string, 0, len(m))
	for k := range m {
		result = append(result, k)
	}
	return result
}
