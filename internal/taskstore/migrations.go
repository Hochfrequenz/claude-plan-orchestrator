package taskstore

const schema = `
CREATE TABLE IF NOT EXISTS tasks (
    id TEXT PRIMARY KEY,
    module TEXT NOT NULL,
    epic_num INTEGER NOT NULL,
    title TEXT NOT NULL,
    description TEXT,
    status TEXT NOT NULL DEFAULT 'not_started',
    priority TEXT,
    depends_on TEXT,
    needs_review BOOLEAN DEFAULT FALSE,
    file_path TEXT NOT NULL,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_tasks_module ON tasks(module);
CREATE INDEX IF NOT EXISTS idx_tasks_status ON tasks(status);

CREATE TABLE IF NOT EXISTS runs (
    id TEXT PRIMARY KEY,
    task_id TEXT NOT NULL REFERENCES tasks(id),
    worktree_path TEXT,
    branch TEXT,
    status TEXT NOT NULL,
    started_at TIMESTAMP,
    finished_at TIMESTAMP,
    tokens_input INTEGER,
    tokens_output INTEGER
);

CREATE INDEX IF NOT EXISTS idx_runs_task_id ON runs(task_id);
CREATE INDEX IF NOT EXISTS idx_runs_status ON runs(status);

CREATE TABLE IF NOT EXISTS logs (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    run_id TEXT NOT NULL REFERENCES runs(id),
    timestamp TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    level TEXT,
    message TEXT
);

CREATE INDEX IF NOT EXISTS idx_logs_run_id ON logs(run_id);

CREATE TABLE IF NOT EXISTS prs (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    run_id TEXT NOT NULL REFERENCES runs(id),
    pr_number INTEGER,
    url TEXT,
    review_status TEXT,
    merged_at TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_prs_run_id ON prs(run_id);

CREATE TABLE IF NOT EXISTS batches (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL,
    started_at TIMESTAMP,
    finished_at TIMESTAMP,
    tasks_completed INTEGER DEFAULT 0,
    tasks_failed INTEGER DEFAULT 0
);

CREATE TABLE IF NOT EXISTS agent_runs (
    id TEXT PRIMARY KEY,
    task_id TEXT NOT NULL,
    worktree_path TEXT NOT NULL,
    log_path TEXT NOT NULL,
    pid INTEGER,
    status TEXT NOT NULL DEFAULT 'running',
    started_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    finished_at TIMESTAMP,
    error_message TEXT,
    session_id TEXT,
    tokens_input INTEGER DEFAULT 0,
    tokens_output INTEGER DEFAULT 0,
    cost_usd REAL DEFAULT 0.0
);

CREATE INDEX IF NOT EXISTS idx_agent_runs_status ON agent_runs(status);
CREATE INDEX IF NOT EXISTS idx_agent_runs_task_id ON agent_runs(task_id);

CREATE TABLE IF NOT EXISTS group_priorities (
    group_name TEXT PRIMARY KEY,
    priority   INTEGER NOT NULL DEFAULT 0
);

CREATE INDEX IF NOT EXISTS idx_group_priorities_priority ON group_priorities(priority);
`

// Migration to add session_id column to existing databases
const migrationAddSessionID = `
ALTER TABLE agent_runs ADD COLUMN session_id TEXT;
`

// Migration to add github_issues table for tracking GitHub issues
const migrationGitHubIssues = `
CREATE TABLE IF NOT EXISTS github_issues (
    issue_number  INTEGER PRIMARY KEY,
    repo          TEXT NOT NULL,
    title         TEXT NOT NULL,
    status        TEXT NOT NULL DEFAULT 'pending',
    group_name    TEXT,
    analyzed_at   TIMESTAMP,
    plan_path     TEXT,
    closed_at     TIMESTAMP,
    pr_number     INTEGER,
    created_at    TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at    TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);
`

// Migration to add github_issue column to tasks table
const migrationTasksGitHubIssue = `
ALTER TABLE tasks ADD COLUMN github_issue INTEGER REFERENCES github_issues(issue_number);
`
