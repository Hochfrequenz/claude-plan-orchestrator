# TUI Sync Button Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add a two-way sync button to the Modules tab that synchronizes task status between markdown files and the SQLite database, with conflict detection and modal-based resolution.

**Architecture:** Centralize all sync logic in `internal/sync/` package with a `TwoWaySync()` function that both CLI and TUI use. TUI displays conflicts in a modal popup, CLI prints them to stdout.

**Tech Stack:** Go, bubbletea, SQLite, YAML frontmatter parsing

---

## Task 1: Add Sync Types to internal/sync/sync.go

**Files:**
- Modify: `internal/sync/sync.go`
- Test: `internal/sync/sync_test.go`

**Step 1: Write the failing test for SyncConflict detection**

Add to `internal/sync/sync_test.go`:

```go
func TestTwoWaySync_DetectsConflicts(t *testing.T) {
	// Setup: create temp dirs and files
	root := t.TempDir()
	plansDir := filepath.Join(root, "docs", "plans")
	moduleDir := filepath.Join(plansDir, "technical")
	os.MkdirAll(moduleDir, 0755)

	// Create epic file with status: complete
	epicPath := filepath.Join(moduleDir, "E05-validators.md")
	content := `---
status: complete
---

# E05: Validators
`
	os.WriteFile(epicPath, []byte(content), 0644)

	// Create in-memory store with status: in_progress
	store, _ := taskstore.New(":memory:")
	defer store.Close()
	store.UpsertTask(&domain.Task{
		ID:       domain.TaskID{Module: "technical", EpicNum: 5},
		Title:    "Validators",
		Status:   domain.StatusInProgress,
		FilePath: epicPath,
	})

	syncer := New(plansDir)
	result, err := syncer.TwoWaySync(store)
	if err != nil {
		t.Fatal(err)
	}

	if len(result.Conflicts) != 1 {
		t.Fatalf("expected 1 conflict, got %d", len(result.Conflicts))
	}

	c := result.Conflicts[0]
	if c.TaskID != "technical/E05" {
		t.Errorf("expected conflict for technical/E05, got %s", c.TaskID)
	}
	if c.DBStatus != "in_progress" {
		t.Errorf("expected DB status in_progress, got %s", c.DBStatus)
	}
	if c.MarkdownStatus != "complete" {
		t.Errorf("expected markdown status complete, got %s", c.MarkdownStatus)
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test -run TestTwoWaySync_DetectsConflicts ./internal/sync -v`
Expected: FAIL with "TwoWaySync not defined"

**Step 3: Add types and TwoWaySync function signature**

Add to `internal/sync/sync.go` after the existing imports:

```go
import (
	// ... existing imports
	"github.com/hochfrequenz/claude-plan-orchestrator/internal/parser"
	"github.com/hochfrequenz/claude-plan-orchestrator/internal/taskstore"
)

// SyncResult contains the result of a two-way sync operation
type SyncResult struct {
	MarkdownToDBCount int            // Tasks updated in DB from markdown
	DBToMarkdownCount int            // Tasks updated in markdown from DB
	Conflicts         []SyncConflict // Mismatches requiring resolution
}

// SyncConflict represents a status mismatch between DB and markdown
type SyncConflict struct {
	TaskID         string
	DBStatus       string
	MarkdownStatus string
	EpicFilePath   string
}

// TwoWaySync performs a two-way sync between markdown files and the database.
// Returns conflicts that need manual resolution.
func (s *Syncer) TwoWaySync(store *taskstore.Store) (*SyncResult, error) {
	result := &SyncResult{}

	// 1. Parse all markdown files to get their statuses
	mdTasks, err := parser.ParsePlansDir(s.plansDir)
	if err != nil {
		return nil, fmt.Errorf("parsing plans: %w", err)
	}

	// Build map of markdown statuses
	mdStatuses := make(map[string]*domain.Task)
	for _, t := range mdTasks {
		mdStatuses[t.ID.String()] = t
	}

	// 2. Get all tasks from database
	dbTasks, err := store.ListTasks(taskstore.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("listing tasks: %w", err)
	}

	// Build map of DB statuses
	dbStatuses := make(map[string]*domain.Task)
	for _, t := range dbTasks {
		dbStatuses[t.ID.String()] = t
	}

	// 3. Compare and categorize
	// Tasks only in markdown -> sync to DB
	for id, mdTask := range mdStatuses {
		if _, exists := dbStatuses[id]; !exists {
			if err := store.UpsertTask(mdTask); err != nil {
				return nil, fmt.Errorf("upserting %s: %w", id, err)
			}
			result.MarkdownToDBCount++
		}
	}

	// Tasks only in DB -> sync to markdown (update frontmatter)
	for id, dbTask := range dbStatuses {
		if _, exists := mdStatuses[id]; !exists {
			// Task in DB but not in markdown - skip (file may have been deleted)
			continue
		}
	}

	// Tasks in both -> check for conflicts
	for id, dbTask := range dbStatuses {
		mdTask, exists := mdStatuses[id]
		if !exists {
			continue
		}

		if dbTask.Status != mdTask.Status {
			result.Conflicts = append(result.Conflicts, SyncConflict{
				TaskID:         id,
				DBStatus:       string(dbTask.Status),
				MarkdownStatus: string(mdTask.Status),
				EpicFilePath:   mdTask.FilePath,
			})
		}
	}

	return result, nil
}
```

