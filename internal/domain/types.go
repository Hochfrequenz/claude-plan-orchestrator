package domain

// TaskStatus represents the lifecycle state of a task
type TaskStatus string

const (
	StatusNotStarted TaskStatus = "not_started"
	StatusInProgress TaskStatus = "in_progress"
	StatusComplete   TaskStatus = "complete"
)

// RunStatus represents the execution state of a run
type RunStatus string

const (
	RunQueued    RunStatus = "queued"
	RunRunning   RunStatus = "running"
	RunCompleted RunStatus = "completed"
	RunFailed    RunStatus = "failed"
	RunStuck     RunStatus = "stuck"
)

// PRReviewStatus represents the PR review state
type PRReviewStatus string

const (
	PRPending  PRReviewStatus = "pending"
	PRApproved PRReviewStatus = "approved"
	PRMerged   PRReviewStatus = "merged"
	PRClosed   PRReviewStatus = "closed"
)

// Priority represents task priority
type Priority string

const (
	PriorityHigh   Priority = "high"
	PriorityNormal Priority = ""
	PriorityLow    Priority = "low"
)
