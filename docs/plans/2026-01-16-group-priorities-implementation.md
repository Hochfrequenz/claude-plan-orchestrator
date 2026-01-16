# Group Priorities Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Allow users to specify execution order between groups via priority tiers in the TUI, ensuring lower-tier groups complete before higher-tier groups start.

**Architecture:** Add a `group_priorities` database table for persistence, extend the scheduler to filter tasks by active priority tier, and add a new TUI view for managing group priorities with keyboard controls.

**Tech Stack:** Go, SQLite, bubbletea/lipgloss (TUI)

---

## Task 1: Add group_priorities table migration

**Files:**
- Modify: `internal/taskstore/migrations.go`
- Test: `internal/taskstore/store_test.go` (create if needed)

**Step 1: Write test for table creation**

Create a new test file to verify the table exists after store creation:

```go
// internal/taskstore/store_test.go
package taskstore

import (
	"os"
	"path/filepath"
	"testing"
)

func TestGroupPrioritiesTableExists(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")

	store, err := New(dbPath)
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
```

**Step 2: Run test to verify it fails**

Run: `go test -run TestGroupPrioritiesTableExists ./internal/taskstore/ -v`

Expected: FAIL with "no such table: group_priorities"

**Step 3: Add table to schema**

Add the `group_priorities` table to the schema in `internal/taskstore/migrations.go`:

```go
// Add after the agent_runs table in the schema constant:

CREATE TABLE IF NOT EXISTS group_priorities (
    group_name TEXT PRIMARY KEY,
    priority   INTEGER NOT NULL DEFAULT 0
);

CREATE INDEX IF NOT EXISTS idx_group_priorities_priority ON group_priorities(priority);
```

**Step 4: Run test to verify it passes**

Run: `go test -run TestGroupPrioritiesTableExists ./internal/taskstore/ -v`

Expected: PASS

**Step 5: Commit**

```bash
git add internal/taskstore/migrations.go internal/taskstore/store_test.go
git commit -m "$(cat <<'EOF'
feat(taskstore): add group_priorities table

Add database table for storing group priority tiers.
Groups with lower priority numbers execute first.
Groups not in the table default to priority 0.

Co-Authored-By: Claude Opus 4.5 <noreply@anthropic.com>
EOF
)"
```

---

## Task 2: Implement GetGroupPriorities store method

**Files:**
- Modify: `internal/taskstore/store.go`
- Test: `internal/taskstore/store_test.go`

**Step 1: Write test for GetGroupPriorities**

```go
func TestGetGroupPriorities(t *testing.T) {
	dir := t.TempDir()
	store, _ := New(filepath.Join(dir, "test.db"))
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
```

**Step 2: Run test to verify it fails**

Run: `go test -run TestGetGroupPriorities ./internal/taskstore/ -v`

Expected: FAIL with "store.GetGroupPriorities undefined"

**Step 3: Implement GetGroupPriorities**

Add to `internal/taskstore/store.go`:

```go
// GetGroupPriorities returns all group priorities as a map
func (s *Store) GetGroupPriorities() (map[string]int, error) {
	rows, err := s.db.Query("SELECT group_name, priority FROM group_priorities")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	priorities := make(map[string]int)
	for rows.Next() {
		var name string
		var priority int
		if err := rows.Scan(&name, &priority); err != nil {
			return nil, err
		}
		priorities[name] = priority
	}
	return priorities, rows.Err()
}
```

**Step 4: Run test to verify it passes**

Run: `go test -run TestGetGroupPriorities ./internal/taskstore/ -v`

Expected: PASS

**Step 5: Commit**

```bash
git add internal/taskstore/store.go internal/taskstore/store_test.go
git commit -m "$(cat <<'EOF'
feat(taskstore): add GetGroupPriorities method

Returns a map of group names to their priority tiers.

Co-Authored-By: Claude Opus 4.5 <noreply@anthropic.com>
EOF
)"
```

---

