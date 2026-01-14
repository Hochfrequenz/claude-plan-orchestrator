package sync

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	gosync "sync"

	"github.com/hochfrequenz/claude-plan-orchestrator/internal/domain"
	"github.com/hochfrequenz/claude-plan-orchestrator/internal/parser"
	"github.com/hochfrequenz/claude-plan-orchestrator/internal/taskstore"
)

// Syncer handles status synchronization back to markdown files
type Syncer struct {
	plansDir    string
	projectRoot string
	gitMu       gosync.Mutex // Mutex for git operations to prevent concurrent access
}

// New creates a new Syncer
func New(plansDir string) *Syncer {
	// Project root is parent of docs/plans
	projectRoot := filepath.Dir(filepath.Dir(plansDir))
	return &Syncer{
		plansDir:    plansDir,
		projectRoot: projectRoot,
	}
}

// StatusEmoji returns the emoji for a task status
func StatusEmoji(status domain.TaskStatus) string {
	switch status {
	case domain.StatusNotStarted:
		return "🔴"
	case domain.StatusInProgress:
		return "🟡"
	case domain.StatusComplete:
		return "🟢"
	default:
		return "🔴"
	}
}

// UpdateTaskStatus updates the task status in README.md at the project root
func (s *Syncer) UpdateTaskStatus(taskID domain.TaskID, status domain.TaskStatus) error {
	// README.md is at project root, not in docs/plans
	readmePath := filepath.Join(s.projectRoot, "README.md")
	content, err := os.ReadFile(readmePath)
	if err != nil {
		return err
	}

	// Pattern to match epic row in format: | [E00](link) | Description | 🔴 |
	// The epic number can be E00, E01, E1, E2, etc.
	// We match: | [E{num}](...) | ... | {emoji} |
	epicNum := taskID.EpicNum
	// Try both formats: E00 (zero-padded) and E0 (not padded)
	patterns := []string{
		fmt.Sprintf(`(\| \[E%02d\]\([^)]+\) \|[^|]+\| )[🔴🟡🟢]( \|)`, epicNum),
		fmt.Sprintf(`(\| \[E%d\]\([^)]+\) \|[^|]+\| )[🔴🟡🟢]( \|)`, epicNum),
	}

	newEmoji := StatusEmoji(status)
	contentStr := string(content)

	// Find the module section and update within it
	for _, pattern := range patterns {
		re := regexp.MustCompile(pattern)
		replacement := fmt.Sprintf("${1}%s${2}", newEmoji)
		updated := updateInModuleSection(contentStr, taskID.Module, re, replacement)
		if updated != contentStr {
			contentStr = updated
			break
		}
	}

	return os.WriteFile(readmePath, []byte(contentStr), 0644)
}

func updateInModuleSection(content, module string, re *regexp.Regexp, replacement string) string {
	// Normalize module name for matching (e.g., "technical" matches "Technical Module")
	// Handle both "### Technical Module" and "## technical-module" style headers
	moduleLower := strings.ToLower(module)
	moduleLower = strings.TrimSuffix(moduleLower, "-module")

	lines := strings.Split(content, "\n")

	inModule := false
	var result []string

	for _, line := range lines {
		// Check for section headers (## or ###)
		if strings.HasPrefix(line, "## ") || strings.HasPrefix(line, "### ") {
			lineLower := strings.ToLower(line)
			// Check if this header contains the module name
			inModule = strings.Contains(lineLower, moduleLower)
		}

		if inModule && re.MatchString(line) {
			line = re.ReplaceAllString(line, replacement)
		}
		result = append(result, line)
	}

	return strings.Join(result, "\n")
}

// UpdateEpicStatus adds a status comment to an epic file
func (s *Syncer) UpdateEpicStatus(epicPath string, status domain.TaskStatus, prNumber int, mergedDate string) error {
	content, err := os.ReadFile(epicPath)
	if err != nil {
		return err
	}

	statusComment := fmt.Sprintf("<!-- erp-orchestrator: status=%s, pr=#%d, merged=%s -->\n",
		status, prNumber, mergedDate)

	// Check if comment already exists
	if strings.Contains(string(content), "<!-- erp-orchestrator:") {
		// Update existing comment
		re := regexp.MustCompile(`<!-- erp-orchestrator:[^>]+ -->\n?`)
		content = re.ReplaceAll(content, []byte(statusComment))
	} else {
		// Add before first heading
		re := regexp.MustCompile(`(# )`)
		loc := re.FindIndex(content)
		if loc != nil {
			newContent := make([]byte, 0, len(content)+len(statusComment))
			newContent = append(newContent, content[:loc[0]]...)
			newContent = append(newContent, []byte(statusComment)...)
			newContent = append(newContent, content[loc[0]:]...)
			content = newContent
		} else {
			content = append([]byte(statusComment), content...)
		}
	}

	return os.WriteFile(epicPath, content, 0644)
}

