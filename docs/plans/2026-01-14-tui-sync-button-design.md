# TUI Sync Button Design

**Date**: 2026-01-14
**Status**: Draft

## Overview

Add a sync button to the Modules tab in the TUI that performs two-way synchronization between epic markdown files (including README.md) and the SQLite database, with conflict detection and resolution.

## User Interaction Flow

### Triggering Sync

In the Modules tab footer, alongside the existing `[x] Run Tests` button, add `[s] Sync`. Pressing `s` initiates the two-way sync process.

### Happy Path (No Conflicts)

1. User presses `s`
2. Spinner appears briefly in status bar: "Syncing..."
3. Sync completes with flash message: "Synced ✓" (auto-dismisses after 2 seconds)

### Conflict Path

1. User presses `s`
2. Spinner: "Syncing..."
3. Conflicts detected → Modal appears:

```
┌─────────────── Sync Conflicts (3) ───────────────┐
│                                                  │
│  technical/E05                                   │
│    DB: complete    Markdown: in_progress         │
│    [d] Use DB      [m] Use Markdown              │
│                                                  │
│  foundation/E02                                  │
│    DB: in_progress Markdown: complete            │
│    [d] Use DB      [m] Use Markdown              │
│                                                  │
│  api/E01                                         │
│    DB: not_started Markdown: in_progress         │
│    [d] Use DB      [m] Use Markdown              │
│                                                  │
├──────────────────────────────────────────────────┤
│  [a] Use All DB    [Enter] Apply    [Esc] Cancel │
└──────────────────────────────────────────────────┘
```

User resolves each conflict with `d`/`m`, then presses Enter to apply or Esc to cancel.

## Centralized Sync Logic

### Current State

- `claude-orch sync` CLI: Parses markdown → upserts to DB (logic in `cmd/claude-orch/commands.go`)
- `syncer.SyncTaskStatus()`: Updates markdown from DB (agent completion flow)

These are separate, one-way operations.

### Proposed API

Move all sync logic into `internal/sync/` package with a unified API:

```go
// internal/sync/sync.go (extended)

type SyncResult struct {
    MarkdownToDBCount int              // Tasks updated in DB from markdown
    DBToMarkdownCount int              // Tasks updated in markdown from DB
    Conflicts         []SyncConflict   // Mismatches requiring resolution
}

type SyncConflict struct {
    TaskID         string
    DBStatus       string
    MarkdownStatus string
}

// Full two-way sync with conflict detection
func (s *Syncer) TwoWaySync(store *taskstore.Store) (*SyncResult, error)

// Apply user resolutions: map[taskID] = "db" | "markdown"
func (s *Syncer) ResolveConflicts(store *taskstore.Store, resolutions map[string]string) error

// One-way helpers (used internally and by existing code)
func (s *Syncer) SyncMarkdownToDB(store *taskstore.Store) (int, error)
func (s *Syncer) SyncDBToMarkdown(store *taskstore.Store) (int, error)
```

### Sync Algorithm

```
1. Git pull (with stash) to get latest markdown
2. Parse all epic files → extract status from frontmatter
3. Query all tasks from database
4. Compare each task:
   - Match by task ID (e.g., "technical/E05")
   - If DB status == markdown status → no action
   - If only one side has the task → sync to other side (no conflict)
   - If both exist but differ → conflict
5. If conflicts exist → return conflicts for modal
6. If no conflicts → apply all changes:
   - DB→Markdown: Update frontmatter + README emoji
   - Markdown→DB: Update task status in SQLite
7. Git commit & push (if markdown changed)
8. Return success
```

### CLI Command Update

`claude-orch sync` becomes a thin wrapper calling `syncer.TwoWaySync()`.

## TUI Implementation

### New Modal Component

```go
// tui/model.go

type SyncConflictModal struct {
    Visible     bool
    Conflicts   []sync.SyncConflict
    Resolutions map[string]string  // taskID → "db" | "markdown" | "" (unresolved)
    Selected    int                // Currently highlighted conflict
}

type Model struct {
    // ... existing fields
    syncModal    SyncConflictModal
    syncFlash    string    // Flash message text
    syncFlashExp time.Time // When flash expires
}
```

### Key Handling

```go
// When modal is visible, capture keys for resolution
if m.syncModal.Visible {
    switch msg.String() {
    case "d":      // Resolve current as "db"
    case "m":      // Resolve current as "markdown"
    case "a":      // Resolve all as "db"
    case "j","down": // Navigate conflicts
    case "k","up":
    case "enter":  // Apply resolutions (if all resolved)
    case "esc":    // Cancel and close modal
    }
    return m, nil  // Consume keys when modal open
}

// In Modules tab (activeTab == 3)
case "s":
    return m, m.startSync()  // Returns Cmd that triggers sync
```

### Footer Update

Modules tab footer changes from:
```
[x] Run Tests
```
to:
```
[x] Run Tests  [s] Sync
```

## File Changes

| File | Changes |
|------|---------|
| `internal/sync/sync.go` | Add `TwoWaySync()`, `ResolveConflicts()`, `SyncMarkdownToDB()`, `SyncDBToMarkdown()` |
| `cmd/claude-orch/commands.go` | Refactor `sync` command to use `syncer.TwoWaySync()` |
| `tui/model.go` | Add `SyncConflictModal` struct, `syncFlash` fields |
| `tui/update.go` | Add `s` key handler, modal key handling, `startSync()` cmd |
| `tui/view.go` | Add modal rendering, flash message rendering, update footer |

## Edge Cases

- **Git conflicts during pull**: Show error in flash message, don't proceed with sync
- **No tasks in DB or markdown**: Treat as one-way sync (no conflicts possible)
- **Partial resolution**: Enter key disabled until all conflicts resolved; show count in modal footer
- **Sync already in progress**: Disable `s` key while syncing (show spinner)

## Status Mapping

Ensure consistent mapping between DB and markdown statuses:

| DB | Markdown Frontmatter | README Emoji |
|----|---------------------|--------------|
| not_started | not_started | 🔴 |
| in_progress | in_progress | 🟡 |
| complete | complete | 🟢 |
| failed | failed | 🔴 |