## Task 3: Implement SetGroupPriority store method

**Files:**
- Modify: `internal/taskstore/store.go`
- Test: `internal/taskstore/store_test.go`

**Step 1: Write test for SetGroupPriority**

```go
func TestSetGroupPriority(t *testing.T) {
	dir := t.TempDir()
	store, _ := New(filepath.Join(dir, "test.db"))
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
```

**Step 2: Run test to verify it fails**

Run: `go test -run TestSetGroupPriority ./internal/taskstore/ -v`

Expected: FAIL

**Step 3: Implement SetGroupPriority**

```go
// SetGroupPriority sets the priority tier for a group (upsert)
func (s *Store) SetGroupPriority(group string, priority int) error {
	_, err := s.db.Exec(`
		INSERT INTO group_priorities (group_name, priority)
		VALUES (?, ?)
		ON CONFLICT(group_name) DO UPDATE SET priority = excluded.priority
	`, group, priority)
	return err
}
```

**Step 4: Run test to verify it passes**

Run: `go test -run TestSetGroupPriority ./internal/taskstore/ -v`

Expected: PASS

**Step 5: Commit**

```bash
git add internal/taskstore/store.go internal/taskstore/store_test.go
git commit -m "$(cat <<'EOF'
feat(taskstore): add SetGroupPriority method

Upserts a group's priority tier.

Co-Authored-By: Claude Opus 4.5 <noreply@anthropic.com>
EOF
)"
```

---

## Task 4: Implement RemoveGroupPriority store method

**Files:**
- Modify: `internal/taskstore/store.go`
- Test: `internal/taskstore/store_test.go`

**Step 1: Write test for RemoveGroupPriority**

```go
func TestRemoveGroupPriority(t *testing.T) {
	dir := t.TempDir()
	store, _ := New(filepath.Join(dir, "test.db"))
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
```

**Step 2: Run test to verify it fails**

Run: `go test -run TestRemoveGroupPriority ./internal/taskstore/ -v`

Expected: FAIL

**Step 3: Implement RemoveGroupPriority**

```go
// RemoveGroupPriority removes a group from the priorities table
func (s *Store) RemoveGroupPriority(group string) error {
	_, err := s.db.Exec("DELETE FROM group_priorities WHERE group_name = ?", group)
	return err
}
```

**Step 4: Run test to verify it passes**

Run: `go test -run TestRemoveGroupPriority ./internal/taskstore/ -v`

Expected: PASS

**Step 5: Commit**

```bash
git add internal/taskstore/store.go internal/taskstore/store_test.go
git commit -m "$(cat <<'EOF'
feat(taskstore): add RemoveGroupPriority method

Removes a group from priorities, making it default to tier 0.

Co-Authored-By: Claude Opus 4.5 <noreply@anthropic.com>
EOF
)"
```

---

## Task 5: Implement GetGroupsWithTaskCounts store method

**Files:**
- Modify: `internal/taskstore/store.go`
- Test: `internal/taskstore/store_test.go`

**Step 1: Write test for GetGroupsWithTaskCounts**

```go
func TestGetGroupsWithTaskCounts(t *testing.T) {
	dir := t.TempDir()
	store, _ := New(filepath.Join(dir, "test.db"))
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
```

**Step 2: Run test to verify it fails**

Run: `go test -run TestGetGroupsWithTaskCounts ./internal/taskstore/ -v`

Expected: FAIL

**Step 3: Add GroupStats type and implement method**