// SyncAll updates all task statuses from the store to markdown
func (s *Syncer) SyncAll(tasks []*domain.Task) error {
	for _, task := range tasks {
		if err := s.UpdateTaskStatus(task.ID, task.Status); err != nil {
			// Log but continue
			fmt.Printf("Warning: failed to sync %s: %v\n", task.ID.String(), err)
		}
	}
	return nil
}

// UpdateEpicFrontmatter updates the status field in an epic file's YAML frontmatter.
// If the file has no frontmatter, it adds one with the status field.
func (s *Syncer) UpdateEpicFrontmatter(epicPath string, status domain.TaskStatus) error {
	content, err := os.ReadFile(epicPath)
	if err != nil {
		return err
	}

	contentStr := string(content)

	// Check if file has frontmatter (starts with ---)
	if !strings.HasPrefix(contentStr, "---") {
		// No frontmatter - add one with status
		newFrontmatter := fmt.Sprintf("---\nstatus: %s\n---\n", status)
		return os.WriteFile(epicPath, []byte(newFrontmatter+contentStr), 0644)
	}

	// Find the end of frontmatter
	endIdx := strings.Index(contentStr[3:], "\n---")
	if endIdx == -1 {
		return fmt.Errorf("epic file %s has malformed frontmatter", epicPath)
	}
	endIdx += 3 // Adjust for the initial offset

	frontmatter := contentStr[:endIdx]
	rest := contentStr[endIdx:]

	// Update or add status field in frontmatter
	statusPattern := regexp.MustCompile(`(?m)^status:\s*\S+`)
	newStatus := fmt.Sprintf("status: %s", status)

	if statusPattern.MatchString(frontmatter) {
		frontmatter = statusPattern.ReplaceAllString(frontmatter, newStatus)
	} else {
		// Add status before closing ---
		frontmatter = frontmatter + "\n" + newStatus
	}

	return os.WriteFile(epicPath, []byte(frontmatter+rest), 0644)
}

// GitPull pulls the latest changes from the remote
// Uses mutex to prevent concurrent git operations
// Deprecated: Use SyncTaskStatus for atomic sync operations
func (s *Syncer) GitPull() error {
	s.gitMu.Lock()
	defer s.gitMu.Unlock()

	return s.gitPullLocked()
}

func (s *Syncer) gitPullLocked() error {
	// Stash any local changes first to avoid rebase conflicts
	cmd := exec.Command("git", "stash", "--include-untracked")
	cmd.Dir = s.projectRoot
	stashOutput, _ := cmd.CombinedOutput()
	hasStash := !strings.Contains(string(stashOutput), "No local changes")

	// Pull with rebase
	cmd = exec.Command("git", "pull", "--rebase")
	cmd.Dir = s.projectRoot
	output, err := cmd.CombinedOutput()

	// Pop stash if we stashed something
	if hasStash {
		popCmd := exec.Command("git", "stash", "pop")
		popCmd.Dir = s.projectRoot
		popCmd.CombinedOutput() // Ignore errors - stash might conflict
	}

	if err != nil {
		return fmt.Errorf("git pull failed: %w\n%s", err, output)
	}
	return nil
}

// GitCommitAndPush commits and pushes the README and epic status changes
// Uses mutex to prevent concurrent git operations
// Deprecated: Use SyncTaskStatus for atomic sync operations
func (s *Syncer) GitCommitAndPush(taskID domain.TaskID, status domain.TaskStatus, epicFilePath string) error {
	s.gitMu.Lock()
	defer s.gitMu.Unlock()

	return s.gitCommitAndPushLocked(taskID, status, epicFilePath)
}

