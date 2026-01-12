package taskstore

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/anthropics/erp-orchestrator/internal/domain"
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

	// Run migrations
	if _, err := db.Exec(schema); err != nil {
		return nil, fmt.Errorf("running migrations: %w", err)
	}

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
		INSERT INTO tasks (id, module, epic_num, title, description, status, priority, depends_on, needs_review, file_path, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			title = excluded.title,
			description = excluded.description,
			priority = excluded.priority,
			depends_on = excluded.depends_on,
			needs_review = excluded.needs_review,
			file_path = excluded.file_path,
			updated_at = excluded.updated_at
	`,
		task.ID.String(),
		task.ID.Module,
		task.ID.EpicNum,
		task.Title,
		task.Description,
		string(task.Status),
		string(task.Priority),
		string(depsJSON),
		task.NeedsReview,
		task.FilePath,
		task.CreatedAt,
		task.UpdatedAt,
	)
	return err
}

// GetTask retrieves a task by ID
func (s *Store) GetTask(id string) (*domain.Task, error) {
	row := s.db.QueryRow(`
		SELECT id, module, epic_num, title, description, status, priority, depends_on, needs_review, file_path, created_at, updated_at
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
	query := `SELECT id, module, epic_num, title, description, status, priority, depends_on, needs_review, file_path, created_at, updated_at FROM tasks WHERE 1=1`
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
	var id, module string
	var epicNum int
	var status, priority, depsJSON string
	var description sql.NullString

	err := row.Scan(&id, &module, &epicNum, &task.Title, &description, &status, &priority, &depsJSON, &task.NeedsReview, &task.FilePath, &task.CreatedAt, &task.UpdatedAt)
	if err != nil {
		return nil, err
	}

	task.ID = domain.TaskID{Module: module, EpicNum: epicNum}
	task.Status = domain.TaskStatus(status)
	task.Priority = domain.Priority(priority)
	if description.Valid {
		task.Description = description.String
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
	var id, module string
	var epicNum int
	var status, priority, depsJSON string
	var description sql.NullString

	err := rows.Scan(&id, &module, &epicNum, &task.Title, &description, &status, &priority, &depsJSON, &task.NeedsReview, &task.FilePath, &task.CreatedAt, &task.UpdatedAt)
	if err != nil {
		return nil, err
	}

	task.ID = domain.TaskID{Module: module, EpicNum: epicNum}
	task.Status = domain.TaskStatus(status)
	task.Priority = domain.Priority(priority)
	if description.Valid {
		task.Description = description.String
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
