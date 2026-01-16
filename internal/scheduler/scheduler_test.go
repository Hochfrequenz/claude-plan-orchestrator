package scheduler

import (
	"testing"

	"github.com/hochfrequenz/claude-plan-orchestrator/internal/domain"
)

func TestScheduler_GetReadyTasks(t *testing.T) {
	tasks := []*domain.Task{
		{ID: domain.TaskID{Module: "tech", EpicNum: 0}, Status: domain.StatusNotStarted, DependsOn: nil},
		{ID: domain.TaskID{Module: "tech", EpicNum: 1}, Status: domain.StatusNotStarted, DependsOn: []domain.TaskID{{Module: "tech", EpicNum: 0}}},
		{ID: domain.TaskID{Module: "tech", EpicNum: 2}, Status: domain.StatusNotStarted, DependsOn: []domain.TaskID{{Module: "tech", EpicNum: 1}}},
		{ID: domain.TaskID{Module: "billing", EpicNum: 0}, Status: domain.StatusNotStarted, DependsOn: nil},
	}
	completed := map[string]bool{}

	sched := New(tasks, completed)
	ready := sched.GetReadyTasks(10)

	// E00 from both modules should be ready
	if len(ready) != 2 {
		t.Errorf("Ready count = %d, want 2", len(ready))
	}
}

func TestScheduler_GetReadyTasks_WithCompleted(t *testing.T) {
	tasks := []*domain.Task{
		{ID: domain.TaskID{Module: "tech", EpicNum: 0}, Status: domain.StatusComplete, DependsOn: nil},
		{ID: domain.TaskID{Module: "tech", EpicNum: 1}, Status: domain.StatusNotStarted, DependsOn: []domain.TaskID{{Module: "tech", EpicNum: 0}}},
		{ID: domain.TaskID{Module: "tech", EpicNum: 2}, Status: domain.StatusNotStarted, DependsOn: []domain.TaskID{{Module: "tech", EpicNum: 1}}},
	}
	completed := map[string]bool{"tech/E00": true}

	sched := New(tasks, completed)
	ready := sched.GetReadyTasks(10)

	// Only E01 should be ready (E00 complete, E02 waiting on E01)
	if len(ready) != 1 {
		t.Errorf("Ready count = %d, want 1", len(ready))
	}
	if ready[0].ID.String() != "tech/E01" {
		t.Errorf("Ready task = %s, want tech/E01", ready[0].ID.String())
	}
}

func TestScheduler_GetReadyTasks_Priority(t *testing.T) {
	tasks := []*domain.Task{
		{ID: domain.TaskID{Module: "tech", EpicNum: 0}, Status: domain.StatusNotStarted, Priority: domain.PriorityNormal},
		{ID: domain.TaskID{Module: "billing", EpicNum: 0}, Status: domain.StatusNotStarted, Priority: domain.PriorityHigh},
		{ID: domain.TaskID{Module: "pricing", EpicNum: 0}, Status: domain.StatusNotStarted, Priority: domain.PriorityLow},
	}
	completed := map[string]bool{}

	sched := New(tasks, completed)
	ready := sched.GetReadyTasks(10)

	// High priority should come first
	if ready[0].ID.Module != "billing" {
		t.Errorf("First task module = %s, want billing (high priority)", ready[0].ID.Module)
	}
}

func TestScheduler_GetReadyTasks_Limit(t *testing.T) {
	tasks := []*domain.Task{
		{ID: domain.TaskID{Module: "tech", EpicNum: 0}, Status: domain.StatusNotStarted},
		{ID: domain.TaskID{Module: "billing", EpicNum: 0}, Status: domain.StatusNotStarted},
		{ID: domain.TaskID{Module: "pricing", EpicNum: 0}, Status: domain.StatusNotStarted},
	}
	completed := map[string]bool{}

	sched := New(tasks, completed)
	ready := sched.GetReadyTasks(2)

	if len(ready) != 2 {
		t.Errorf("Ready count = %d, want 2 (limited)", len(ready))
	}
}

func TestScheduler_DependencyDepth(t *testing.T) {
	tasks := []*domain.Task{
		{ID: domain.TaskID{Module: "tech", EpicNum: 0}, Status: domain.StatusNotStarted},
		{ID: domain.TaskID{Module: "tech", EpicNum: 1}, Status: domain.StatusNotStarted, DependsOn: []domain.TaskID{{Module: "tech", EpicNum: 0}}},
		{ID: domain.TaskID{Module: "tech", EpicNum: 2}, Status: domain.StatusNotStarted, DependsOn: []domain.TaskID{{Module: "tech", EpicNum: 1}}},
		{ID: domain.TaskID{Module: "billing", EpicNum: 0}, Status: domain.StatusNotStarted},
	}

	sched := New(tasks, map[string]bool{})

	// tech/E00 unblocks more (E01, E02) than billing/E00 (nothing)
	depth := sched.dependencyDepth("tech/E00")
	if depth != 2 {
		t.Errorf("tech/E00 depth = %d, want 2", depth)
	}

	depth = sched.dependencyDepth("billing/E00")
	if depth != 0 {
		t.Errorf("billing/E00 depth = %d, want 0", depth)
	}
}