func (s *Syncer) gitCommitAndPushLocked(taskID domain.TaskID, status domain.TaskStatus, epicFilePath string) error {
	root := s.projectRoot

	// Stage README.md at project root
	readmePath := filepath.Join(root, "README.md")
	relReadme, _ := filepath.Rel(root, readmePath)

	// Build list of files to stage
	filesToStage := []string{relReadme}

	// Also stage the epic file if provided
	if epicFilePath != "" {
		relEpic, err := filepath.Rel(root, epicFilePath)
		if err == nil {
			filesToStage = append(filesToStage, relEpic)
		}
	}

	// Stage all files
	args := append([]string{"add"}, filesToStage...)
	cmd := exec.Command("git", args...)
	cmd.Dir = root
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git add failed: %w\n%s", err, output)
	}

	// Check if there are staged changes
	cmd = exec.Command("git", "diff", "--cached", "--quiet")
	cmd.Dir = root
	if err := cmd.Run(); err == nil {
		// No changes staged, nothing to commit
		return nil
	}

	// Commit
	statusStr := "in_progress"
	if status == domain.StatusComplete {
		statusStr = "complete"
	}
	commitMsg := fmt.Sprintf("chore: update %s status to %s", taskID.String(), statusStr)

	cmd = exec.Command("git", "commit", "-m", commitMsg)
	cmd.Dir = root
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git commit failed: %w\n%s", err, output)
	}

	// Push (with retry on rejection)
	for i := 0; i < 3; i++ {
		cmd = exec.Command("git", "push")
		cmd.Dir = root
		output, err := cmd.CombinedOutput()
		if err == nil {
			return nil
		}
		// If push was rejected, pull and retry
		if strings.Contains(string(output), "rejected") || strings.Contains(string(output), "non-fast-forward") {
			s.gitPullLocked()
			continue
		}
		return fmt.Errorf("git push failed: %w\n%s", err, output)
	}
	return fmt.Errorf("git push failed after 3 retries")
}

// SyncTaskStatus atomically syncs task status: pull, update files, commit, push
// This holds the mutex for the entire operation to prevent race conditions
func (s *Syncer) SyncTaskStatus(taskID domain.TaskID, status domain.TaskStatus, epicFilePath string) error {
	s.gitMu.Lock()
	defer s.gitMu.Unlock()

	// 1. Pull latest changes (with stash to handle any uncommitted changes)
	if err := s.gitPullLocked(); err != nil {
		// Log but continue - we'll try to commit our changes anyway
		fmt.Printf("Warning: git pull failed: %v\n", err)
	}

	// 2. Update epic frontmatter
	if epicFilePath != "" {
		if err := s.UpdateEpicFrontmatter(epicFilePath, status); err != nil {
			fmt.Printf("Warning: failed to update epic frontmatter: %v\n", err)
		}
	}

	// 3. Update README status
	if err := s.UpdateTaskStatus(taskID, status); err != nil {
		fmt.Printf("Warning: failed to update README status: %v\n", err)
	}

	// 4. Commit and push
	return s.gitCommitAndPushLocked(taskID, status, epicFilePath)
}

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

// ResolveConflicts applies user resolutions to sync conflicts.
// resolutions maps taskID to "db" or "markdown" indicating which source wins.
func (s *Syncer) ResolveConflicts(store *taskstore.Store, resolutions map[string]string) error {
	for taskID, resolution := range resolutions {
		// Parse task ID
		tid, err := domain.ParseTaskID(taskID)
		if err != nil {
			return fmt.Errorf("parsing task ID %s: %w", taskID, err)
		}

		// Get task from DB
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
			_ = dbTask // suppress unused variable warning
			continue
		}
	}

	// Tasks in both -> update DB with markdown data (preserving DB status) and check for conflicts
	for id, dbTask := range dbStatuses {
		mdTask, exists := mdStatuses[id]
		if !exists {
			continue
		}

		// Always update DB with markdown data (dependencies, title, priority, etc.)
		// but preserve the DB status since agents may have updated it
		taskToUpsert := *mdTask
		taskToUpsert.Status = dbTask.Status // Preserve DB status
		if err := store.UpsertTask(&taskToUpsert); err != nil {
			return nil, fmt.Errorf("updating %s: %w", id, err)
		}
		result.MarkdownToDBCount++

		// Flag status conflicts for user resolution
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
