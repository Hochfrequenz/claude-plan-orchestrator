package taskstore

import (
	"testing"
	"time"

	"github.com/hochfrequenz/claude-plan-orchestrator/internal/domain"
)

func TestStore_CreateAndGetTask(t *testing.T) {
	store, err := New(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	task := &domain.Task{
		ID:          domain.TaskID{Module: "technical", EpicNum: 5},
		Title:       "Validators",
		Description: "Implement validators",
		Status:      domain.StatusNotStarted,
		Priority:    domain.PriorityHigh,
		DependsOn:   []domain.TaskID{{Module: "technical", EpicNum: 4}},
		NeedsReview: true,
		FilePath:    "/path/to/epic.md",
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}

	if err := store.UpsertTask(task); err != nil {
		t.Fatal(err)
	}

	got, err := store.GetTask("technical/E05")
	if err != nil {
		t.Fatal(err)
	}

	if got.Title != task.Title {
		t.Errorf("Title = %q, want %q", got.Title, task.Title)
	}
	if got.Priority != task.Priority {
		t.Errorf("Priority = %q, want %q", got.Priority, task.Priority)
	}
	if len(got.DependsOn) != 1 {
		t.Errorf("DependsOn count = %d, want 1", len(got.DependsOn))
	}
}

func TestStore_ListTasks(t *testing.T) {
	store, err := New(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	tasks := []*domain.Task{
		{ID: domain.TaskID{Module: "technical", EpicNum: 0}, Title: "Setup", Status: domain.StatusComplete, FilePath: "/a"},
		{ID: domain.TaskID{Module: "technical", EpicNum: 1}, Title: "Core", Status: domain.StatusNotStarted, FilePath: "/b"},
		{ID: domain.TaskID{Module: "billing", EpicNum: 0}, Title: "Setup", Status: domain.StatusNotStarted, FilePath: "/c"},
	}

	for _, task := range tasks {
		task.CreatedAt = time.Now()
		task.UpdatedAt = time.Now()
		if err := store.UpsertTask(task); err != nil {
			t.Fatal(err)
		}
	}

	// List all
	all, err := store.ListTasks(ListOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 3 {
		t.Errorf("All tasks count = %d, want 3", len(all))
	}

	// Filter by module
	techTasks, err := store.ListTasks(ListOptions{Module: "technical"})
	if err != nil {
		t.Fatal(err)
	}
	if len(techTasks) != 2 {
		t.Errorf("Technical tasks count = %d, want 2", len(techTasks))
	}

	// Filter by status
	notStarted, err := store.ListTasks(ListOptions{Status: domain.StatusNotStarted})
	if err != nil {
		t.Fatal(err)
	}
	if len(notStarted) != 2 {
		t.Errorf("Not started count = %d, want 2", len(notStarted))
	}
}

func TestStore_UpdateTaskStatus(t *testing.T) {
	store, err := New(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	task := &domain.Task{
		ID:        domain.TaskID{Module: "technical", EpicNum: 0},
		Title:     "Setup",
		Status:    domain.StatusNotStarted,
		FilePath:  "/a",
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	store.UpsertTask(task)

	if err := store.UpdateTaskStatus("technical/E00", domain.StatusInProgress); err != nil {
		t.Fatal(err)
	}

	got, _ := store.GetTask("technical/E00")
	if got.Status != domain.StatusInProgress {
		t.Errorf("Status = %q, want in_progress", got.Status)
	}
}

func TestGroupPrioritiesTableExists(t *testing.T) {
	store, err := New(":memory:")
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer store.Close()

	// Query the table to verify it exists
	var count int
	err = store.db.QueryRow("SELECT COUNT(*) FROM group_priorities").Scan(&count)
	if err != nil {
		t.Errorf("group_priorities table does not exist: %v", err)
	}
}

func TestGetGroupPriorities(t *testing.T) {
	store, err := New(":memory:")
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer store.Close()

	// Insert some test data
	store.db.Exec("INSERT INTO group_priorities (group_name, priority) VALUES ('auth', 0)")
	store.db.Exec("INSERT INTO group_priorities (group_name, priority) VALUES ('billing', 1)")
	store.db.Exec("INSERT INTO group_priorities (group_name, priority) VALUES ('analytics', 2)")

	priorities, err := store.GetGroupPriorities()
	if err != nil {
		t.Fatalf("GetGroupPriorities() error = %v", err)
	}

	if len(priorities) != 3 {
		t.Errorf("len(priorities) = %d, want 3", len(priorities))
	}
	if priorities["auth"] != 0 {
		t.Errorf("priorities[auth] = %d, want 0", priorities["auth"])
	}
	if priorities["billing"] != 1 {
		t.Errorf("priorities[billing] = %d, want 1", priorities["billing"])
	}
	if priorities["analytics"] != 2 {
		t.Errorf("priorities[analytics] = %d, want 2", priorities["analytics"])
	}
}

func TestSetGroupPriority(t *testing.T) {
	store, err := New(":memory:")
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer store.Close()

	// Set priority for new group
	if err := store.SetGroupPriority("auth", 0); err != nil {
		t.Fatalf("SetGroupPriority() error = %v", err)
	}

	// Update existing group priority
	if err := store.SetGroupPriority("auth", 1); err != nil {
		t.Fatalf("SetGroupPriority() update error = %v", err)
	}

	// Verify the update
	priorities, _ := store.GetGroupPriorities()
	if priorities["auth"] != 1 {
		t.Errorf("priorities[auth] = %d, want 1", priorities["auth"])
	}
}

func TestRemoveGroupPriority(t *testing.T) {
	store, err := New(":memory:")
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer store.Close()

	// Add a group
	store.SetGroupPriority("auth", 1)

	// Remove it
	if err := store.RemoveGroupPriority("auth"); err != nil {
		t.Fatalf("RemoveGroupPriority() error = %v", err)
	}

	// Verify removal
	priorities, _ := store.GetGroupPriorities()
	if _, exists := priorities["auth"]; exists {
		t.Errorf("auth group should be removed")
	}
}

func TestGetGroupsWithTaskCounts(t *testing.T) {
	store, err := New(":memory:")
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer store.Close()

	// Add tasks for different groups
	now := time.Now()
	store.UpsertTask(&domain.Task{
		ID:        domain.TaskID{Module: "auth", EpicNum: 0},
		Title:     "Setup",
		Status:    domain.StatusComplete,
		FilePath:  "test.md",
		CreatedAt: now,
		UpdatedAt: now,
	})
	store.UpsertTask(&domain.Task{
		ID:        domain.TaskID{Module: "auth", EpicNum: 1},
		Title:     "Login",
		Status:    domain.StatusNotStarted,
		FilePath:  "test.md",
		CreatedAt: now,
		UpdatedAt: now,
	})
	store.UpsertTask(&domain.Task{
		ID:        domain.TaskID{Module: "billing", EpicNum: 0},
		Title:     "Setup",
		Status:    domain.StatusNotStarted,
		FilePath:  "test.md",
		CreatedAt: now,
		UpdatedAt: now,
	})

	// Set priority for auth
	store.SetGroupPriority("auth", 0)

	stats, err := store.GetGroupsWithTaskCounts()
	if err != nil {
		t.Fatalf("GetGroupsWithTaskCounts() error = %v", err)
	}

	if len(stats) != 2 {
		t.Errorf("len(stats) = %d, want 2", len(stats))
	}

	// Find auth group
	var authStats *GroupStats
	for _, s := range stats {
		if s.Name == "auth" {
			authStats = &s
			break
		}
	}

	if authStats == nil {
		t.Fatal("auth group not found")
	}
	if authStats.Total != 2 {
		t.Errorf("auth.Total = %d, want 2", authStats.Total)
	}
	if authStats.Completed != 1 {
		t.Errorf("auth.Completed = %d, want 1", authStats.Completed)
	}
	if authStats.Priority != 0 {
		t.Errorf("auth.Priority = %d, want 0", authStats.Priority)
	}
}
