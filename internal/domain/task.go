package domain

import (
	"fmt"
	"regexp"
	"strconv"
	"time"
)

var (
	taskIDRegex       = regexp.MustCompile(`^([a-z][a-z0-9-]*)/E(\d+)$`)
	taskIDPrefixRegex = regexp.MustCompile(`^([a-z][a-z0-9-]*)/([A-Z]+)(\d+)$`)
)

// TaskID uniquely identifies a task as module/E{number} or module/PREFIX{number}
type TaskID struct {
	Module  string
	Prefix  string // Optional subsystem prefix (e.g., "CLI", "TUI"), empty for standard epics
	EpicNum int
}

// ParseTaskID parses a string like "technical/E05" or "cli-impl/CLI02" into a TaskID
func ParseTaskID(s string) (TaskID, error) {
	// Try standard format first: module/E##
	if matches := taskIDRegex.FindStringSubmatch(s); matches != nil {
		epicNum, _ := strconv.Atoi(matches[2]) // regex guarantees digits
		return TaskID{Module: matches[1], EpicNum: epicNum}, nil
	}
	// Try prefix format: module/PREFIX##
	if matches := taskIDPrefixRegex.FindStringSubmatch(s); matches != nil {
		epicNum, _ := strconv.Atoi(matches[3])
		return TaskID{Module: matches[1], Prefix: matches[2], EpicNum: epicNum}, nil
	}
	return TaskID{}, fmt.Errorf("invalid task ID format: %q (expected module/E## or module/PREFIX##)", s)
}

// String returns the canonical string representation
func (t TaskID) String() string {
	if t.Prefix != "" {
		return fmt.Sprintf("%s/%s%02d", t.Module, t.Prefix, t.EpicNum)
	}
	return fmt.Sprintf("%s/E%02d", t.Module, t.EpicNum)
}

// TestSummary holds test execution results for a completed epic
type TestSummary struct {
	Tests      int      `json:"tests"`
	Passed     int      `json:"passed"`
	Failed     int      `json:"failed"`
	Skipped    int      `json:"skipped"`
	Coverage   string   `json:"coverage,omitempty"`
	FilesTested []string `json:"files_tested,omitempty"`
}

// Task represents a unit of work parsed from an epic markdown file
type Task struct {
	ID          TaskID
	Title       string
	Description string
	Status      TaskStatus
	Priority    Priority
	DependsOn   []TaskID
	NeedsReview bool
	FilePath    string
	TestSummary *TestSummary
	GitHubIssue *int // Source GitHub issue number, nil if not from issue
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// IsReady returns true if all dependencies are in the completed set
func (t *Task) IsReady(completed map[string]bool) bool {
	if t.Status != StatusNotStarted {
		return false
	}
	for _, dep := range t.DependsOn {
		if !completed[dep.String()] {
			return false
		}
	}
	return true
}

// ImplicitDependency returns the previous epic in the same module (and prefix), if any
func (t *Task) ImplicitDependency() *TaskID {
	if t.ID.EpicNum == 0 {
		return nil
	}
	dep := TaskID{Module: t.ID.Module, Prefix: t.ID.Prefix, EpicNum: t.ID.EpicNum - 1}
	return &dep
}

// HasGitHubIssue returns true if this task originated from a GitHub issue.
func (t *Task) HasGitHubIssue() bool {
	return t.GitHubIssue != nil
}
