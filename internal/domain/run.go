package domain

import "time"

// Run represents a single execution attempt of a task
type Run struct {
	ID           string
	TaskID       TaskID
	WorktreePath string
	Branch       string
	Status       RunStatus
	StartedAt    *time.Time
	FinishedAt   *time.Time
	TokensInput  int
	TokensOutput int
}

// PR represents a pull request created from a run
type PR struct {
	ID           int
	RunID        string
	PRNumber     int
	URL          string
	ReviewStatus PRReviewStatus
	MergedAt     *time.Time
}

// LogEntry represents a log message from a run
type LogEntry struct {
	ID        int
	RunID     string
	Timestamp time.Time
	Level     string
	Message   string
}

// Batch represents a scheduled batch of tasks
type Batch struct {
	ID             int
	Name           string
	StartedAt      *time.Time
	FinishedAt     *time.Time
	TasksCompleted int
	TasksFailed    int
}
