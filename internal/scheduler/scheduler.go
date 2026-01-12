package scheduler

import (
	"sort"

	"github.com/hochfrequenz/claude-plan-orchestrator/internal/domain"
)

// Scheduler determines which tasks are ready to run
type Scheduler struct {
	tasks     []*domain.Task
	taskMap   map[string]*domain.Task
	completed map[string]bool
	depGraph  map[string][]string // task -> tasks that depend on it
}

// New creates a new Scheduler
func New(tasks []*domain.Task, completed map[string]bool) *Scheduler {
	taskMap := make(map[string]*domain.Task, len(tasks))
	depGraph := make(map[string][]string)

	for _, t := range tasks {
		taskMap[t.ID.String()] = t
		for _, dep := range t.DependsOn {
			depGraph[dep.String()] = append(depGraph[dep.String()], t.ID.String())
		}
	}

	return &Scheduler{
		tasks:     tasks,
		taskMap:   taskMap,
		completed: completed,
		depGraph:  depGraph,
	}
}

// GetReadyTasks returns up to limit tasks that are ready to run
func (s *Scheduler) GetReadyTasks(limit int) []*domain.Task {
	var ready []*domain.Task

	for _, task := range s.tasks {
		if task.IsReady(s.completed) {
			ready = append(ready, task)
		}
	}

	// Sort by priority
	sort.Slice(ready, func(i, j int) bool {
		// 1. Priority (high > normal > low)
		pi, pj := priorityOrder(ready[i].Priority), priorityOrder(ready[j].Priority)
		if pi != pj {
			return pi < pj
		}

		// 2. Dependency depth (unblocks more work)
		di, dj := s.dependencyDepth(ready[i].ID.String()), s.dependencyDepth(ready[j].ID.String())
		if di != dj {
			return di > dj
		}

		// 3. Module grouping (same module as recently completed)
		// 4. Epic number (earlier epics first within same module)
		if ready[i].ID.Module != ready[j].ID.Module {
			return ready[i].ID.Module < ready[j].ID.Module
		}
		return ready[i].ID.EpicNum < ready[j].ID.EpicNum
	})

	if len(ready) > limit {
		ready = ready[:limit]
	}

	return ready
}

// dependencyDepth returns how many tasks depend (transitively) on this task
func (s *Scheduler) dependencyDepth(taskID string) int {
	visited := make(map[string]bool)
	return s.countDependents(taskID, visited)
}

func (s *Scheduler) countDependents(taskID string, visited map[string]bool) int {
	if visited[taskID] {
		return 0
	}
	visited[taskID] = true

	count := 0
	for _, depID := range s.depGraph[taskID] {
		count += 1 + s.countDependents(depID, visited)
	}
	return count
}

func priorityOrder(p domain.Priority) int {
	switch p {
	case domain.PriorityHigh:
		return 0
	case domain.PriorityLow:
		return 2
	default:
		return 1
	}
}

// TopologicalSort returns tasks in dependency order
func (s *Scheduler) TopologicalSort() ([]*domain.Task, error) {
	inDegree := make(map[string]int)
	for _, t := range s.tasks {
		inDegree[t.ID.String()] = 0
	}
	for _, t := range s.tasks {
		for _, dep := range t.DependsOn {
			inDegree[t.ID.String()]++
			_ = dep // use the dependency
		}
	}

	var queue []string
	for id, deg := range inDegree {
		if deg == 0 {
			queue = append(queue, id)
		}
	}

	var result []*domain.Task
	for len(queue) > 0 {
		id := queue[0]
		queue = queue[1:]
		result = append(result, s.taskMap[id])

		for _, depID := range s.depGraph[id] {
			inDegree[depID]--
			if inDegree[depID] == 0 {
				queue = append(queue, depID)
			}
		}
	}

	return result, nil
}