```go
// GroupStats holds aggregated task counts for a group
type GroupStats struct {
	Name      string
	Priority  int  // -1 if unassigned
	Total     int
	Completed int
}

// GetGroupsWithTaskCounts returns all groups with their task statistics
func (s *Store) GetGroupsWithTaskCounts() ([]GroupStats, error) {
	// Query task counts by module
	rows, err := s.db.Query(`
		SELECT
			t.module,
			COALESCE(gp.priority, -1) as priority,
			COUNT(*) as total,
			SUM(CASE WHEN t.status = 'complete' THEN 1 ELSE 0 END) as completed
		FROM tasks t
		LEFT JOIN group_priorities gp ON t.module = gp.group_name
		GROUP BY t.module
		ORDER BY COALESCE(gp.priority, 0), t.module
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var stats []GroupStats
	for rows.Next() {
		var gs GroupStats
		if err := rows.Scan(&gs.Name, &gs.Priority, &gs.Total, &gs.Completed); err != nil {
			return nil, err
		}
		stats = append(stats, gs)
	}
	return stats, rows.Err()
}
```

**Step 4: Run test to verify it passes**

Run: `go test -run TestGetGroupsWithTaskCounts ./internal/taskstore/ -v`

Expected: PASS

**Step 5: Commit**

```bash
git add internal/taskstore/store.go internal/taskstore/store_test.go
git commit -m "$(cat <<'EOF'
feat(taskstore): add GetGroupsWithTaskCounts method

Returns groups with task counts and priority info.
Priority is -1 for unassigned groups.

Co-Authored-By: Claude Opus 4.5 <noreply@anthropic.com>
EOF
)"
```

---

## Task 6: Update scheduler to accept group priorities

**Files:**
- Modify: `internal/scheduler/scheduler.go`
- Test: `internal/scheduler/scheduler_test.go`

**Step 1: Write test for scheduler with group priorities**

```go
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
```

**Step 2: Run tests to verify they fail**

Run: `go test -run TestScheduler_WithGroupPriorities ./internal/scheduler/ -v`

Expected: FAIL with "NewWithPriorities undefined"

**Step 3: Add groupPriorities field and NewWithPriorities constructor**

```go
// Add to Scheduler struct:
type Scheduler struct {
	tasks           []*domain.Task
	taskMap         map[string]*domain.Task
	completed       map[string]bool
	depGraph        map[string][]string
	groupPriorities map[string]int // group -> priority tier
}

// Add new constructor after existing New():
// NewWithPriorities creates a Scheduler with group priority constraints
func NewWithPriorities(tasks []*domain.Task, completed map[string]bool, groupPriorities map[string]int) *Scheduler {
	s := New(tasks, completed)
	s.groupPriorities = groupPriorities
	return s
}
```

**Step 4: Run tests to verify partial pass**

Run: `go test -run TestScheduler_WithGroupPriorities ./internal/scheduler/ -v`

Expected: Tests run but may fail on priority filtering

**Step 5: Commit**

```bash
git add internal/scheduler/scheduler.go internal/scheduler/scheduler_test.go
git commit -m "$(cat <<'EOF'
feat(scheduler): add NewWithPriorities constructor

Prepares scheduler for group priority support.

Co-Authored-By: Claude Opus 4.5 <noreply@anthropic.com>
EOF
)"
```

---

## Task 7: Implement getActivePriorityTier method

**Files:**
- Modify: `internal/scheduler/scheduler.go`
- Test: `internal/scheduler/scheduler_test.go`

**Step 1: Write test for getActivePriorityTier**

```go
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
```

**Step 2: Run test to verify it fails**

Run: `go test -run TestScheduler_GetActivePriorityTier ./internal/scheduler/ -v`

Expected: FAIL

**Step 3: Implement getActivePriorityTier**

```go
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
```

**Step 4: Run test to verify it passes**

Run: `go test -run TestScheduler_GetActivePriorityTier ./internal/scheduler/ -v`

Expected: PASS

**Step 5: Commit**

```bash
git add internal/scheduler/scheduler.go internal/scheduler/scheduler_test.go
git commit -m "$(cat <<'EOF'
feat(scheduler): add getActivePriorityTier method

Finds the lowest priority tier with incomplete tasks.

