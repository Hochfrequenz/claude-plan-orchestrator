package sync

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/anthropics/erp-orchestrator/internal/domain"
)

// Syncer handles status synchronization back to markdown files
type Syncer struct {
	plansDir string
}

// New creates a new Syncer
func New(plansDir string) *Syncer {
	return &Syncer{plansDir: plansDir}
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

// UpdateTaskStatus updates the task status in README.md
func (s *Syncer) UpdateTaskStatus(taskID domain.TaskID, status domain.TaskStatus) error {
	readmePath := filepath.Join(s.plansDir, "README.md")
	content, err := os.ReadFile(readmePath)
	if err != nil {
		return err
	}

	// Pattern to match epic row: | E{num} | {emoji} |
	pattern := fmt.Sprintf(`(\| E%02d \| )[🔴🟡🟢]( \|)`, taskID.EpicNum)
	re := regexp.MustCompile(pattern)

	newEmoji := StatusEmoji(status)
	replacement := fmt.Sprintf("${1}%s${2}", newEmoji)

	// Find the module section and update within it
	updated := updateInModuleSection(string(content), taskID.Module, re, replacement)

	return os.WriteFile(readmePath, []byte(updated), 0644)
}

func updateInModuleSection(content, module string, re *regexp.Regexp, replacement string) string {
	// Find module header (## Module Name or ## module-name Module)
	modulePattern := regexp.MustCompile(fmt.Sprintf(`(?i)##\s+%s[- ]?module`, module))
	lines := strings.Split(content, "\n")

	inModule := false
	var result []string

	for _, line := range lines {
		if strings.HasPrefix(line, "## ") {
			inModule = modulePattern.MatchString(line)
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
