package taskstore

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/hochfrequenz/claude-plan-orchestrator/internal/domain"
	_ "modernc.org/sqlite"
)

// Store provides SQLite-backed task persistence
type Store struct {
	db *sql.DB
}

// New creates a new Store with the given database path
func New(dbPath string) (*Store, error) {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, err
	}

	// Enable foreign keys
	if _, err := db.Exec("PRAGMA foreign_keys = ON"); err != nil {
		return nil, err
	}

	// Set busy timeout to 5 seconds - prevents SQLITE_BUSY errors when multiple
	// goroutines try to write simultaneously (e.g., agent updates + TUI refresh)
	if _, err := db.Exec("PRAGMA busy_timeout = 5000"); err != nil {
		return nil, err
	}

	// Run migrations
	if _, err := db.Exec(schema); err != nil {
		return nil, fmt.Errorf("running migrations: %w", err)
	}

	// Run additional migrations (ignore errors for already-applied migrations)
	db.Exec(migrationAddSessionID)

	// Run github_issues migration
	if _, err := db.Exec(migrationGitHubIssues); err != nil {
		return nil, fmt.Errorf("github_issues migration: %w", err)
	}

	// Add github_issue column to tasks (ignore error if already exists)
	db.Exec(migrationTasksGitHubIssue)

	// Add index on tasks.github_issue for query performance
	db.Exec(migrationTasksGitHubIssueIndex)

	// Add prefix column to tasks for subsystem prefixes (CLI, TUI, etc.)
	db.Exec(migrationAddTaskPrefix)

	return &Store{db: db}, nil
}

// Close closes the database connection
func (s *Store) Close() error {
	return s.db.Close()
}

// UpsertTask inserts or updates a task
func (s *Store) UpsertTask(task *domain.Task) error {
	depsJSON, err := json.Marshal(task.DependsOn)
	if err != nil {
		return err
	}

	_, err = s.db.Exec(`
		INSERT INTO tasks (id, module, prefix, epic_num, title, description, status, priority, depends_on, needs_review, file_path, github_issue, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			module = excluded.module,
			prefix = excluded.prefix,
			epic_num = excluded.epic_num,
			title = excluded.title,
			description = excluded.description,
			status = excluded.status,
			priority = excluded.priority,
			depends_on = excluded.depends_on,
			needs_review = excluded.needs_review,
			file_path = excluded.file_path,
			github_issue = excluded.github_issue,
			updated_at = excluded.updated_at
	`,
		task.ID.String(),
		task.ID.Module,
		task.ID.Prefix,
		task.ID.EpicNum,
		task.Title,
		task.Description,
		string(task.Status),
		string(task.Priority),
		string(depsJSON),
		task.NeedsReview,
		task.FilePath,
		task.GitHubIssue,
		task.CreatedAt,
		task.UpdatedAt,
	)
	return err
}

// GetTask retrieves a task by ID
func (s *Store) GetTask(id string) (*domain.Task, error) {
	row := s.db.QueryRow(`
		SELECT id, module, prefix, epic_num, title, description, status, priority, depends_on, needs_review, file_path, github_issue, created_at, updated_at
		FROM tasks WHERE id = ?
	`, id)

	return scanTask(row)
}

// ListOptions specifies filters for listing tasks
type ListOptions struct {
	Module string
	Status domain.TaskStatus
}

// ListTasks returns tasks matching the given options
func (s *Store) ListTasks(opts ListOptions) ([]*domain.Task, error) {
	query := `SELECT id, module, prefix, epic_num, title, description, status, priority, depends_on, needs_review, file_path, github_issue, created_at, updated_at FROM tasks WHERE 1=1`
	var args []interface{}

	if opts.Module != "" {
		query += " AND module = ?"
		args = append(args, opts.Module)
	}
	if opts.Status != "" {
		query += " AND status = ?"
		args = append(args, string(opts.Status))
	}

	query += " ORDER BY module, epic_num"

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tasks []*domain.Task
	for rows.Next() {
		task, err := scanTaskRows(rows)
		if err != nil {
			return nil, err
		}
		tasks = append(tasks, task)
	}

	return tasks, rows.Err()
}

// UpdateTaskStatus updates a task's status
func (s *Store) UpdateTaskStatus(id string, status domain.TaskStatus) error {
	_, err := s.db.Exec(`UPDATE tasks SET status = ?, updated_at = ? WHERE id = ?`,
		string(status), time.Now(), id)
	return err
}