Co-Authored-By: Claude Opus 4.5 <noreply@anthropic.com>
EOF
)"
```

---

## Task 8: Update GetReadyTasksExcluding to filter by active tier

**Files:**
- Modify: `internal/scheduler/scheduler.go`
- Test: `internal/scheduler/scheduler_test.go`

**Step 1: Run existing group priority tests**

Run: `go test -run TestScheduler_WithGroupPriorities ./internal/scheduler/ -v`
Run: `go test -run TestScheduler_GroupPriorities ./internal/scheduler/ -v`

Expected: Some tests fail because filtering not implemented yet

**Step 2: Update GetReadyTasksExcluding to filter by tier**

Modify the `GetReadyTasksExcluding` method to skip tasks not in the active tier:

```go
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

	for _, task := range s.tasks {
		// Skip tasks not in active tier (if priorities are configured)
		if len(s.groupPriorities) > 0 {
			taskTier := s.groupPriorities[task.ID.Module] // Defaults to 0
			if taskTier > activeTier {
				continue
			}
		}

		if task.IsReady(s.completed) && !s.dependsOnAny(task, inProgress) {
			ready = append(ready, task)
		}
	}

	// ... rest of sorting and selection unchanged
```

**Step 3: Run all group priority tests**

Run: `go test -run TestScheduler_WithGroupPriorities ./internal/scheduler/ -v`
Run: `go test -run TestScheduler_GroupPriorities ./internal/scheduler/ -v`

Expected: All PASS

**Step 4: Run all scheduler tests to ensure no regressions**

Run: `go test ./internal/scheduler/ -v`

Expected: All PASS

**Step 5: Commit**

```bash
git add internal/scheduler/scheduler.go
git commit -m "$(cat <<'EOF'
feat(scheduler): filter tasks by active priority tier

Tasks in higher tiers are blocked until all lower tiers complete.
Unassigned groups default to tier 0.

Co-Authored-By: Claude Opus 4.5 <noreply@anthropic.com>
EOF
)"
```

---

## Task 9: Add GroupPrioritiesView model state to TUI

**Files:**
- Modify: `tui/model.go`

**Step 1: Define GroupPrioritiesView struct**

Add new types and state to `tui/model.go`:

```go
// GroupPriorityItem represents a group in the priorities view
type GroupPriorityItem struct {
	Name      string
	Priority  int  // -1 if unassigned
	Total     int
	Completed int
}

// Add new fields to Model struct:
type Model struct {
	// ... existing fields ...

	// Group priorities view state
	showGroupPriorities bool                // Toggle with 'g' key
	groupPriorityItems  []GroupPriorityItem // Groups with their priorities
	selectedPriorityRow int                 // Currently selected row
}
```

**Step 2: Commit**

```bash
git add tui/model.go
git commit -m "$(cat <<'EOF'
feat(tui): add GroupPrioritiesView state

Prepares TUI model for group priorities view.

