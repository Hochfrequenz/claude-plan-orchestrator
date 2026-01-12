package observer

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

// PlanChangeCallback is called when plan files change
// worktreePath is the root of the worktree where changes occurred
type PlanChangeCallback func(worktreePath string, changedFiles []string)

// PlanWatcher monitors worktrees for changes to plan/epic files
type PlanWatcher struct {
	watcher  *fsnotify.Watcher
	callback PlanChangeCallback
	debounce time.Duration

	// Track watched worktrees
	worktrees map[string]struct{}

	// Debounce state - track by worktree
	pendingByWorktree map[string]map[string]struct{}
	timer             *time.Timer
	mu                sync.Mutex

	cancel context.CancelFunc
}

// NewPlanWatcher creates a new watcher for worktree plan files
func NewPlanWatcher(callback PlanChangeCallback) (*PlanWatcher, error) {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}

	pw := &PlanWatcher{
		watcher:           watcher,
		callback:          callback,
		debounce:          500 * time.Millisecond, // Debounce rapid changes
		worktrees:         make(map[string]struct{}),
		pendingByWorktree: make(map[string]map[string]struct{}),
	}

	return pw, nil
}

// AddWorktree starts watching a worktree's docs/plans directory
func (pw *PlanWatcher) AddWorktree(worktreePath string) error {
	pw.mu.Lock()
	defer pw.mu.Unlock()

	if _, exists := pw.worktrees[worktreePath]; exists {
		return nil // Already watching
	}

	plansDir := filepath.Join(worktreePath, "docs", "plans")

	// Check if plans directory exists
	if _, err := os.Stat(plansDir); os.IsNotExist(err) {
		return nil // No plans directory, nothing to watch
	}

	// Add the plans directory and all subdirectories
	err := filepath.Walk(plansDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // Skip errors
		}
		if info.IsDir() {
			return pw.watcher.Add(path)
		}
		return nil
	})
	if err != nil {
		return err
	}

	pw.worktrees[worktreePath] = struct{}{}
	return nil
}

// RemoveWorktree stops watching a worktree
func (pw *PlanWatcher) RemoveWorktree(worktreePath string) {
	pw.mu.Lock()
	defer pw.mu.Unlock()

	if _, exists := pw.worktrees[worktreePath]; !exists {
		return
	}

	plansDir := filepath.Join(worktreePath, "docs", "plans")

	// Remove all watches under this worktree
	filepath.Walk(plansDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() {
			pw.watcher.Remove(path)
		}
		return nil
	})

	delete(pw.worktrees, worktreePath)
	delete(pw.pendingByWorktree, worktreePath)
}

// Start begins watching for file changes
func (pw *PlanWatcher) Start(ctx context.Context) {
	ctx, pw.cancel = context.WithCancel(ctx)

	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case event, ok := <-pw.watcher.Events:
				if !ok {
					return
				}
				pw.handleEvent(event)
			case _, ok := <-pw.watcher.Errors:
				if !ok {
					return
				}
				// Log error but continue watching
			}
		}
	}()
}

// Stop stops watching for file changes
func (pw *PlanWatcher) Stop() {
	if pw.cancel != nil {
		pw.cancel()
	}
	pw.watcher.Close()
}

func (pw *PlanWatcher) handleEvent(event fsnotify.Event) {
	// Only care about markdown files
	if !strings.HasSuffix(event.Name, ".md") {
		return
	}

	// Only care about writes and creates
	if event.Op&(fsnotify.Write|fsnotify.Create) == 0 {
		return
	}

	pw.mu.Lock()
	defer pw.mu.Unlock()

	// Find which worktree this file belongs to
	worktreePath := pw.findWorktree(event.Name)
	if worktreePath == "" {
		return // Not in a watched worktree
	}

	// Add to pending files for this worktree
	if pw.pendingByWorktree[worktreePath] == nil {
		pw.pendingByWorktree[worktreePath] = make(map[string]struct{})
	}
	pw.pendingByWorktree[worktreePath][event.Name] = struct{}{}

	// Reset or start debounce timer
	if pw.timer != nil {
		pw.timer.Stop()
	}
	pw.timer = time.AfterFunc(pw.debounce, pw.flush)
}

// findWorktree returns the worktree path that contains the given file path
func (pw *PlanWatcher) findWorktree(filePath string) string {
	for wt := range pw.worktrees {
		if strings.HasPrefix(filePath, wt) {
			return wt
		}
	}
	return ""
}

func (pw *PlanWatcher) flush() {
	pw.mu.Lock()
	// Copy pending state and clear
	pending := pw.pendingByWorktree
	pw.pendingByWorktree = make(map[string]map[string]struct{})
	pw.mu.Unlock()

	if pw.callback == nil {
		return
	}

	// Call callback for each worktree with changes
	for worktreePath, fileMap := range pending {
		files := make([]string, 0, len(fileMap))
		for f := range fileMap {
			files = append(files, f)
		}
		if len(files) > 0 {
			pw.callback(worktreePath, files)
		}
	}
}

// SetDebounce sets the debounce duration for batching file changes
func (pw *PlanWatcher) SetDebounce(d time.Duration) {
	pw.mu.Lock()
	defer pw.mu.Unlock()
	pw.debounce = d
}