**Step 4: Run test to verify it passes**

Run: `go test -run TestTwoWaySync_DetectsConflicts ./internal/sync -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/sync/sync.go internal/sync/sync_test.go
git commit -m "feat(sync): add TwoWaySync with conflict detection"
```

---

## Task 2: Add ResolveConflicts Function

**Files:**
- Modify: `internal/sync/sync.go`
- Test: `internal/sync/sync_test.go`

**Step 1: Write the failing test for conflict resolution**

Add to `internal/sync/sync_test.go`:

```go
func TestResolveConflicts_UseDB(t *testing.T) {
	root := t.TempDir()
	plansDir := filepath.Join(root, "docs", "plans")
	moduleDir := filepath.Join(plansDir, "technical")
	os.MkdirAll(moduleDir, 0755)

	// Epic file says complete
	epicPath := filepath.Join(moduleDir, "E05-validators.md")
	content := `---
status: complete
---

# E05: Validators
`
	os.WriteFile(epicPath, []byte(content), 0644)

	// README at project root
	readmePath := filepath.Join(root, "README.md")
	readme := `# Project

### Technical Module

| Epic | Description | Status |
|------|-------------|:------:|
| [E05](docs/plans/technical/E05-validators.md) | Validators | 🟢 |
`
	os.WriteFile(readmePath, []byte(readme), 0644)

	// DB says in_progress
	store, _ := taskstore.New(":memory:")
	defer store.Close()
	store.UpsertTask(&domain.Task{
		ID:       domain.TaskID{Module: "technical", EpicNum: 5},
		Title:    "Validators",
		Status:   domain.StatusInProgress,
		FilePath: epicPath,
	})

	syncer := New(plansDir)

	// Resolve: use DB value (in_progress)
	resolutions := map[string]string{
		"technical/E05": "db",
	}
	err := syncer.ResolveConflicts(store, resolutions)
	if err != nil {
		t.Fatal(err)
	}

	// Verify markdown was updated to in_progress
	updated, _ := os.ReadFile(epicPath)
	if !strings.Contains(string(updated), "status: in_progress") {
		t.Errorf("epic should have status: in_progress, got:\n%s", string(updated))
	}

	// Verify README emoji was updated
	updatedReadme, _ := os.ReadFile(readmePath)
	if !strings.Contains(string(updatedReadme), "🟡") {
		t.Errorf("README should have 🟡, got:\n%s", string(updatedReadme))
	}
}

func TestResolveConflicts_UseMarkdown(t *testing.T) {
	root := t.TempDir()
	plansDir := filepath.Join(root, "docs", "plans")
	moduleDir := filepath.Join(plansDir, "technical")
	os.MkdirAll(moduleDir, 0755)

	// Epic file says complete
	epicPath := filepath.Join(moduleDir, "E05-validators.md")
	content := `---
status: complete
---

