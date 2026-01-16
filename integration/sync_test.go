//go:build integration

package integration

import (
	"testing"

	"github.com/hochfrequenz/claude-plan-orchestrator/internal/domain"
	"github.com/hochfrequenz/claude-plan-orchestrator/internal/parser"
	"github.com/hochfrequenz/claude-plan-orchestrator/internal/scheduler"
	"github.com/hochfrequenz/claude-plan-orchestrator/internal/taskstore"
)

// TestSyncFlow_ParseToStore tests the full sync pipeline:
// markdown files -> parser -> taskstore
func TestSyncFlow_ParseToStore(t *testing.T) {
	plansDir := SamplePlansDir(t)
	dbPath := TempDBPath(t)

	// Step 1: Parse plans directory
	tasks, err := parser.ParsePlansDir(plansDir)
	if err != nil {
		t.Fatalf("ParsePlansDir failed: %v", err)
	}

	// Verify expected task count (5 epics across 2 modules)
	if len(tasks) != 5 {
		t.Errorf("Task count = %d, want 5", len(tasks))
	}

	// Step 2: Store tasks in database
	store, err := taskstore.New(dbPath)
	if err != nil {
		t.Fatalf("Failed to create store: %v", err)
	}
	defer store.Close()

	for _, task := range tasks {
		if err := store.UpsertTask(task); err != nil {
			t.Fatalf("UpsertTask failed for %s: %v", task.ID.String(), err)
		}
	}

	// Step 3: Verify tasks were stored correctly
	storedTasks, err := store.ListTasks(taskstore.ListOptions{})
	if err != nil {
		t.Fatalf("ListTasks failed: %v", err)
	}

	if len(storedTasks) != 5 {
		t.Errorf("Stored task count = %d, want 5", len(storedTasks))
	}

	// Build a map for easier lookup
	taskMap := make(map[string]*domain.Task)
	for _, task := range storedTasks {
		taskMap[task.ID.String()] = task
	}

	// Verify specific tasks
	testCases := []struct {
		id           string
		wantStatus   domain.TaskStatus
		wantPriority domain.Priority
		wantReview   bool
		wantDeps     int
	}{
		{"testing/E00", domain.StatusComplete, domain.PriorityHigh, false, 0},
		{"testing/E01", domain.StatusInProgress, domain.PriorityHigh, true, 1},
		{"testing/E02", domain.StatusNotStarted, domain.PriorityMedium, true, 2},
		{"billing/E00", domain.StatusComplete, domain.PriorityHigh, false, 0},
		{"billing/E01", domain.StatusNotStarted, domain.PriorityMedium, true, 1},
	}

	for _, tc := range testCases {
		t.Run(tc.id, func(t *testing.T) {
			task, ok := taskMap[tc.id]
			if !ok {
				t.Fatalf("Task %s not found", tc.id)
			}

			if task.Status != tc.wantStatus {
				t.Errorf("Status = %q, want %q", task.Status, tc.wantStatus)
			}

			if task.Priority != tc.wantPriority {
				t.Errorf("Priority = %q, want %q", task.Priority, tc.wantPriority)
			}

			if task.NeedsReview != tc.wantReview {
				t.Errorf("NeedsReview = %v, want %v", task.NeedsReview, tc.wantReview)
			}

			if len(task.DependsOn) != tc.wantDeps {
				t.Errorf("DependsOn count = %d, want %d", len(task.DependsOn), tc.wantDeps)
			}
		})
	}
}

// TestSyncFlow_StatusFromReadme tests that README statuses are correctly applied
func TestSyncFlow_StatusFromReadme(t *testing.T) {
	plansDir := SamplePlansDir(t)

	tasks, err := parser.ParsePlansDir(plansDir)
	if err != nil {
		t.Fatalf("ParsePlansDir failed: %v", err)
	}

	// Build status map
	statusMap := make(map[string]domain.TaskStatus)
	for _, task := range tasks {
		statusMap[task.ID.String()] = task.Status
	}

	// Verify statuses from README
	// green_circle = complete, yellow_circle = in_progress, red_circle = not_started
	expectedStatuses := map[string]domain.TaskStatus{
		"testing/E00": domain.StatusComplete,   // green_circle
		"testing/E01": domain.StatusInProgress, // yellow_circle
		"testing/E02": domain.StatusNotStarted, // red_circle
		"billing/E00": domain.StatusComplete,   // green_circle
		"billing/E01": domain.StatusNotStarted, // red_circle
	}

	for id, expected := range expectedStatuses {
		if got := statusMap[id]; got != expected {
			t.Errorf("Task %s: status = %q, want %q", id, got, expected)
		}
	}
}

