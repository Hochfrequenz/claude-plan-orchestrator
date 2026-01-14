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
func (s *Syncer) GitPull() error {
	s.gitMu.Lock()
	defer s.gitMu.Unlock()

	cmd := exec.Command("git", "pull", "--rebase")
	cmd.Dir = s.projectRoot
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git pull failed: %w\n%s", err, output)
	}
	return nil
}

// GitCommitAndPush commits and pushes the README and epic status changes
// Uses mutex to prevent concurrent git operations
func (s *Syncer) GitCommitAndPush(taskID domain.TaskID, status domain.TaskStatus, epicFilePath string) error {
	s.gitMu.Lock()
	defer s.gitMu.Unlock()

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

	// Push
	cmd = exec.Command("git", "push")
	cmd.Dir = root
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git push failed: %w\n%s", err, output)
	}

	return nil
}