# E05: Validators
`
	os.WriteFile(epicPath, []byte(content), 0644)

	// DB says in_progress
	store, _ := taskstore.New(":memory:")
	defer store.Close()
	store.UpsertTask(&domain.Task{
		ID:       domain.TaskID{Module: "technical", EpicNum: 5},
		Title:    "Validators",
		Status:   domain.StatusInProgress,
		FilePath: epicPath,
	})

	syncer := New(plansDir)

	// Resolve: use markdown value (complete)
	resolutions := map[string]string{
		"technical/E05": "markdown",
	}
	err := syncer.ResolveConflicts(store, resolutions)
	if err != nil {
		t.Fatal(err)
	}

	// Verify DB was updated to complete
	task, _ := store.GetTask("technical/E05")
	if task.Status != domain.StatusComplete {
		t.Errorf("DB should have status complete, got %s", task.Status)
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test -run TestResolveConflicts ./internal/sync -v`
Expected: FAIL with "ResolveConflicts not defined"

**Step 3: Implement ResolveConflicts**

Add to `internal/sync/sync.go`:

```go
// ResolveConflicts applies user resolutions to sync conflicts.
// resolutions maps taskID to "db" or "markdown" indicating which source wins.
func (s *Syncer) ResolveConflicts(store *taskstore.Store, resolutions map[string]string) error {
	for taskID, resolution := range resolutions {
		// Parse task ID
		tid, err := domain.ParseTaskID(taskID)
		if err != nil {
			return fmt.Errorf("parsing task ID %s: %w", taskID, err)
		}

		// Get both sources
		dbTask, err := store.GetTask(taskID)
		if err != nil {
			return fmt.Errorf("getting task %s from DB: %w", taskID, err)
		}

		switch resolution {
		case "db":
			// DB wins: update markdown to match DB
			if dbTask.FilePath != "" {
				if err := s.UpdateEpicFrontmatter(dbTask.FilePath, dbTask.Status); err != nil {
					return fmt.Errorf("updating epic %s: %w", taskID, err)
				}
				if err := s.UpdateTaskStatus(tid, dbTask.Status); err != nil {
					return fmt.Errorf("updating README for %s: %w", taskID, err)
				}
			}

		case "markdown":
			// Markdown wins: update DB to match markdown
			mdTasks, err := parser.ParsePlansDir(s.plansDir)
			if err != nil {
				return fmt.Errorf("parsing plans: %w", err)
			}
			for _, mdTask := range mdTasks {
				if mdTask.ID.String() == taskID {
					if err := store.UpdateTaskStatus(taskID, mdTask.Status); err != nil {
						return fmt.Errorf("updating DB for %s: %w", taskID, err)
					}
					break
				}
			}

		default:
			return fmt.Errorf("invalid resolution %q for %s (must be 'db' or 'markdown')", resolution, taskID)
		}
	}

	return nil
}
```

**Step 4: Run test to verify it passes**

Run: `go test -run TestResolveConflicts ./internal/sync -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/sync/sync.go internal/sync/sync_test.go
git commit -m "feat(sync): add ResolveConflicts for two-way sync"
```

---

## Task 3: Add SyncMarkdownToDB Helper

**Files:**
- Modify: `internal/sync/sync.go`
- Test: `internal/sync/sync_test.go`

**Step 1: Write the failing test**

Add to `internal/sync/sync_test.go`:

```go
func TestSyncMarkdownToDB(t *testing.T) {
	root := t.TempDir()
	plansDir := filepath.Join(root, "docs", "plans")
	moduleDir := filepath.Join(plansDir, "technical")
	os.MkdirAll(moduleDir, 0755)

	// Create two epic files
	epic1 := filepath.Join(moduleDir, "E01-setup.md")
	os.WriteFile(epic1, []byte("---\nstatus: complete\n---\n\n# E01: Setup\n"), 0644)

	epic2 := filepath.Join(moduleDir, "E02-feature.md")
	os.WriteFile(epic2, []byte("---\nstatus: in_progress\n---\n\n# E02: Feature\n"), 0644)

	store, _ := taskstore.New(":memory:")
	defer store.Close()

	syncer := New(plansDir)
	count, err := syncer.SyncMarkdownToDB(store)
	if err != nil {
		t.Fatal(err)
	}

	if count != 2 {
		t.Errorf("expected 2 tasks synced, got %d", count)
	}

	// Verify tasks are in DB
	task1, _ := store.GetTask("technical/E01")
	if task1 == nil || task1.Status != domain.StatusComplete {
		t.Error("E01 should be complete in DB")
	}

	task2, _ := store.GetTask("technical/E02")
	if task2 == nil || task2.Status != domain.StatusInProgress {
		t.Error("E02 should be in_progress in DB")
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test -run TestSyncMarkdownToDB ./internal/sync -v`
Expected: FAIL