// TestSyncFlow_DependencyResolution tests that dependencies are correctly parsed
func TestSyncFlow_DependencyResolution(t *testing.T) {
	plansDir := SamplePlansDir(t)
	dbPath := TempDBPath(t)

	// Parse and store
	tasks, err := parser.ParsePlansDir(plansDir)
	if err != nil {
		t.Fatalf("ParsePlansDir failed: %v", err)
	}

	store, err := taskstore.New(dbPath)
	if err != nil {
		t.Fatalf("Failed to create store: %v", err)
	}
	defer store.Close()

	for _, task := range tasks {
		store.UpsertTask(task)
	}

	// Load tasks and run scheduler
	allTasks, _ := store.ListTasks(taskstore.ListOptions{})
	completed, _ := store.GetCompletedTaskIDs()

	sched := scheduler.New(allTasks, completed)

	// Get ready tasks - should be tasks with all dependencies satisfied
	ready := sched.GetReadyTasks(10)

	// testing/E00 and billing/E00 are complete
	// testing/E01 depends on testing/E00 (complete) -> should be ready (but it's in_progress)
	// testing/E02 depends on testing/E01 (in_progress) and billing/E00 (complete) -> not ready
	// billing/E01 depends on billing/E00 (complete) -> should be ready

	// Ready tasks should be those not complete and with satisfied deps
	readyIDs := make(map[string]bool)
	for _, task := range ready {
		readyIDs[task.ID.String()] = true
	}

	// billing/E01 should be ready (not started, deps satisfied)
	if !readyIDs["billing/E01"] {
		t.Error("billing/E01 should be ready (deps satisfied)")
	}

	// testing/E02 should NOT be ready (depends on testing/E01 which is in_progress)
	if readyIDs["testing/E02"] {
		t.Error("testing/E02 should NOT be ready (testing/E01 not complete)")
	}
}

// TestSyncFlow_ModuleFiltering tests filtering tasks by module
func TestSyncFlow_ModuleFiltering(t *testing.T) {
	plansDir := SamplePlansDir(t)
	dbPath := TempDBPath(t)

	tasks, _ := parser.ParsePlansDir(plansDir)

	store, err := taskstore.New(dbPath)
	if err != nil {
		t.Fatalf("Failed to create store: %v", err)
	}
	defer store.Close()

	for _, task := range tasks {
		store.UpsertTask(task)
	}

	// Filter by testing module
	testingTasks, err := store.ListTasks(taskstore.ListOptions{Module: "testing"})
	if err != nil {
		t.Fatalf("ListTasks failed: %v", err)
	}

	if len(testingTasks) != 3 {
		t.Errorf("Testing module task count = %d, want 3", len(testingTasks))
	}

	// Filter by billing module
	billingTasks, err := store.ListTasks(taskstore.ListOptions{Module: "billing"})
	if err != nil {
		t.Fatalf("ListTasks failed: %v", err)
	}

	if len(billingTasks) != 2 {
		t.Errorf("Billing module task count = %d, want 2", len(billingTasks))
	}

	// Filter by status
	completeTasks, err := store.ListTasks(taskstore.ListOptions{Status: domain.StatusComplete})
	if err != nil {
		t.Fatalf("ListTasks failed: %v", err)
	}

	if len(completeTasks) != 2 {
		t.Errorf("Complete task count = %d, want 2", len(completeTasks))
	}
}

// TestSyncFlow_UpsertUpdatesExisting tests that upsert correctly updates existing tasks
func TestSyncFlow_UpsertUpdatesExisting(t *testing.T) {
	dbPath := TempDBPath(t)

	store, err := taskstore.New(dbPath)
	if err != nil {
		t.Fatalf("Failed to create store: %v", err)
	}
	defer store.Close()

	// Insert initial task
	task := &domain.Task{
		ID:       domain.TaskID{Module: "testing", EpicNum: 0},
		Title:    "Original Title",
		Status:   domain.StatusNotStarted,
		Priority: domain.PriorityLow,
		FilePath: "/testing/epic-00.md",
	}
	store.UpsertTask(task)

	// Upsert with updated values
	task.Title = "Updated Title"
	task.Status = domain.StatusComplete
	task.Priority = domain.PriorityHigh
	store.UpsertTask(task)

	// Verify updates were applied
	got, _ := store.GetTask("testing/E00")

	if got.Title != "Updated Title" {
		t.Errorf("Title = %q, want 'Updated Title'", got.Title)
	}

	if got.Status != domain.StatusComplete {
		t.Errorf("Status = %q, want 'complete'", got.Status)
	}

	if got.Priority != domain.PriorityHigh {
		t.Errorf("Priority = %q, want 'high'", got.Priority)
	}
}
