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
`