**Step 3: Implement SyncMarkdownToDB**

Add to `internal/sync/sync.go`:

```go
// SyncMarkdownToDB parses all markdown files and upserts them to the database.
// Returns the number of tasks synced.
func (s *Syncer) SyncMarkdownToDB(store *taskstore.Store) (int, error) {
	tasks, err := parser.ParsePlansDir(s.plansDir)
	if err != nil {
		return 0, fmt.Errorf("parsing plans: %w", err)
	}

	for _, task := range tasks {
		if err := store.UpsertTask(task); err != nil {
			return 0, fmt.Errorf("upserting %s: %w", task.ID.String(), err)
		}
	}

	return len(tasks), nil
}
```

**Step 4: Run test to verify it passes**

Run: `go test -run TestSyncMarkdownToDB ./internal/sync -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/sync/sync.go internal/sync/sync_test.go
git commit -m "feat(sync): add SyncMarkdownToDB helper"
```

---

## Task 4: Add SyncDBToMarkdown Helper

**Files:**
- Modify: `internal/sync/sync.go`
- Test: `internal/sync/sync_test.go`

**Step 1: Write the failing test**

Add to `internal/sync/sync_test.go`:

```go
func TestSyncDBToMarkdown(t *testing.T) {
	root := t.TempDir()
	plansDir := filepath.Join(root, "docs", "plans")
	moduleDir := filepath.Join(plansDir, "technical")
	os.MkdirAll(moduleDir, 0755)

	// Create epic file with not_started
	epicPath := filepath.Join(moduleDir, "E01-setup.md")
	os.WriteFile(epicPath, []byte("---\nstatus: not_started\n---\n\n# E01: Setup\n"), 0644)

	// Create README
	readmePath := filepath.Join(root, "README.md")
	readme := `# Project

### Technical Module

| Epic | Description | Status |
|------|-------------|:------:|
| [E01](docs/plans/technical/E01-setup.md) | Setup | 🔴 |
`
	os.WriteFile(readmePath, []byte(readme), 0644)

	// DB has it as complete
	store, _ := taskstore.New(":memory:")
	defer store.Close()
	store.UpsertTask(&domain.Task{
		ID:       domain.TaskID{Module: "technical", EpicNum: 1},
		Title:    "Setup",
		Status:   domain.StatusComplete,
		FilePath: epicPath,
	})

	syncer := New(plansDir)
	count, err := syncer.SyncDBToMarkdown(store)
	if err != nil {
		t.Fatal(err)
	}

	if count != 1 {
		t.Errorf("expected 1 task synced, got %d", count)
	}

	// Verify epic frontmatter updated
	updated, _ := os.ReadFile(epicPath)
	if !strings.Contains(string(updated), "status: complete") {
		t.Errorf("epic should have status: complete, got:\n%s", string(updated))
	}

	// Verify README updated
	updatedReadme, _ := os.ReadFile(readmePath)
	if !strings.Contains(string(updatedReadme), "🟢") {
		t.Errorf("README should have 🟢, got:\n%s", string(updatedReadme))
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test -run TestSyncDBToMarkdown ./internal/sync -v`
Expected: FAIL

**Step 3: Implement SyncDBToMarkdown**

Add to `internal/sync/sync.go`:

```go
// SyncDBToMarkdown updates all markdown files to match database statuses.
// Returns the number of tasks synced.
func (s *Syncer) SyncDBToMarkdown(store *taskstore.Store) (int, error) {
	tasks, err := store.ListTasks(taskstore.ListOptions{})
	if err != nil {
		return 0, fmt.Errorf("listing tasks: %w", err)
	}

	count := 0
	for _, task := range tasks {
		if task.FilePath == "" {
			continue
		}

		// Update frontmatter
		if err := s.UpdateEpicFrontmatter(task.FilePath, task.Status); err != nil {
			// Log but continue - file may not exist
			continue
		}

		// Update README
		if err := s.UpdateTaskStatus(task.ID, task.Status); err != nil {
			// Log but continue
			continue
		}

		count++
	}

	return count, nil
}
```