// GetCompletedTaskIDs returns a set of completed task IDs
func (s *Store) GetCompletedTaskIDs() (map[string]bool, error) {
	rows, err := s.db.Query(`SELECT id FROM tasks WHERE status = ?`, string(domain.StatusComplete))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	completed := make(map[string]bool)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		completed[id] = true
	}
	return completed, rows.Err()
}

func scanTask(row *sql.Row) (*domain.Task, error) {
	var task domain.Task
	var id, module, prefix string
	var epicNum int
	var status, priority, depsJSON string
	var description sql.NullString
	var githubIssue sql.NullInt64

	err := row.Scan(&id, &module, &prefix, &epicNum, &task.Title, &description, &status, &priority, &depsJSON, &task.NeedsReview, &task.FilePath, &githubIssue, &task.CreatedAt, &task.UpdatedAt)
	if err != nil {
		return nil, err
	}

	task.ID = domain.TaskID{Module: module, Prefix: prefix, EpicNum: epicNum}
	task.Status = domain.TaskStatus(status)
	task.Priority = domain.Priority(priority)
	if description.Valid {
		task.Description = description.String
	}
	if githubIssue.Valid {
		gi := int(githubIssue.Int64)
		task.GitHubIssue = &gi
	}

	if depsJSON != "" && depsJSON != "null" {
		var deps []domain.TaskID
		if err := json.Unmarshal([]byte(depsJSON), &deps); err != nil {
			return nil, err
		}
		task.DependsOn = deps
	}

	return &task, nil
}

func scanTaskRows(rows *sql.Rows) (*domain.Task, error) {
	var task domain.Task
	var id, module, prefix string
	var epicNum int
	var status, priority, depsJSON string
	var description sql.NullString
	var githubIssue sql.NullInt64

	err := rows.Scan(&id, &module, &prefix, &epicNum, &task.Title, &description, &status, &priority, &depsJSON, &task.NeedsReview, &task.FilePath, &githubIssue, &task.CreatedAt, &task.UpdatedAt)
	if err != nil {
		return nil, err
	}

	task.ID = domain.TaskID{Module: module, Prefix: prefix, EpicNum: epicNum}
	task.Status = domain.TaskStatus(status)
	task.Priority = domain.Priority(priority)
	if description.Valid {
		task.Description = description.String
	}
	if githubIssue.Valid {
		gi := int(githubIssue.Int64)
		task.GitHubIssue = &gi
	}

	if depsJSON != "" && depsJSON != "null" {
		var deps []domain.TaskID
		if err := json.Unmarshal([]byte(depsJSON), &deps); err != nil {
			return nil, err
		}
		task.DependsOn = deps
	}

	return &task, nil
}

// AgentRun represents a running or completed agent execution
type AgentRun struct {
	ID           string
	TaskID       string
	WorktreePath string
	LogPath      string
	PID          int
	Status       string // "running", "completed", "failed"
	StartedAt    time.Time
	FinishedAt   *time.Time
	ErrorMessage string
	SessionID    string // Claude Code session ID for resume capability
	TokensInput  int
	TokensOutput int
	CostUSD      float64
}

// SaveAgentRun creates or updates an agent run record
func (s *Store) SaveAgentRun(run *AgentRun) error {
	_, err := s.db.Exec(`
		INSERT INTO agent_runs (id, task_id, worktree_path, log_path, pid, status, started_at, finished_at, error_message, session_id)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			status = excluded.status,
			finished_at = excluded.finished_at,
			error_message = excluded.error_message,
			session_id = excluded.session_id
	`,
		run.ID,
		run.TaskID,
		run.WorktreePath,
		run.LogPath,
		run.PID,
		run.Status,
		run.StartedAt,
		run.FinishedAt,
		run.ErrorMessage,
		run.SessionID,
	)
	return err
}