Co-Authored-By: Claude Opus 4.5 <noreply@anthropic.com>
EOF
)"
```

---

## Task 10: Implement renderGroupPriorities view function

**Files:**
- Modify: `tui/view.go`

**Step 1: Add renderGroupPriorities function**

```go
func (m Model) renderGroupPriorities() string {
	var b strings.Builder
	b.WriteString(titleStyle.Render("GROUP PRIORITIES"))
	b.WriteString("\n\n")

	if len(m.groupPriorityItems) == 0 {
		b.WriteString(queuedStyle.Render("  No groups found. Run 'claude-orch sync' to load tasks."))
		return b.String()
	}

	// Group items by tier
	tiers := make(map[int][]GroupPriorityItem)
	var maxTier int
	for _, item := range m.groupPriorityItems {
		tier := item.Priority
		if tier < 0 {
			tier = 0 // Unassigned defaults to tier 0
		}
		tiers[tier] = append(tiers[tier], item)
		if tier > maxTier {
			maxTier = tier
		}
	}

	// Determine active tier (lowest with incomplete tasks)
	activeTier := 0
	for tier := 0; tier <= maxTier; tier++ {
		for _, item := range tiers[tier] {
			if item.Completed < item.Total {
				activeTier = tier
				break
			}
		}
		if activeTier == tier {
			break
		}
	}

	// Render each tier
	itemIndex := 0
	for tier := 0; tier <= maxTier; tier++ {
		items := tiers[tier]
		if len(items) == 0 {
			continue
		}

		// Tier header
		tierStatus := "waiting"
		if tier == activeTier {
			tierStatus = "active"
		} else if tier < activeTier {
			tierStatus = "complete"
		}
		tierHeader := fmt.Sprintf("  Tier %d (%s)", tier, tierStatus)
		if tier == activeTier {
			b.WriteString(runningStyle.Render(tierHeader))
		} else {
			b.WriteString(queuedStyle.Render(tierHeader))
		}
		b.WriteString("\n")

		// Render items in this tier
		for _, item := range items {
			selected := itemIndex == m.selectedPriorityRow
			var statusIcon string
			var style lipgloss.Style

			if item.Completed == item.Total && item.Total > 0 {
				statusIcon = "✓"
				style = completedStyle
			} else if tier == activeTier {
				statusIcon = "●"
				style = runningStyle
			} else {
				statusIcon = "○"
				style = queuedStyle
			}

			line := fmt.Sprintf("    %s %-18s [%d/%d complete]",
				statusIcon, truncate(item.Name, 18), item.Completed, item.Total)

			if selected {
				line = fmt.Sprintf("  > %s", line[4:])
				b.WriteString(tabActiveStyle.Render(line))
			} else {
				b.WriteString(style.Render(line))
			}
			b.WriteString("\n")
			itemIndex++
		}
		b.WriteString("\n")
	}

	// Show unassigned groups section
	unassigned := tiers[-1] // Groups with Priority == -1 before we mapped them to 0
	// Actually, we already mapped them to 0 above, so this needs different tracking
	// Let's add a separate section for truly unassigned
	var unassignedItems []GroupPriorityItem
	for _, item := range m.groupPriorityItems {
		if item.Priority == -1 {
			unassignedItems = append(unassignedItems, item)
		}
	}

	if len(unassignedItems) > 0 {
		b.WriteString(queuedStyle.Render("  (unassigned - runs with tier 0)"))
		b.WriteString("\n")
		for _, item := range unassignedItems {
			selected := itemIndex == m.selectedPriorityRow
			statusIcon := "○"
			style := queuedStyle

			if item.Completed == item.Total && item.Total > 0 {
				statusIcon = "✓"
				style = completedStyle
			}

			line := fmt.Sprintf("    %s %-18s [%d/%d complete]",
				statusIcon, truncate(item.Name, 18), item.Completed, item.Total)

			if selected {
				line = fmt.Sprintf("  > %s", line[4:])
				b.WriteString(tabActiveStyle.Render(line))
			} else {
				b.WriteString(style.Render(line))
			}
			b.WriteString("\n")
			itemIndex++
		}
	}

	// Help text
	b.WriteString("\n")
	b.WriteString(queuedStyle.Render("  [↑/↓] select  [+/-] change tier  [u] unassign  [g] back"))

	return strings.TrimSuffix(b.String(), "\n")
}
```

**Step 2: Commit**

```bash
git add tui/view.go
git commit -m "$(cat <<'EOF'
feat(tui): add renderGroupPriorities view

Displays groups organized by priority tier with status.

Co-Authored-By: Claude Opus 4.5 <noreply@anthropic.com>
EOF
)"
```

---

## Task 11: Add 'g' key handler to toggle group priorities view

**Files:**
- Modify: `tui/update.go`
- Modify: `tui/view.go`

**Step 1: Add key handler in Update**

In `tui/update.go`, add handling for the 'g' key:

```go
// In the switch msg.String() block for tea.KeyMsg:
case "g":
	// Toggle group priorities view (only on Dashboard or Modules tab)
	if m.activeTab == 0 || m.activeTab == 3 {
		m.showGroupPriorities = !m.showGroupPriorities
		if m.showGroupPriorities {
			m.selectedPriorityRow = 0
			// Load group priority data
			return m, loadGroupPrioritiesCmd(m.store)
		}
	}