**Step 4: Run test to verify it passes**

Run: `go test -run TestSyncDBToMarkdown ./internal/sync -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/sync/sync.go internal/sync/sync_test.go
git commit -m "feat(sync): add SyncDBToMarkdown helper"
```

---

## Task 5: Refactor CLI sync Command

**Files:**
- Modify: `cmd/claude-orch/commands.go`

**Step 1: Read current implementation**

Current `runSync` at lines 255-285 parses markdown and upserts to DB.

**Step 2: Refactor to use centralized sync**

Replace `runSync` function:

```go
func runSync(cmd *cobra.Command, args []string) error {
	cfg, err := loadConfig()
	if err != nil {
		return err
	}

	if cfg.General.ProjectRoot == "" {
		return fmt.Errorf("project_root not configured")
	}

	plansDir := cfg.General.ProjectRoot + "/docs/plans"

	store, err := taskstore.New(cfg.General.DatabasePath)
	if err != nil {
		return err
	}
	defer store.Close()

	syncer := sync.New(plansDir)
	result, err := syncer.TwoWaySync(store)
	if err != nil {
		return fmt.Errorf("sync failed: %w", err)
	}

	// Report results
	if result.MarkdownToDBCount > 0 {
		fmt.Printf("Synced %d tasks from markdown to database\n", result.MarkdownToDBCount)
	}
	if result.DBToMarkdownCount > 0 {
		fmt.Printf("Synced %d tasks from database to markdown\n", result.DBToMarkdownCount)
	}

	// Report conflicts
	if len(result.Conflicts) > 0 {
		fmt.Printf("\n%d conflicts detected:\n", len(result.Conflicts))
		for _, c := range result.Conflicts {
			fmt.Printf("  %s: DB=%s, Markdown=%s\n", c.TaskID, c.DBStatus, c.MarkdownStatus)
		}
		fmt.Println("\nUse 'claude-orch tui' to resolve conflicts interactively.")
	} else {
		fmt.Println("No conflicts found.")
	}

	return nil
}
```

**Step 3: Build and test CLI**

Run: `go build -o claude-orch ./cmd/claude-orch && ./claude-orch sync`
Expected: Shows sync results or "project_root not configured"

**Step 4: Commit**

```bash
git add cmd/claude-orch/commands.go
git commit -m "refactor(cli): use centralized TwoWaySync in sync command"
```

---

## Task 6: Add Sync Modal to TUI Model

**Files:**
- Modify: `tui/model.go`

**Step 1: Add new types and fields**

Add after `ModuleSummary` struct (around line 32):

```go
// SyncConflictModal holds state for the sync conflict resolution modal
type SyncConflictModal struct {
	Visible     bool
	Conflicts   []syncpkg.SyncConflict
	Resolutions map[string]string // taskID -> "db" | "markdown" | ""
	Selected    int               // Currently highlighted conflict
}
```

Add fields to `Model` struct (around line 93):

```go
	// Sync modal state
	syncModal    SyncConflictModal
	syncFlash    string
	syncFlashExp time.Time
	syncer       *isync.Syncer
	store        *taskstore.Store
```

**Step 2: Update ModelConfig struct**

Add to `ModelConfig`:

```go
	Store *taskstore.Store // Database store for sync operations
```

**Step 3: Update NewModel to initialize sync fields**

In `NewModel`, add after setting `agentMgr.SetSyncer(syncer)`:

```go
	// Store syncer and store for sync operations
	var syncerRef *isync.Syncer
	var storeRef *taskstore.Store
	if cfg.PlansDir != "" {
		syncerRef = isync.New(cfg.PlansDir)
	}
	storeRef = cfg.Store
```

Add to the Model initialization:

```go
		syncer:          syncerRef,
		store:           storeRef,
		syncModal:       SyncConflictModal{Resolutions: make(map[string]string)},
```