// GetAgentRun retrieves an agent run by ID
func (s *Store) GetAgentRun(id string) (*AgentRun, error) {
	row := s.db.QueryRow(`
		SELECT id, task_id, worktree_path, log_path, pid, status, started_at, finished_at, error_message, COALESCE(session_id, '')
		FROM agent_runs WHERE id = ?
	`, id)

	var run AgentRun
	var finishedAt sql.NullTime
	var errorMsg sql.NullString

	err := row.Scan(&run.ID, &run.TaskID, &run.WorktreePath, &run.LogPath, &run.PID, &run.Status, &run.StartedAt, &finishedAt, &errorMsg, &run.SessionID)
	if err != nil {
		return nil, err
	}

	if finishedAt.Valid {
		run.FinishedAt = &finishedAt.Time
	}
	if errorMsg.Valid {
		run.ErrorMessage = errorMsg.String
	}

	return &run, nil
}

// ListActiveAgentRuns returns all agent runs that are still running
func (s *Store) ListActiveAgentRuns() ([]*AgentRun, error) {
	rows, err := s.db.Query(`
		SELECT id, task_id, worktree_path, log_path, pid, status, started_at, finished_at, error_message, COALESCE(session_id, '')
		FROM agent_runs WHERE status = 'running'
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var runs []*AgentRun
	for rows.Next() {
		var run AgentRun
		var finishedAt sql.NullTime
		var errorMsg sql.NullString

		err := rows.Scan(&run.ID, &run.TaskID, &run.WorktreePath, &run.LogPath, &run.PID, &run.Status, &run.StartedAt, &finishedAt, &errorMsg, &run.SessionID)
		if err != nil {
			return nil, err
		}

		if finishedAt.Valid {
			run.FinishedAt = &finishedAt.Time
		}
		if errorMsg.Valid {
			run.ErrorMessage = errorMsg.String
		}

		runs = append(runs, &run)
	}

	return runs, rows.Err()
}

// UpdateAgentRunStatus updates the status of an agent run
func (s *Store) UpdateAgentRunStatus(id string, status string, errorMessage string) error {
	var finishedAt *time.Time
	if status == "completed" || status == "failed" {
		now := time.Now()
		finishedAt = &now
	}

	_, err := s.db.Exec(`
		UPDATE agent_runs SET status = ?, finished_at = ?, error_message = ? WHERE id = ?
	`, status, finishedAt, errorMessage, id)
	return err
}

// DeleteAgentRun removes an agent run record
func (s *Store) DeleteAgentRun(id string) error {
	_, err := s.db.Exec(`DELETE FROM agent_runs WHERE id = ?`, id)
	return err
}

// UpdateAgentRunUsage updates the token usage for an agent run
func (s *Store) UpdateAgentRunUsage(id string, tokensInput, tokensOutput int, costUSD float64) error {
	_, err := s.db.Exec(`
		UPDATE agent_runs SET tokens_input = ?, tokens_output = ?, cost_usd = ? WHERE id = ?
	`, tokensInput, tokensOutput, costUSD, id)
	return err
}

// GetGroupPriorities returns all group priorities as a map
func (s *Store) GetGroupPriorities() (map[string]int, error) {
	rows, err := s.db.Query("SELECT group_name, priority FROM group_priorities")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	priorities := make(map[string]int)
	for rows.Next() {
		var name string
		var priority int
		if err := rows.Scan(&name, &priority); err != nil {
			return nil, err
		}
		priorities[name] = priority
	}
	return priorities, rows.Err()
}

// SetGroupPriority sets the priority tier for a group (upsert)
func (s *Store) SetGroupPriority(group string, priority int) error {
	_, err := s.db.Exec(`
		INSERT INTO group_priorities (group_name, priority)
		VALUES (?, ?)
		ON CONFLICT(group_name) DO UPDATE SET priority = excluded.priority
	`, group, priority)
	return err
}

// RemoveGroupPriority removes a group from the priorities table
func (s *Store) RemoveGroupPriority(group string) error {
	_, err := s.db.Exec("DELETE FROM group_priorities WHERE group_name = ?", group)
	return err
}

// GroupStats holds aggregated task counts for a group
type GroupStats struct {
	Name      string
	Priority  int // -1 if unassigned
	Total     int
	Completed int
}

// GetGroupsWithTaskCounts returns all groups with their task statistics
func (s *Store) GetGroupsWithTaskCounts() ([]GroupStats, error) {
	// Query task counts by module
	rows, err := s.db.Query(`
		SELECT
			t.module,
			COALESCE(gp.priority, -1) as priority,
			COUNT(*) as total,
			SUM(CASE WHEN t.status = 'complete' THEN 1 ELSE 0 END) as completed
		FROM tasks t
		LEFT JOIN group_priorities gp ON t.module = gp.group_name
		GROUP BY t.module
		ORDER BY COALESCE(gp.priority, 0), t.module
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var stats []GroupStats
	for rows.Next() {
		var gs GroupStats
		if err := rows.Scan(&gs.Name, &gs.Priority, &gs.Total, &gs.Completed); err != nil {
			return nil, err
		}
		stats = append(stats, gs)
	}
	return stats, rows.Err()
}

// ListRecentAgentRuns returns completed/failed agent runs, in chronological order (oldest first)
func (s *Store) ListRecentAgentRuns(limit int) ([]*AgentRun, error) {
	// Get the N most recent runs, then reverse to show in chronological order
	rows, err := s.db.Query(`
		SELECT id, task_id, worktree_path, log_path, pid, status, started_at, finished_at,
		       error_message, COALESCE(session_id, ''), tokens_input, tokens_output, cost_usd
		FROM (
			SELECT * FROM agent_runs
			WHERE status IN ('completed', 'failed')
			ORDER BY COALESCE(finished_at, started_at) DESC
			LIMIT ?
		) sub
		ORDER BY COALESCE(finished_at, started_at) ASC
	`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var runs []*AgentRun
	for rows.Next() {
		var run AgentRun
		var finishedAt sql.NullTime
		var errorMsg sql.NullString

		err := rows.Scan(&run.ID, &run.TaskID, &run.WorktreePath, &run.LogPath, &run.PID,
			&run.Status, &run.StartedAt, &finishedAt, &errorMsg, &run.SessionID,
			&run.TokensInput, &run.TokensOutput, &run.CostUSD)
		if err != nil {
			return nil, err
		}

		if finishedAt.Valid {
			run.FinishedAt = &finishedAt.Time
		}
		if errorMsg.Valid {
			run.ErrorMessage = errorMsg.String
		}

		runs = append(runs, &run)
	}

	return runs, rows.Err()
}

// UpsertGitHubIssue inserts or updates a GitHub issue record
func (s *Store) UpsertGitHubIssue(issue *domain.GitHubIssue) error {
	now := time.Now()
	_, err := s.db.Exec(`
		INSERT INTO github_issues (issue_number, repo, title, status, group_name, analyzed_at, plan_path, closed_at, pr_number, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(issue_number) DO UPDATE SET
			repo = excluded.repo,
			title = excluded.title,
			status = excluded.status,
			group_name = excluded.group_name,
			analyzed_at = excluded.analyzed_at,
			plan_path = excluded.plan_path,
			closed_at = excluded.closed_at,
			pr_number = excluded.pr_number,
			updated_at = excluded.updated_at
	`, issue.IssueNumber, issue.Repo, issue.Title, string(issue.Status), issue.GroupName,
		issue.AnalyzedAt, issue.PlanPath, issue.ClosedAt, issue.PRNumber, now, now)
	return err
}

// scanGitHubIssue populates a GitHubIssue from nullable scan values
func scanGitHubIssue(issue *domain.GitHubIssue, status string, groupName sql.NullString, analyzedAt sql.NullTime, planPath sql.NullString, closedAt sql.NullTime, prNumber sql.NullInt64) {
	issue.Status = domain.IssueStatus(status)
	if groupName.Valid {
		issue.GroupName = groupName.String
	}
	if analyzedAt.Valid {
		issue.AnalyzedAt = &analyzedAt.Time
	}
	if planPath.Valid {
		issue.PlanPath = planPath.String
	}
	if closedAt.Valid {
		issue.ClosedAt = &closedAt.Time
	}
	if prNumber.Valid {
		pn := int(prNumber.Int64)
		issue.PRNumber = &pn
	}
}

// GetGitHubIssue retrieves a GitHub issue by its issue number
func (s *Store) GetGitHubIssue(issueNumber int) (*domain.GitHubIssue, error) {
	row := s.db.QueryRow(`
		SELECT issue_number, repo, title, status, group_name, analyzed_at, plan_path, closed_at, pr_number, created_at, updated_at
		FROM github_issues WHERE issue_number = ?
	`, issueNumber)

	var issue domain.GitHubIssue
	var status string
	var groupName sql.NullString
	var analyzedAt sql.NullTime
	var planPath sql.NullString
	var closedAt sql.NullTime
	var prNumber sql.NullInt64

	err := row.Scan(&issue.IssueNumber, &issue.Repo, &issue.Title, &status, &groupName,
		&analyzedAt, &planPath, &closedAt, &prNumber, &issue.CreatedAt, &issue.UpdatedAt)
	if err != nil {
		return nil, err
	}
	scanGitHubIssue(&issue, status, groupName, analyzedAt, planPath, closedAt, prNumber)
	return &issue, nil
}

// ListPendingIssues returns all GitHub issues with status "pending" for a given repo
func (s *Store) ListPendingIssues(repo string) ([]*domain.GitHubIssue, error) {
	rows, err := s.db.Query(`
		SELECT issue_number, repo, title, status, group_name, analyzed_at, plan_path, closed_at, pr_number, created_at, updated_at
		FROM github_issues WHERE repo = ? AND status = ?
	`, repo, string(domain.IssuePending))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var issues []*domain.GitHubIssue
	for rows.Next() {
		var issue domain.GitHubIssue
		var status string
		var groupName sql.NullString
		var analyzedAt sql.NullTime
		var planPath sql.NullString
		var closedAt sql.NullTime
		var prNumber sql.NullInt64

		if err := rows.Scan(&issue.IssueNumber, &issue.Repo, &issue.Title, &status, &groupName,
			&analyzedAt, &planPath, &closedAt, &prNumber, &issue.CreatedAt, &issue.UpdatedAt); err != nil {
			return nil, err
		}
		scanGitHubIssue(&issue, status, groupName, analyzedAt, planPath, closedAt, prNumber)
		issues = append(issues, &issue)
	}
	return issues, rows.Err()
}

// UpdateIssueStatus updates the status of a GitHub issue
func (s *Store) UpdateIssueStatus(issueNumber int, status domain.IssueStatus) error {
	_, err := s.db.Exec(`UPDATE github_issues SET status = ?, updated_at = ? WHERE issue_number = ?`,
		string(status), time.Now(), issueNumber)
	return err
}

// MarkIssueClosed marks a GitHub issue as implemented with the associated PR number
func (s *Store) MarkIssueClosed(issueNumber int, prNumber int) error {
	now := time.Now()
	_, err := s.db.Exec(`UPDATE github_issues SET status = ?, closed_at = ?, pr_number = ?, updated_at = ? WHERE issue_number = ?`,
		string(domain.IssueImplemented), now, prNumber, now, issueNumber)
	return err
}

// ListGitHubIssues returns all tracked GitHub issues ordered by issue number descending
func (s *Store) ListGitHubIssues() ([]*domain.GitHubIssue, error) {
	rows, err := s.db.Query(`
		SELECT issue_number, repo, title, status, group_name, analyzed_at, plan_path, closed_at, pr_number, created_at, updated_at
		FROM github_issues ORDER BY issue_number DESC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var issues []*domain.GitHubIssue
	for rows.Next() {
		var issue domain.GitHubIssue
		var status string
		var groupName sql.NullString
		var analyzedAt sql.NullTime
		var planPath sql.NullString
		var closedAt sql.NullTime
		var prNumber sql.NullInt64

		if err := rows.Scan(&issue.IssueNumber, &issue.Repo, &issue.Title, &status, &groupName,
			&analyzedAt, &planPath, &closedAt, &prNumber, &issue.CreatedAt, &issue.UpdatedAt); err != nil {
			return nil, err
		}
		scanGitHubIssue(&issue, status, groupName, analyzedAt, planPath, closedAt, prNumber)
		issues = append(issues, &issue)
	}
	return issues, rows.Err()
}

// GetIncompleteEpicsForIssue returns all tasks linked to a GitHub issue that are not complete
func (s *Store) GetIncompleteEpicsForIssue(issueNumber int) ([]*domain.Task, error) {
	rows, err := s.db.Query(`
		SELECT id, module, prefix, epic_num, title, description, status, priority, depends_on, needs_review, file_path, github_issue, created_at, updated_at
		FROM tasks WHERE github_issue = ? AND status != ?
	`, issueNumber, string(domain.StatusComplete))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tasks []*domain.Task
	for rows.Next() {
		task, err := scanTaskRows(rows)
		if err != nil {
			return nil, err
		}
		tasks = append(tasks, task)
	}
	return tasks, rows.Err()
}
