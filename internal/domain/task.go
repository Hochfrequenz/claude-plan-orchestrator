package domain

import (
	"fmt"
	"regexp"
	"strconv"
	"time"
)

var taskIDRegex = regexp.MustCompile(`^([a-z][a-z0-9-]*)/E(\d+)$`)

// TaskID uniquely identifies a task as module/E{number}
type TaskID struct {
	Module  string
	EpicNum int
}

// ParseTaskID parses a string like "technical/E05" into a TaskID
func ParseTaskID(s string) (TaskID, error) {
	matches := taskIDRegex.FindStringSubmatch(s)
	if matches == nil {
		return TaskID{}, fmt.Errorf("invalid task ID format: %q (expected module/E##)", s)
	}
	epicNum, _ := strconv.Atoi(matches[2]) // regex guarantees digits
	return TaskID{Module: matches[1], EpicNum: epicNum}, nil
}

// String returns the canonical string representation
func (t TaskID) String() string {
	return fmt.Sprintf("%s/E%02d", t.Module, t.EpicNum)
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

// ImplicitDependency returns the previous epic in the same module, if any
func (t *Task) ImplicitDependency() *TaskID {
	if t.ID.EpicNum == 0 {
		return nil
	}
	dep := TaskID{Module: t.ID.Module, EpicNum: t.ID.EpicNum - 1}
	return &dep
}