**Step 4: Build to verify no syntax errors**

Run: `go build ./tui`
Expected: Build succeeds

**Step 5: Commit**

```bash
git add tui/model.go
git commit -m "feat(tui): add sync modal state to Model"
```

---

## Task 7: Add Sync Message Types and Handlers

**Files:**
- Modify: `tui/update.go`

**Step 1: Add message types**

Add after `WorkerTestMsg` (around line 88):

```go
// SyncStartMsg triggers a sync operation
type SyncStartMsg struct{}

// SyncCompleteMsg reports sync completion
type SyncCompleteMsg struct {
	Result *isync.SyncResult
	Err    error
}

// SyncResolveMsg reports conflict resolution completion
type SyncResolveMsg struct {
	Err error
}
```

**Step 2: Add import for sync package**

Add to imports:

```go
	isync "github.com/hochfrequenz/claude-plan-orchestrator/internal/sync"
```

**Step 3: Add 's' key handler for Modules tab**

In the `tea.KeyMsg` switch, after the "x" case (around line 215), add:

```go
		case "s":
			// Sync (only on Modules tab when not already syncing)
			if m.activeTab == 3 && !m.syncModal.Visible && m.syncer != nil && m.store != nil {
				m.statusMsg = "Syncing..."
				return m, startSyncCmd(m.syncer, m.store)
			} else if m.syncer == nil || m.store == nil {
				m.statusMsg = "Sync not available (no plans directory or database)"
			}
```

**Step 4: Add modal key handlers**

Add at the start of the `tea.KeyMsg` switch (before other cases):

```go
		// Handle sync modal keys first
		if m.syncModal.Visible {
			switch msg.String() {
			case "d":
				// Resolve current conflict as "db"
				if m.syncModal.Selected < len(m.syncModal.Conflicts) {
					taskID := m.syncModal.Conflicts[m.syncModal.Selected].TaskID
					m.syncModal.Resolutions[taskID] = "db"
				}
				return m, nil
			case "m":
				// Resolve current conflict as "markdown"
				if m.syncModal.Selected < len(m.syncModal.Conflicts) {
					taskID := m.syncModal.Conflicts[m.syncModal.Selected].TaskID
					m.syncModal.Resolutions[taskID] = "markdown"
				}
				return m, nil
			case "a":
				// Resolve all as "db"
				for _, c := range m.syncModal.Conflicts {
					m.syncModal.Resolutions[c.TaskID] = "db"
				}
				return m, nil
			case "j", "down":
				if m.syncModal.Selected < len(m.syncModal.Conflicts)-1 {
					m.syncModal.Selected++
				}
				return m, nil
			case "k", "up":
				if m.syncModal.Selected > 0 {
					m.syncModal.Selected--
				}
				return m, nil
			case "enter":
				// Apply resolutions if all conflicts are resolved
				allResolved := true
				for _, c := range m.syncModal.Conflicts {
					if m.syncModal.Resolutions[c.TaskID] == "" {
						allResolved = false
						break
					}
				}
				if allResolved {
					m.syncModal.Visible = false
					m.statusMsg = "Applying resolutions..."
					return m, applyResolutionsCmd(m.syncer, m.store, m.syncModal.Resolutions)
				}
				return m, nil
			case "esc":
				// Cancel and close modal
				m.syncModal.Visible = false
				m.syncModal.Conflicts = nil
				m.syncModal.Resolutions = make(map[string]string)
				m.syncModal.Selected = 0
				m.statusMsg = "Sync cancelled"
				return m, nil
			case "q", "ctrl+c":
				return m, tea.Quit
			}
			return m, nil // Consume all other keys when modal is open
		}
```

**Step 5: Add message handlers**

Add cases in the main `msg` switch:

