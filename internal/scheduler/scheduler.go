package scheduler

import (
	"fmt"
	"os"
	"sort"

	"github.com/hochfrequenz/claude-plan-orchestrator/internal/domain"
)

// Scheduler determines which tasks are ready to run
type Scheduler struct {
	tasks           []*domain.Task
	taskMap         map[string]*domain.Task
	completed       map[string]bool
	depGraph        map[string][]string // task -> tasks that depend on it
	groupPriorities map[string]int      // group -> priority tier
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

// NewWithPriorities creates a Scheduler with group priority constraints
func NewWithPriorities(tasks []*domain.Task, completed map[string]bool, groupPriorities map[string]int) *Scheduler {
	s := New(tasks, completed)
	s.groupPriorities = groupPriorities
	return s
}

// GetReadyTasks returns up to limit tasks that are ready to run
// It also accepts a set of currently in-progress task IDs to avoid conflicts
func (s *Scheduler) GetReadyTasks(limit int) []*domain.Task {
	return s.GetReadyTasksExcluding(limit, nil)
}

// GetReadyTasksExcluding returns up to limit tasks that are ready to run,
// excluding tasks that depend on the given in-progress tasks.
// It also ensures selected tasks don't conflict with each other (no dependencies between them).
func (s *Scheduler) GetReadyTasksExcluding(limit int, inProgress map[string]bool) []*domain.Task {
	if inProgress == nil {
		inProgress = make(map[string]bool)
	}

	// Determine active priority tier (only if priorities are configured)
	activeTier := 0
	if len(s.groupPriorities) > 0 {
		activeTier = s.getActivePriorityTier()
	}

	// Combine completed and in-progress for dependency checking
	unavailable := make(map[string]bool)
	for id := range s.completed {
		unavailable[id] = true
	}

	var ready []*domain.Task
	var debugSkipped []string

	for _, task := range s.tasks {
		// Skip tasks not in active tier (if priorities are configured)
		if len(s.groupPriorities) > 0 {
			taskTier := s.groupPriorities[task.ID.Module] // Defaults to 0 if not found
			if taskTier > activeTier {
				continue
			}
		}

		isReady := task.IsReady(s.completed)
		dependsOnInProgress := s.dependsOnAny(task, inProgress)

		if isReady && !dependsOnInProgress {
			ready = append(ready, task)
		} else if task.ID.String() == "cli-tui-implementation/TUI06" {
			// Debug: why is TUI06 not ready?
			debugSkipped = append(debugSkipped, fmt.Sprintf("TUI06: isReady=%v, dependsOnInProgress=%v, status=%s", isReady, dependsOnInProgress, task.Status))
		}
	}

	// Log debug info for TUI06 if it was skipped
	if len(debugSkipped) > 0 {
		fmt.Fprintf(os.Stderr, "SCHEDULER DEBUG: %v\n", debugSkipped)
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

		// 3. Different modules first (spread work across modules)
		if ready[i].ID.Module != ready[j].ID.Module {
			return ready[i].ID.Module < ready[j].ID.Module
		}

		// 4. Epic number (earlier epics first within same module)
		if ready[i].ID.EpicNum != ready[j].ID.EpicNum {
			return ready[i].ID.EpicNum < ready[j].ID.EpicNum
		}

		// 5. Prefix (alphabetical, for consistent ordering of CLI vs TUI etc.)
		return ready[i].ID.Prefix < ready[j].ID.Prefix
	})

	// Select tasks ensuring no conflicts between selected tasks
	var selected []*domain.Task
	selectedIDs := make(map[string]bool)
	selectedSequences := make(map[string]int) // track highest epic per sequence (module+prefix)

	for _, task := range ready {
		if len(selected) >= limit {
			break
		}

		// Check if this task conflicts with already selected tasks
		if s.conflictsWithSelected(task, selectedIDs, selectedSequences) {
			continue
		}

		selected = append(selected, task)
		selectedIDs[task.ID.String()] = true
		// Track highest epic number selected per sequence (module+prefix)
		seqKey := task.ID.Module + "/" + task.ID.Prefix
		if task.ID.EpicNum > selectedSequences[seqKey] {
			selectedSequences[seqKey] = task.ID.EpicNum
		}
	}

	return selected
}