```

**Step 2: Add command to load group priorities**

```go
// GroupPrioritiesMsg contains loaded group priority data
type GroupPrioritiesMsg struct {
	Items []GroupPriorityItem
	Error error
}

func loadGroupPrioritiesCmd(store *taskstore.Store) tea.Cmd {
	return func() tea.Msg {
		if store == nil {
			return GroupPrioritiesMsg{Error: fmt.Errorf("no database configured")}
		}

		stats, err := store.GetGroupsWithTaskCounts()
		if err != nil {
			return GroupPrioritiesMsg{Error: err}
		}

		items := make([]GroupPriorityItem, len(stats))
		for i, s := range stats {
			items[i] = GroupPriorityItem{
				Name:      s.Name,
				Priority:  s.Priority,
				Total:     s.Total,
				Completed: s.Completed,
			}
		}

		return GroupPrioritiesMsg{Items: items}
	}
}
```

**Step 3: Handle the message in Update**

```go
case GroupPrioritiesMsg:
	if msg.Error != nil {
		m.statusMsg = fmt.Sprintf("Failed to load priorities: %v", msg.Error)
		m.showGroupPriorities = false
	} else {
		m.groupPriorityItems = msg.Items
	}
	return m, nil
```

**Step 4: Update View to render group priorities when active**

In `tui/view.go`, modify the View function to check for `showGroupPriorities`:

```go
// In the View() function, after tab bar:
// If group priorities view is active, render it instead of normal content
if m.showGroupPriorities {
	prioritiesSection := m.renderGroupPriorities()
	b.WriteString(sectionStyle.Width(m.width - 2).Render(prioritiesSection))
	b.WriteString("\n")
	// Skip to status bar
	goto statusBar
}

// ... existing tab content ...

statusBar:
// Status bar code
```

**Step 5: Commit**

```bash
git add tui/update.go tui/view.go
git commit -m "$(cat <<'EOF'
feat(tui): toggle group priorities view with 'g' key

Press 'g' on Dashboard or Modules tab to open priorities view.

Co-Authored-By: Claude Opus 4.5 <noreply@anthropic.com>
EOF
)"
```

---

## Task 12: Add +/- keys to change group priority tier

**Files:**
- Modify: `tui/update.go`

**Step 1: Add key handlers for +/- in group priorities view**

```go
// Add to key handlers when m.showGroupPriorities is true:
if m.showGroupPriorities {
	switch msg.String() {
	case "j", "down":
		if m.selectedPriorityRow < len(m.groupPriorityItems)-1 {
			m.selectedPriorityRow++
		}
		return m, nil
	case "k", "up":
		if m.selectedPriorityRow > 0 {
			m.selectedPriorityRow--
		}
		return m, nil
	case "+", "=":
		// Increase tier (lower priority)
		if m.selectedPriorityRow < len(m.groupPriorityItems) {
			item := &m.groupPriorityItems[m.selectedPriorityRow]
			newPriority := item.Priority + 1
			if item.Priority < 0 {
				newPriority = 1 // Unassigned goes to tier 1
			}
			return m, setGroupPriorityCmd(m.store, item.Name, newPriority)
		}
		return m, nil
	case "-", "_":
		// Decrease tier (higher priority)
		if m.selectedPriorityRow < len(m.groupPriorityItems) {
			item := &m.groupPriorityItems[m.selectedPriorityRow]
			newPriority := item.Priority - 1
			if newPriority < 0 {
				newPriority = 0
			}
			return m, setGroupPriorityCmd(m.store, item.Name, newPriority)
		}
		return m, nil
	case "u":
		// Unassign (remove from table)
		if m.selectedPriorityRow < len(m.groupPriorityItems) {
			item := m.groupPriorityItems[m.selectedPriorityRow]
			return m, removeGroupPriorityCmd(m.store, item.Name)
		}
		return m, nil
	case "g", "esc":
		// Close priorities view
		m.showGroupPriorities = false
		return m, nil
	}
}
```

**Step 2: Add command functions**

```go
// SetGroupPriorityMsg reports result of setting a group priority
type SetGroupPriorityMsg struct {
	Group    string
	Priority int
	Error    error
}