```go
	case SyncCompleteMsg:
		if msg.Err != nil {
			m.statusMsg = fmt.Sprintf("Sync failed: %v", msg.Err)
		} else if len(msg.Result.Conflicts) > 0 {
			// Show conflict modal
			m.syncModal.Visible = true
			m.syncModal.Conflicts = msg.Result.Conflicts
			m.syncModal.Resolutions = make(map[string]string)
			m.syncModal.Selected = 0
			m.statusMsg = fmt.Sprintf("%d conflict(s) found", len(msg.Result.Conflicts))
		} else {
			// Success - show flash
			total := msg.Result.MarkdownToDBCount + msg.Result.DBToMarkdownCount
			if total > 0 {
				m.syncFlash = fmt.Sprintf("Synced %d task(s) ✓", total)
			} else {
				m.syncFlash = "Already in sync ✓"
			}
			m.syncFlashExp = time.Now().Add(2 * time.Second)
			m.statusMsg = ""
		}
		return m, nil

	case SyncResolveMsg:
		if msg.Err != nil {
			m.statusMsg = fmt.Sprintf("Resolution failed: %v", msg.Err)
		} else {
			m.syncFlash = "Conflicts resolved ✓"
			m.syncFlashExp = time.Now().Add(2 * time.Second)
			m.statusMsg = ""
			// Recompute module summaries
			m.modules = computeModuleSummaries(m.allTasks)
		}
		return m, nil
```

**Step 6: Add command functions**

Add at end of file:

```go
// startSyncCmd initiates a two-way sync
func startSyncCmd(syncer *isync.Syncer, store *taskstore.Store) tea.Cmd {
	return func() tea.Msg {
		result, err := syncer.TwoWaySync(store)
		return SyncCompleteMsg{Result: result, Err: err}
	}
}

// applyResolutionsCmd applies conflict resolutions
func applyResolutionsCmd(syncer *isync.Syncer, store *taskstore.Store, resolutions map[string]string) tea.Cmd {
	return func() tea.Msg {
		err := syncer.ResolveConflicts(store, resolutions)
		return SyncResolveMsg{Err: err}
	}
}
```

**Step 7: Build to verify**

Run: `go build ./tui`
Expected: Build succeeds

**Step 8: Commit**

```bash
git add tui/update.go
git commit -m "feat(tui): add sync key handlers and message types"
```

---

## Task 8: Add Sync Modal Rendering to View

**Files:**
- Modify: `tui/view.go`

**Step 1: Update Modules tab footer**

Find line 173 (status bar for Modules tab) and update:

```go
	case 3: // Modules
		statusBar = fmt.Sprintf(" [tab]switch [j/k]scroll [x]run tests [s]sync %s [q]uit ", mouseHint)
```

**Step 2: Add flash message rendering**

In the `View()` function, after the status message rendering (around line 146), add:

```go
	// Flash message (if any and not expired)
	if m.syncFlash != "" && time.Now().Before(m.syncFlashExp) {
		flashLine := fmt.Sprintf(" %s ", m.syncFlash)
		b.WriteString(completedStyle.Width(m.width).Render(flashLine))
		b.WriteString("\n")
	}
```

**Step 3: Add modal rendering function**

Add new function:

```go
func (m Model) renderSyncModal() string {
	if !m.syncModal.Visible {
		return ""
	}

	var b strings.Builder

	// Modal dimensions
	modalWidth := 60
	if m.width > 0 && m.width < modalWidth+4 {
		modalWidth = m.width - 4
	}

	// Title
	title := fmt.Sprintf(" Sync Conflicts (%d) ", len(m.syncModal.Conflicts))
	titlePadding := (modalWidth - len(title)) / 2
	b.WriteString(strings.Repeat("─", titlePadding))
	b.WriteString(title)
	b.WriteString(strings.Repeat("─", modalWidth-titlePadding-len(title)))
	b.WriteString("\n\n")

	// Conflicts
	for i, c := range m.syncModal.Conflicts {
		selected := i == m.syncModal.Selected
		resolution := m.syncModal.Resolutions[c.TaskID]

		// Task ID line
		prefix := "  "
		if selected {
			prefix = "> "
		}

		taskLine := fmt.Sprintf("%s%s", prefix, c.TaskID)
		if selected {
			b.WriteString(tabActiveStyle.Render(taskLine))
		} else {
			b.WriteString(taskLine)
		}
		b.WriteString("\n")

		// Status comparison line
		dbStyle := queuedStyle
		mdStyle := queuedStyle
		if resolution == "db" {
			dbStyle = completedStyle
		} else if resolution == "markdown" {
			mdStyle = completedStyle
		}

		statusLine := fmt.Sprintf("    DB: %s    Markdown: %s",
			dbStyle.Render(c.DBStatus),
			mdStyle.Render(c.MarkdownStatus))
		b.WriteString(statusLine)
		b.WriteString("\n")

		// Resolution buttons
		dbBtn := "[d] Use DB"
		mdBtn := "[m] Use Markdown"
		if resolution == "db" {
			dbBtn = completedStyle.Render("[d] Use DB ✓")
		} else if resolution == "markdown" {
			mdBtn = completedStyle.Render("[m] Use Markdown ✓")
		}
		b.WriteString(fmt.Sprintf("    %s      %s\n\n", dbBtn, mdBtn))
	}

	// Footer
	b.WriteString(strings.Repeat("─", modalWidth))
	b.WriteString("\n")

	// Count resolved
	resolved := 0
	for _, c := range m.syncModal.Conflicts {
		if m.syncModal.Resolutions[c.TaskID] != "" {
			resolved++
		}
	}

	footer := fmt.Sprintf(" [a] Use All DB    [Enter] Apply (%d/%d)    [Esc] Cancel ",
		resolved, len(m.syncModal.Conflicts))
	b.WriteString(queuedStyle.Render(footer))

	return b.String()
}
```