// dependsOnAny checks if task depends on any of the given task IDs
func (s *Scheduler) dependsOnAny(task *domain.Task, taskIDs map[string]bool) bool {
	for _, dep := range task.DependsOn {
		if taskIDs[dep.String()] {
			return true
		}
	}
	return false
}

// conflictsWithSelected checks if selecting this task would conflict with already selected tasks
func (s *Scheduler) conflictsWithSelected(task *domain.Task, selectedIDs map[string]bool, selectedSequences map[string]int) bool {
	// Check explicit dependencies - task can't depend on a selected task
	for _, dep := range task.DependsOn {
		if selectedIDs[dep.String()] {
			return true
		}
	}

	// Check if a selected task depends on this task
	for selID := range selectedIDs {
		selTask := s.taskMap[selID]
		if selTask != nil {
			for _, dep := range selTask.DependsOn {
				if dep.String() == task.ID.String() {
					return true
				}
			}
		}
	}

	// Check implicit sequential dependency within same sequence (module+prefix)
	// Tasks with different prefixes (e.g., CLI vs TUI) are independent sequences
	seqKey := task.ID.Module + "/" + task.ID.Prefix
	if highestEpic, exists := selectedSequences[seqKey]; exists {
		// Within same sequence, epics are sequential
		// Don't run CLI03 if CLI02 is already selected (implicit dependency)
		// But CLI02 and TUI02 are independent (different sequences)

		// Check if this task has implicit dependency on any selected task in same sequence
		if task.ID.EpicNum > highestEpic {
			// This task comes after already selected tasks in same sequence
			for epic := highestEpic; epic < task.ID.EpicNum; epic++ {
				implicitDep := domain.TaskID{Module: task.ID.Module, Prefix: task.ID.Prefix, EpicNum: epic}
				// If the implicit dependency isn't completed, we shouldn't run this in parallel
				if !s.completed[implicitDep.String()] && selectedIDs[implicitDep.String()] {
					return true
				}
			}
		} else if task.ID.EpicNum < highestEpic {
			// A later epic was already selected, check if it depends on this one
			for epic := task.ID.EpicNum + 1; epic <= highestEpic; epic++ {
				implicitDep := domain.TaskID{Module: task.ID.Module, Prefix: task.ID.Prefix, EpicNum: epic}
				if selectedIDs[implicitDep.String()] {
					// Check if that selected task might depend on this one
					selTask := s.taskMap[implicitDep.String()]
					if selTask != nil && !s.completed[task.ID.String()] {
						// If they're in same sequence and sequential, assume dependency
						return true
					}
				}
			}
		}
	}

	return false
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

// getActivePriorityTier returns the lowest priority tier that has incomplete tasks
func (s *Scheduler) getActivePriorityTier() int {
	if len(s.groupPriorities) == 0 {
		return 0 // No priorities configured, all tasks are tier 0
	}

	// Find the maximum tier number
	maxTier := 0
	for _, tier := range s.groupPriorities {
		if tier > maxTier {
			maxTier = tier
		}
	}

	// Track which tiers have incomplete tasks
	tierHasIncomplete := make(map[int]bool)
	for _, task := range s.tasks {
		if !s.completed[task.ID.String()] && task.Status != domain.StatusComplete {
			tier := s.groupPriorities[task.ID.Module] // Defaults to 0 if not found
			tierHasIncomplete[tier] = true
		}
	}

	// Return lowest tier with incomplete work
	for tier := 0; tier <= maxTier; tier++ {
		if tierHasIncomplete[tier] {
			return tier
		}
	}
	return 0
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