func setGroupPriorityCmd(store *taskstore.Store, group string, priority int) tea.Cmd {
	return func() tea.Msg {
		if store == nil {
			return SetGroupPriorityMsg{Group: group, Error: fmt.Errorf("no database")}
		}
		err := store.SetGroupPriority(group, priority)
		return SetGroupPriorityMsg{Group: group, Priority: priority, Error: err}
	}
}

// RemoveGroupPriorityMsg reports result of removing a group priority
type RemoveGroupPriorityMsg struct {
	Group string
	Error error
}

func removeGroupPriorityCmd(store *taskstore.Store, group string) tea.Cmd {
	return func() tea.Msg {
		if store == nil {
			return RemoveGroupPriorityMsg{Group: group, Error: fmt.Errorf("no database")}
		}
		err := store.RemoveGroupPriority(group)
		return RemoveGroupPriorityMsg{Group: group, Error: err}
	}
}
```

**Step 3: Handle the messages**

```go
case SetGroupPriorityMsg:
	if msg.Error != nil {
		m.statusMsg = fmt.Sprintf("Failed to set priority: %v", msg.Error)
	} else {
		// Update local state
		for i, item := range m.groupPriorityItems {
			if item.Name == msg.Group {
				m.groupPriorityItems[i].Priority = msg.Priority
				break
			}
		}
		m.statusMsg = fmt.Sprintf("Set %s to tier %d", msg.Group, msg.Priority)
	}
	return m, nil

case RemoveGroupPriorityMsg:
	if msg.Error != nil {
		m.statusMsg = fmt.Sprintf("Failed to unassign: %v", msg.Error)
	} else {
		// Update local state
		for i, item := range m.groupPriorityItems {
			if item.Name == msg.Group {
				m.groupPriorityItems[i].Priority = -1
				break
			}
		}
		m.statusMsg = fmt.Sprintf("Unassigned %s (defaults to tier 0)", msg.Group)
	}
	return m, nil
```

**Step 4: Commit**

```bash
git add tui/update.go
git commit -m "$(cat <<'EOF'
feat(tui): add +/- keys to change group priority tier

Press + to demote (higher tier number), - to promote (lower tier).
Press u to unassign (defaults to tier 0).

Co-Authored-By: Claude Opus 4.5 <noreply@anthropic.com>
EOF
)"
```

---

## Task 13: Wire scheduler to use stored group priorities

**Files:**
- Modify: `tui/update.go`
- Modify: `cmd/claude-orch/commands.go`

**Step 1: Update scheduler creation in TUI to load priorities**

When creating the scheduler in `tryStartAutoTasks` and batch start, load priorities from store:

```go
// In tryStartAutoTasks and anywhere scheduler is created:
var groupPriorities map[string]int
if m.store != nil {
	groupPriorities, _ = m.store.GetGroupPriorities()
}

// Use scheduler with priorities
var sched *scheduler.Scheduler
if len(groupPriorities) > 0 {
	sched = scheduler.NewWithPriorities(m.queued, m.completedTasks, groupPriorities)
} else {
	sched = scheduler.New(m.queued, m.completedTasks)
}
```

**Step 2: Also update renderQueued to respect priorities**

```go
// In renderQueued(), load priorities for scheduler:
var groupPriorities map[string]int
if m.store != nil {
	groupPriorities, _ = m.store.GetGroupPriorities()
}

