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
