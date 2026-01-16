# Group Priorities Design

## Goal

Allow users to specify execution order between groups via the TUI. Groups with lower priority tiers must complete all tasks before groups in higher tiers start.

## Data Model

**New database table: `group_priorities`**

```sql
CREATE TABLE group_priorities (
    group_name TEXT PRIMARY KEY,
    priority   INTEGER NOT NULL DEFAULT 0
);
```

- `priority` is a tier number (0, 1, 2, 3...)
- Lower numbers run first
- Groups with same priority can run in parallel
- Groups not in the table default to priority 0 (highest)

**Example:**

| Group | Priority |
|-------|----------|
| auth | 0 |
| billing | 1 |
| reporting | 1 |
| analytics | 2 |

Execution order: `auth` completes first, then `billing` and `reporting` run in parallel, finally `analytics`.

## Scheduler Changes

**Modified `GetReadyTasksExcluding` logic:**

```go
func (s *Scheduler) GetReadyTasksExcluding(limit int, inProgress map[string]bool) []*domain.Task {
    // Determine the active priority tier
    activeTier := s.getActivePriorityTier()

    var ready []*domain.Task
    for _, task := range s.tasks {
        // Skip tasks not in active tier
        if s.groupPriorities[task.ID.Module] > activeTier {
            continue
        }

        if task.IsReady(s.completed) && !s.dependsOnAny(task, inProgress) {
            ready = append(ready, task)
        }
    }
    // ... rest of sorting/selection unchanged
}

func (s *Scheduler) getActivePriorityTier() int {
    // Find lowest tier that has incomplete tasks
    tierHasIncomplete := make(map[int]bool)
    for _, task := range s.tasks {
        if !s.completed[task.ID.String()] {
            tier := s.groupPriorities[task.ID.Module]
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
```

The scheduler constructor takes `groupPriorities map[string]int` as a new parameter, loaded from the database.

## TUI Changes

**New "Group Priorities" view** (toggle with `g` key):

```
┌─ Group Priorities ────────────────────────────┐
│                                               │
│  Tier 0 (active)                              │
│    ● auth           [3/5 complete]            │
│                                               │
│  Tier 1 (waiting)                             │
│    ○ billing        [0/3 complete]            │
│                                               │
│  Tier 2 (waiting)                             │
│    ○ analytics      [0/4 complete]            │
│                                               │
│  (unassigned - runs with tier 0)              │
│    ● notifications  [1/2 complete]            │
│                                               │
├───────────────────────────────────────────────┤
│  ↑/↓ select   +/- change tier   u unassign   │
└───────────────────────────────────────────────┘
```

**Interactions:**
- `↑/↓` - Select group
- `+/-` - Increase/decrease tier number
- `u` - Unassign (remove from table, defaults to tier 0)
- `g` - Toggle back to task view

Groups are auto-discovered from parsed tasks.

## Storage Layer

**New methods in `taskstore/store.go`:**

```go
// GetGroupPriorities returns all group priorities
func (s *Store) GetGroupPriorities() (map[string]int, error)

// SetGroupPriority sets priority for a group (upsert)
func (s *Store) SetGroupPriority(group string, priority int) error

// RemoveGroupPriority removes a group from priorities table
func (s *Store) RemoveGroupPriority(group string) error

// GetGroupsWithTaskCounts returns groups with their task/completion stats
func (s *Store) GetGroupsWithTaskCounts() ([]GroupStats, error)

type GroupStats struct {
    Name      string
    Priority  int  // -1 if unassigned
    Total     int
    Completed int
}
```

**Migration:** Add `group_priorities` table on startup if not exists.

## Implementation Tasks

1. Add `group_priorities` table migration in taskstore
2. Implement store methods for group priorities
3. Update scheduler to accept and use group priorities
4. Add TUI group priorities view
5. Wire up TUI view to store
6. Add tests for scheduler with group priorities
7. Update documentation