var sched *scheduler.Scheduler
if len(groupPriorities) > 0 {
	sched = scheduler.NewWithPriorities(m.queued, m.completedTasks, groupPriorities)
} else {
	sched = scheduler.New(m.queued, m.completedTasks)
}
```

**Step 3: Commit**

```bash
git add tui/update.go tui/view.go
git commit -m "$(cat <<'EOF'
feat(tui): use stored group priorities in scheduler

Scheduler now respects priority tiers set via TUI.

Co-Authored-By: Claude Opus 4.5 <noreply@anthropic.com>
EOF
)"
```

---

## Task 14: Update CLI list command to show group filter by priority

**Files:**
- Modify: `cmd/claude-orch/commands.go`

**Step 1: Add --priority flag to list command**

```go
// In init(), add flag to listCmd:
listCmd.Flags().IntVar(&listPriority, "priority", -1, "filter by priority tier")

// Add variable:
var listPriority int
```

**Step 2: Update runList to filter by priority**

```go
func runList(cmd *cobra.Command, args []string) error {
	// ... existing code ...

	// If priority filter is set, load priorities and filter
	if listPriority >= 0 {
		priorities, err := store.GetGroupPriorities()
		if err != nil {
			return err
		}

		var filtered []*domain.Task
		for _, t := range tasks {
			taskPriority := priorities[t.ID.Module] // Defaults to 0
			if taskPriority == listPriority {
				filtered = append(filtered, t)
			}
		}
		tasks = filtered
	}

	// ... rest of output ...
}
```

**Step 3: Commit**

```bash
git add cmd/claude-orch/commands.go
git commit -m "$(cat <<'EOF'
feat(cli): add --priority flag to list command

Filter tasks by their group's priority tier.

Co-Authored-By: Claude Opus 4.5 <noreply@anthropic.com>
EOF
)"
```

---

## Task 15: Update documentation

**Files:**
- Modify: `CLAUDE.md`

**Step 1: Add Group Priorities section**

Add documentation about group priorities to CLAUDE.md:

```markdown
### Group Priorities

Groups can be assigned to priority tiers to control execution order. Groups in lower tiers must complete all tasks before groups in higher tiers start.

**TUI Controls:**
- Press `g` on Dashboard or Modules tab to open Group Priorities view
- `↑/↓` to navigate between groups
- `+/-` to change a group's priority tier
- `u` to unassign (defaults to tier 0)
- `g` or `esc` to close the view

**Example:**
```
Tier 0: auth (runs first)
Tier 1: billing, reporting (run in parallel after auth completes)
Tier 2: analytics (runs last)
```

Groups not explicitly assigned default to tier 0.
```

**Step 2: Update CLI Commands section**

Add priority flag to list command documentation:

```markdown
claude-orch list [--status S] [--group G] [--priority P]  # List tasks (filter by priority tier)
```

**Step 3: Commit**

```bash
git add CLAUDE.md
git commit -m "$(cat <<'EOF'
docs: add group priorities documentation

Document TUI controls and CLI flag for priority tiers.

Co-Authored-By: Claude Opus 4.5 <noreply@anthropic.com>
EOF
)"
```

---

## Task 16: Run full test suite and build

**Step 1: Run all tests**

Run: `go test ./... -v`

Expected: All tests pass.

**Step 2: Build and verify**

Run: `go build -o claude-orch ./cmd/claude-orch`

Expected: Build succeeds.

**Step 3: Manual smoke test**

Run: `./claude-orch tui`

- Press `g` to open group priorities view
- Navigate with arrows
- Press `+` or `-` to change a tier
- Press `g` to close
- Verify queued tasks respect priority tiers

**Step 4: Commit any final fixes if needed**

```bash
git add -A
git commit -m "$(cat <<'EOF'
chore: fix any issues found during testing

Co-Authored-By: Claude Opus 4.5 <noreply@anthropic.com>
EOF
)"
```