func TestScheduler_WithGroupPriorities(t *testing.T) {
	tasks := []*domain.Task{
		{ID: domain.TaskID{Module: "auth", EpicNum: 0}, Status: domain.StatusNotStarted},
		{ID: domain.TaskID{Module: "billing", EpicNum: 0}, Status: domain.StatusNotStarted},
		{ID: domain.TaskID{Module: "analytics", EpicNum: 0}, Status: domain.StatusNotStarted},
	}
	completed := map[string]bool{}
	priorities := map[string]int{
		"auth":      0, // tier 0 - runs first
		"billing":   1, // tier 1 - runs after auth completes
		"analytics": 2, // tier 2 - runs last
	}

	sched := NewWithPriorities(tasks, completed, priorities)
	ready := sched.GetReadyTasks(10)

	// Only auth (tier 0) should be ready
	if len(ready) != 1 {
		t.Errorf("Ready count = %d, want 1", len(ready))
	}
	if ready[0].ID.Module != "auth" {
		t.Errorf("Ready task = %s, want auth/E00", ready[0].ID.String())
	}
}

func TestScheduler_GroupPriorities_TierAdvance(t *testing.T) {
	tasks := []*domain.Task{
		{ID: domain.TaskID{Module: "auth", EpicNum: 0}, Status: domain.StatusComplete},
		{ID: domain.TaskID{Module: "billing", EpicNum: 0}, Status: domain.StatusNotStarted},
		{ID: domain.TaskID{Module: "reporting", EpicNum: 0}, Status: domain.StatusNotStarted},
		{ID: domain.TaskID{Module: "analytics", EpicNum: 0}, Status: domain.StatusNotStarted},
	}
	completed := map[string]bool{"auth/E00": true}
	priorities := map[string]int{
		"auth":      0,
		"billing":   1,
		"reporting": 1, // Same tier as billing
		"analytics": 2,
	}

	sched := NewWithPriorities(tasks, completed, priorities)
	ready := sched.GetReadyTasks(10)

	// billing and reporting (both tier 1) should be ready
	if len(ready) != 2 {
		t.Errorf("Ready count = %d, want 2", len(ready))
	}

	modules := make(map[string]bool)
	for _, task := range ready {
		modules[task.ID.Module] = true
	}
	if !modules["billing"] || !modules["reporting"] {
		t.Errorf("Expected billing and reporting, got %v", modules)
	}
}

func TestScheduler_GroupPriorities_UnassignedDefaultsToZero(t *testing.T) {
	tasks := []*domain.Task{
		{ID: domain.TaskID{Module: "auth", EpicNum: 0}, Status: domain.StatusNotStarted},
		{ID: domain.TaskID{Module: "notifications", EpicNum: 0}, Status: domain.StatusNotStarted}, // Not in priorities
		{ID: domain.TaskID{Module: "billing", EpicNum: 0}, Status: domain.StatusNotStarted},
	}
	completed := map[string]bool{}
	priorities := map[string]int{
		"auth":    0,
		"billing": 1,
		// notifications not listed - should default to tier 0
	}

	sched := NewWithPriorities(tasks, completed, priorities)
	ready := sched.GetReadyTasks(10)

	// auth and notifications (both effectively tier 0) should be ready
	if len(ready) != 2 {
		t.Errorf("Ready count = %d, want 2", len(ready))
	}

	modules := make(map[string]bool)
	for _, task := range ready {
		modules[task.ID.Module] = true
	}
	if !modules["auth"] || !modules["notifications"] {
		t.Errorf("Expected auth and notifications, got %v", modules)
	}
}

func TestScheduler_GetActivePriorityTier(t *testing.T) {
	tests := []struct {
		name       string
		tasks      []*domain.Task
		completed  map[string]bool
		priorities map[string]int
		wantTier   int
	}{
		{
			name: "tier 0 has incomplete tasks",
			tasks: []*domain.Task{
				{ID: domain.TaskID{Module: "auth", EpicNum: 0}, Status: domain.StatusNotStarted},
				{ID: domain.TaskID{Module: "billing", EpicNum: 0}, Status: domain.StatusNotStarted},
			},
			completed:  map[string]bool{},
			priorities: map[string]int{"auth": 0, "billing": 1},
			wantTier:   0,
		},
		{
			name: "tier 0 complete, tier 1 active",
			tasks: []*domain.Task{
				{ID: domain.TaskID{Module: "auth", EpicNum: 0}, Status: domain.StatusComplete},
				{ID: domain.TaskID{Module: "billing", EpicNum: 0}, Status: domain.StatusNotStarted},
			},
			completed:  map[string]bool{"auth/E00": true},
			priorities: map[string]int{"auth": 0, "billing": 1},
			wantTier:   1,
		},
		{
			name: "unassigned group treated as tier 0",
			tasks: []*domain.Task{
				{ID: domain.TaskID{Module: "unassigned", EpicNum: 0}, Status: domain.StatusNotStarted},
				{ID: domain.TaskID{Module: "billing", EpicNum: 0}, Status: domain.StatusNotStarted},
			},
			completed:  map[string]bool{},
			priorities: map[string]int{"billing": 1},
			wantTier:   0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sched := NewWithPriorities(tt.tasks, tt.completed, tt.priorities)
			tier := sched.getActivePriorityTier()
			if tier != tt.wantTier {
				t.Errorf("getActivePriorityTier() = %d, want %d", tier, tt.wantTier)
			}
		})
	}
}