**Step 4: Render modal overlay in View()**

In `View()`, before the status bar rendering (around line 148), add:

```go
	// Sync modal overlay
	if m.syncModal.Visible {
		modal := m.renderSyncModal()
		b.WriteString(sectionStyle.Width(m.width - 2).Render(modal))
		b.WriteString("\n")
	}
```

**Step 5: Build to verify**

Run: `go build ./tui`
Expected: Build succeeds

**Step 6: Commit**

```bash
git add tui/view.go
git commit -m "feat(tui): add sync modal rendering and flash message"
```

---

## Task 9: Update TUI Command to Pass Store

**Files:**
- Modify: `cmd/claude-orch/commands.go`

**Step 1: Update runTUI to pass store to ModelConfig**

Find the `runTUI` function and update the `ModelConfig` initialization to include the store:

```go
	model := tui.NewModel(tui.ModelConfig{
		MaxActive:       cfg.General.MaxParallelAgents,
		AllTasks:        tasks,
		Queued:          queued,
		Agents:          nil,
		Flagged:         flagged,
		Workers:         nil,
		ProjectRoot:     cfg.General.ProjectRoot,
		WorktreeDir:     cfg.General.WorktreeDir,
		PlansDir:        plansDir,
		BuildPoolURL:    buildPoolURL,
		AgentManager:    agentMgr,
		WorktreeManager: worktreeMgr,
		RecoveredAgents: recoveredAgents,
		PlanWatcher:     planWatcher,
		PlanChangeChan:  planChangeChan,
		Store:           store, // Add this line
	})
```

Note: `store` should already be defined earlier in `runTUI`. If not, ensure it's opened:

```go
	store, err := taskstore.New(cfg.General.DatabasePath)
	if err != nil {
		return err
	}
	// Don't defer store.Close() here - TUI needs it during operation
```

**Step 2: Build and test**

Run: `go build -o claude-orch ./cmd/claude-orch`
Expected: Build succeeds

**Step 3: Commit**

```bash
git add cmd/claude-orch/commands.go
git commit -m "feat(tui): pass store to TUI for sync operations"
```

---

## Task 10: Run All Tests and Final Verification

**Files:**
- All modified files

**Step 1: Run all tests**

Run: `go test ./... -v`
Expected: All tests pass

**Step 2: Build the binary**

Run: `go build -o claude-orch ./cmd/claude-orch`
Expected: Build succeeds

**Step 3: Manual verification**

Test the TUI manually:
1. Run `./claude-orch tui`
2. Navigate to Modules tab (press `m` or Tab)
3. Press `s` to trigger sync
4. Verify flash message or conflict modal appears

**Step 4: Final commit if any fixes needed**

```bash
git status
# If any uncommitted changes:
git add -A
git commit -m "fix: address test feedback"
```

**Step 5: Push branch**

```bash
git push -u origin feature/tui-sync-button
```
