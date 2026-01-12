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
