# ERP Orchestrator Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Build an autonomous development orchestrator that parses markdown plans, dispatches Claude Code agents in git worktrees, and manages the full PR lifecycle.

**Architecture:** A Go CLI application with six internal packages (parser, taskstore, scheduler, executor, prbot, observer) coordinated through SQLite state. The TUI provides real-time monitoring via bubbletea, while the web UI (Svelte) offers historical analytics.

**Tech Stack:** Go 1.23+, SQLite (modernc.org/sqlite for pure Go), bubbletea/lipgloss (TUI), Svelte (Web UI), gh CLI (GitHub operations)

---

## Progress Summary

| Task | Status |
|------|--------|
| 1.1 Initialize Go Module | DONE |
| 1.2 Define Core Domain Types | DONE |
| 1.3 Create Configuration Package | DONE |
| 2.1 Create Markdown Parser | DONE |
| 2.2 Create SQLite Task Store | DONE |
| 3.1 Create Scheduler | DONE |
| 4.1 Create Worktree Manager | DONE |
| 4.2 Create Agent Spawner | DONE |
| 5.1 Create PR Bot | DONE |
| 6.1 Create Observer | TODO |
| 6.2 Create CLI | TODO |
| 7.1 Create TUI Model | TODO |

**Next task to implement:** Task 6.1: Create Observer

---

## Phase 1: Project Scaffolding and Core Types

### Task 1.1: Initialize Go Module [DONE]

**Files:**
- Create: `go.mod`
- Create: `go.sum`

**Step 1: Initialize Go module**

Run:
```bash
cd /home/claude/github/erp-orchestrator
go mod init github.com/anthropics/erp-orchestrator
```

**Step 2: Verify module created**

Run: `cat go.mod`
Expected: Module path and Go version

**Step 3: Commit**

```bash
git init
git add go.mod
git commit -m "chore: initialize Go module"
```

---

### Task 1.2: Define Core Domain Types [DONE]

**Files:**
- Create: `internal/domain/task.go`
- Create: `internal/domain/run.go`
- Create: `internal/domain/types.go`

**Step 1: Write types test**

Create: `internal/domain/task_test.go`

```go
package domain

import (
	"testing"
)

func TestTaskID_Parse(t *testing.T) {
	tests := []struct {
		input      string
		wantModule string
		wantEpic   int
		wantErr    bool
	}{
		{"technical/E05", "technical", 5, false},
		{"billing/E00", "billing", 0, false},
		{"pricing/E123", "pricing", 123, false},
		{"invalid", "", 0, true},
		{"module/invalid", "", 0, true},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			tid, err := ParseTaskID(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseTaskID(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
				return
			}
			if err == nil {
				if tid.Module != tt.wantModule {
					t.Errorf("Module = %q, want %q", tid.Module, tt.wantModule)
				}
				if tid.EpicNum != tt.wantEpic {
					t.Errorf("EpicNum = %d, want %d", tid.EpicNum, tt.wantEpic)
				}
			}
		})
	}
}

func TestTaskID_String(t *testing.T) {
	tid := TaskID{Module: "technical", EpicNum: 5}
	if got := tid.String(); got != "technical/E05" {
		t.Errorf("String() = %q, want %q", got, "technical/E05")
	}
}

func TestTask_IsReady(t *testing.T) {
	completed := map[string]bool{"technical/E04": true}

	task := Task{
		ID:        TaskID{Module: "technical", EpicNum: 5},
		DependsOn: []TaskID{{Module: "technical", EpicNum: 4}},
		Status:    StatusNotStarted,
	}

	if !task.IsReady(completed) {
		t.Error("Task should be ready when dependencies are complete")
	}

	task.DependsOn = append(task.DependsOn, TaskID{Module: "billing", EpicNum: 1})
	if task.IsReady(completed) {
		t.Error("Task should not be ready when dependencies are incomplete")
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/domain/... -v`
Expected: FAIL (package doesn't exist)

**Step 3: Create types file**

Create: `internal/domain/types.go`

```go
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
```

**Step 4: Create task file**

Create: `internal/domain/task.go`

```go
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
```

**Step 5: Create run file**

Create: `internal/domain/run.go`

```go
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
```

**Step 6: Run tests to verify they pass**

Run: `go test ./internal/domain/... -v`
Expected: PASS

**Step 7: Commit**

```bash
git add internal/domain/
git commit -m "feat(domain): add core domain types for tasks, runs, and PRs"
```

---

### Task 1.3: Create Configuration Package [DONE]

**Files:**
- Create: `internal/config/config.go`
- Create: `internal/config/config_test.go`

**Step 1: Write config test**

Create: `internal/config/config_test.go`

```go
package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoad_Defaults(t *testing.T) {
	cfg := Default()

	if cfg.General.MaxParallelAgents != 3 {
		t.Errorf("MaxParallelAgents = %d, want 3", cfg.General.MaxParallelAgents)
	}
	if cfg.Web.Port != 8080 {
		t.Errorf("Web.Port = %d, want 8080", cfg.Web.Port)
	}
	if cfg.Web.Host != "127.0.0.1" {
		t.Errorf("Web.Host = %q, want 127.0.0.1", cfg.Web.Host)
	}
}

func TestLoad_FromFile(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.toml")

	content := `
[general]
project_root = "/test/project"
max_parallel_agents = 5

[web]
port = 9000
`
	if err := os.WriteFile(configPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(configPath)
	if err != nil {
		t.Fatal(err)
	}

	if cfg.General.ProjectRoot != "/test/project" {
		t.Errorf("ProjectRoot = %q, want /test/project", cfg.General.ProjectRoot)
	}
	if cfg.General.MaxParallelAgents != 5 {
		t.Errorf("MaxParallelAgents = %d, want 5", cfg.General.MaxParallelAgents)
	}
	if cfg.Web.Port != 9000 {
		t.Errorf("Web.Port = %d, want 9000", cfg.Web.Port)
	}
}

func TestExpandPath(t *testing.T) {
	home, _ := os.UserHomeDir()

	tests := []struct {
		input string
		want  string
	}{
		{"~/test", filepath.Join(home, "test")},
		{"/absolute/path", "/absolute/path"},
		{"relative", "relative"},
	}

	for _, tt := range tests {
		got := ExpandPath(tt.input)
		if got != tt.want {
			t.Errorf("ExpandPath(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/config/... -v`
Expected: FAIL (package doesn't exist)

**Step 3: Create config package**

Create: `internal/config/config.go`

```go
package config

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

// Config holds all application configuration
type Config struct {
	General       GeneralConfig       `toml:"general"`
	Claude        ClaudeConfig        `toml:"claude"`
	Notifications NotificationsConfig `toml:"notifications"`
	Web           WebConfig           `toml:"web"`
}

// GeneralConfig holds general settings
type GeneralConfig struct {
	ProjectRoot       string `toml:"project_root"`
	WorktreeDir       string `toml:"worktree_dir"`
	MaxParallelAgents int    `toml:"max_parallel_agents"`
	DatabasePath      string `toml:"database_path"`
}

// ClaudeConfig holds Claude API settings
type ClaudeConfig struct {
	Model     string `toml:"model"`
	MaxTokens int    `toml:"max_tokens"`
}

// NotificationsConfig holds notification settings
type NotificationsConfig struct {
	Desktop      bool   `toml:"desktop"`
	SlackWebhook string `toml:"slack_webhook"`
}

// WebConfig holds web UI settings
type WebConfig struct {
	Port int    `toml:"port"`
	Host string `toml:"host"`
}

// Default returns a Config with sensible defaults
func Default() *Config {
	home, _ := os.UserHomeDir()
	return &Config{
		General: GeneralConfig{
			ProjectRoot:       "",
			WorktreeDir:       filepath.Join(home, ".erp-orchestrator", "worktrees"),
			MaxParallelAgents: 3,
			DatabasePath:      filepath.Join(home, ".erp-orchestrator", "orchestrator.db"),
		},
		Claude: ClaudeConfig{
			Model:     "claude-sonnet-4-20250514",
			MaxTokens: 16000,
		},
		Notifications: NotificationsConfig{
			Desktop: true,
		},
		Web: WebConfig{
			Port: 8080,
			Host: "127.0.0.1",
		},
	}
}

// Load reads configuration from a TOML file, falling back to defaults
func Load(path string) (*Config, error) {
	cfg := Default()

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return nil, err
	}

	if err := toml.Unmarshal(data, cfg); err != nil {
		return nil, err
	}

	// Expand paths
	cfg.General.ProjectRoot = ExpandPath(cfg.General.ProjectRoot)
	cfg.General.WorktreeDir = ExpandPath(cfg.General.WorktreeDir)
	cfg.General.DatabasePath = ExpandPath(cfg.General.DatabasePath)

	return cfg, nil
}

// ExpandPath expands ~ to the user's home directory
func ExpandPath(path string) string {
	if strings.HasPrefix(path, "~/") {
		home, _ := os.UserHomeDir()
		return filepath.Join(home, path[2:])
	}
	return path
}

// DefaultConfigPath returns the default config file location
func DefaultConfigPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "erp-orchestrator", "config.toml")
}
```

**Step 4: Add TOML dependency**

Run:
```bash
go get github.com/pelletier/go-toml/v2
```

**Step 5: Run tests to verify they pass**

Run: `go test ./internal/config/... -v`
Expected: PASS

**Step 6: Commit**

```bash
git add internal/config/ go.mod go.sum
git commit -m "feat(config): add TOML configuration loading with defaults"
```

---

## Phase 2: Markdown Parser and Task Store

### Task 2.1: Create Markdown Parser [DONE]

**Files:**
- Create: `internal/parser/parser.go`
- Create: `internal/parser/parser_test.go`
- Create: `internal/parser/frontmatter.go`

**Step 1: Write parser test**

Create: `internal/parser/parser_test.go`

```go
package parser

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/anthropics/erp-orchestrator/internal/domain"
)

func TestParseEpicFile(t *testing.T) {
	content := `---
priority: high
depends_on: [billing/E01, technical/E03]
needs_review: true
---
# Epic 05: Validators

Implement input validation for all user-facing forms.

## Requirements

- Validate email format
- Validate phone numbers
`
	dir := t.TempDir()
	epicPath := filepath.Join(dir, "technical-module", "epic-05-validators.md")
	os.MkdirAll(filepath.Dir(epicPath), 0755)
	os.WriteFile(epicPath, []byte(content), 0644)

	task, err := ParseEpicFile(epicPath)
	if err != nil {
		t.Fatal(err)
	}

	if task.ID.String() != "technical/E05" {
		t.Errorf("ID = %q, want technical/E05", task.ID.String())
	}
	if task.Title != "Epic 05: Validators" {
		t.Errorf("Title = %q, want 'Epic 05: Validators'", task.Title)
	}
	if task.Priority != domain.PriorityHigh {
		t.Errorf("Priority = %q, want high", task.Priority)
	}
	if !task.NeedsReview {
		t.Error("NeedsReview should be true")
	}
	if len(task.DependsOn) != 2 {
		t.Errorf("DependsOn count = %d, want 2", len(task.DependsOn))
	}
}

func TestParseModuleDir(t *testing.T) {
	dir := t.TempDir()
	moduleDir := filepath.Join(dir, "technical-module")
	os.MkdirAll(moduleDir, 0755)

	// Create overview
	os.WriteFile(filepath.Join(moduleDir, "00-overview.md"), []byte("# Technical Module\n\nOverview content."), 0644)

	// Create epics
	os.WriteFile(filepath.Join(moduleDir, "epic-00-scaffolding.md"), []byte("# Epic 00: Scaffolding\n\nSetup project."), 0644)
	os.WriteFile(filepath.Join(moduleDir, "epic-01-entities.md"), []byte("# Epic 01: Entities\n\nCreate entities."), 0644)

	tasks, err := ParseModuleDir(moduleDir)
	if err != nil {
		t.Fatal(err)
	}

	if len(tasks) != 2 {
		t.Errorf("Task count = %d, want 2", len(tasks))
	}

	// Check implicit dependency
	if tasks[1].ID.EpicNum == 1 {
		dep := tasks[1].ImplicitDependency()
		if dep == nil || dep.EpicNum != 0 {
			t.Error("Epic 01 should have implicit dependency on Epic 00")
		}
	}
}

func TestExtractTaskIDFromPath(t *testing.T) {
	tests := []struct {
		path       string
		wantModule string
		wantEpic   int
		wantErr    bool
	}{
		{"/plans/technical-module/epic-05-validators.md", "technical", 5, false},
		{"/plans/billing-module/epic-00-setup.md", "billing", 0, false},
		{"/plans/some-module/00-overview.md", "", 0, true},
		{"/plans/invalid/file.txt", "", 0, true},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			tid, err := ExtractTaskIDFromPath(tt.path)
			if (err != nil) != tt.wantErr {
				t.Errorf("error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if err == nil {
				if tid.Module != tt.wantModule {
					t.Errorf("Module = %q, want %q", tid.Module, tt.wantModule)
				}
				if tid.EpicNum != tt.wantEpic {
					t.Errorf("EpicNum = %d, want %d", tid.EpicNum, tt.wantEpic)
				}
			}
		})
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/parser/... -v`
Expected: FAIL (package doesn't exist)

**Step 3: Create frontmatter parser**

Create: `internal/parser/frontmatter.go`

```go
package parser

import (
	"bytes"

	"github.com/anthropics/erp-orchestrator/internal/domain"
	"gopkg.in/yaml.v3"
)

// Frontmatter represents the YAML frontmatter in epic files
type Frontmatter struct {
	Priority    string   `yaml:"priority"`
	DependsOn   []string `yaml:"depends_on"`
	NeedsReview bool     `yaml:"needs_review"`
}

// ParseFrontmatter extracts YAML frontmatter from markdown content
// Returns the frontmatter, remaining content, and any error
func ParseFrontmatter(content []byte) (*Frontmatter, []byte, error) {
	if !bytes.HasPrefix(content, []byte("---\n")) {
		return &Frontmatter{}, content, nil
	}

	// Find end of frontmatter
	rest := content[4:]
	endIdx := bytes.Index(rest, []byte("\n---"))
	if endIdx == -1 {
		return &Frontmatter{}, content, nil
	}

	fmData := rest[:endIdx]
	remaining := rest[endIdx+4:] // skip \n---

	var fm Frontmatter
	if err := yaml.Unmarshal(fmData, &fm); err != nil {
		return nil, nil, err
	}

	return &fm, bytes.TrimLeft(remaining, "\n"), nil
}

// ParseDependencies converts string dependency IDs to TaskIDs
func ParseDependencies(deps []string) ([]domain.TaskID, error) {
	result := make([]domain.TaskID, 0, len(deps))
	for _, d := range deps {
		tid, err := domain.ParseTaskID(d)
		if err != nil {
			return nil, err
		}
		result = append(result, tid)
	}
	return result, nil
}

// ToPriority converts a string to a Priority
func ToPriority(s string) domain.Priority {
	switch s {
	case "high":
		return domain.PriorityHigh
	case "low":
		return domain.PriorityLow
	default:
		return domain.PriorityNormal
	}
}
```

**Step 4: Create main parser**

Create: `internal/parser/parser.go`

```go
package parser

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/anthropics/erp-orchestrator/internal/domain"
)

var (
	epicFileRegex   = regexp.MustCompile(`^epic-(\d+)-.*\.md$`)
	moduleDirRegex  = regexp.MustCompile(`^([a-z][a-z0-9-]*)-module$`)
	titleRegex      = regexp.MustCompile(`^#\s+(.+)$`)
)

// ParseEpicFile parses a single epic markdown file into a Task
func ParseEpicFile(path string) (*domain.Task, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	taskID, err := ExtractTaskIDFromPath(path)
	if err != nil {
		return nil, err
	}

	fm, body, err := ParseFrontmatter(content)
	if err != nil {
		return nil, fmt.Errorf("parsing frontmatter: %w", err)
	}

	title := extractTitle(body)
	description := extractDescription(body)

	deps, err := ParseDependencies(fm.DependsOn)
	if err != nil {
		return nil, fmt.Errorf("parsing dependencies: %w", err)
	}

	now := time.Now()
	return &domain.Task{
		ID:          taskID,
		Title:       title,
		Description: description,
		Status:      domain.StatusNotStarted,
		Priority:    ToPriority(fm.Priority),
		DependsOn:   deps,
		NeedsReview: fm.NeedsReview,
		FilePath:    path,
		CreatedAt:   now,
		UpdatedAt:   now,
	}, nil
}

// ParseModuleDir parses all epic files in a module directory
func ParseModuleDir(dir string) ([]*domain.Task, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	var tasks []*domain.Task
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if !epicFileRegex.MatchString(entry.Name()) {
			continue
		}

		task, err := ParseEpicFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			return nil, fmt.Errorf("parsing %s: %w", entry.Name(), err)
		}
		tasks = append(tasks, task)
	}

	// Add implicit dependencies
	for _, task := range tasks {
		if dep := task.ImplicitDependency(); dep != nil {
			// Check if already in explicit deps
			found := false
			for _, d := range task.DependsOn {
				if d.String() == dep.String() {
					found = true
					break
				}
			}
			if !found {
				task.DependsOn = append(task.DependsOn, *dep)
			}
		}
	}

	return tasks, nil
}

// ParsePlansDir parses all modules in a docs/plans directory
func ParsePlansDir(plansDir string) ([]*domain.Task, error) {
	entries, err := os.ReadDir(plansDir)
	if err != nil {
		return nil, err
	}

	var allTasks []*domain.Task
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		if !moduleDirRegex.MatchString(entry.Name()) {
			continue
		}

		tasks, err := ParseModuleDir(filepath.Join(plansDir, entry.Name()))
		if err != nil {
			return nil, fmt.Errorf("parsing module %s: %w", entry.Name(), err)
		}
		allTasks = append(allTasks, tasks...)
	}

	return allTasks, nil
}

// ExtractTaskIDFromPath extracts a TaskID from an epic file path
func ExtractTaskIDFromPath(path string) (domain.TaskID, error) {
	dir := filepath.Dir(path)
	base := filepath.Base(path)

	// Extract module from directory name
	dirBase := filepath.Base(dir)
	matches := moduleDirRegex.FindStringSubmatch(dirBase)
	if matches == nil {
		return domain.TaskID{}, fmt.Errorf("invalid module directory: %s", dirBase)
	}
	module := matches[1]

	// Extract epic number from filename
	matches = epicFileRegex.FindStringSubmatch(base)
	if matches == nil {
		return domain.TaskID{}, fmt.Errorf("invalid epic filename: %s", base)
	}
	epicNum, _ := strconv.Atoi(matches[1])

	return domain.TaskID{Module: module, EpicNum: epicNum}, nil
}

func extractTitle(content []byte) string {
	scanner := bufio.NewScanner(bytes.NewReader(content))
	for scanner.Scan() {
		line := scanner.Text()
		if matches := titleRegex.FindStringSubmatch(line); matches != nil {
			return strings.TrimSpace(matches[1])
		}
	}
	return ""
}

func extractDescription(content []byte) string {
	scanner := bufio.NewScanner(bytes.NewReader(content))
	var lines []string
	foundTitle := false

	for scanner.Scan() {
		line := scanner.Text()
		if !foundTitle {
			if titleRegex.MatchString(line) {
				foundTitle = true
			}
			continue
		}

		// Skip empty lines immediately after title
		if len(lines) == 0 && strings.TrimSpace(line) == "" {
			continue
		}

		// Stop at next heading
		if strings.HasPrefix(line, "#") {
			break
		}

		lines = append(lines, line)
	}

	return strings.TrimSpace(strings.Join(lines, "\n"))
}
```

**Step 5: Add YAML dependency**

Run:
```bash
go get gopkg.in/yaml.v3
```

**Step 6: Run tests to verify they pass**

Run: `go test ./internal/parser/... -v`
Expected: PASS

**Step 7: Commit**

```bash
git add internal/parser/ go.mod go.sum
git commit -m "feat(parser): add markdown parser with frontmatter support"
```

---

### Task 2.2: Create SQLite Task Store [DONE]

**Files:**
- Create: `internal/taskstore/store.go`
- Create: `internal/taskstore/store_test.go`
- Create: `internal/taskstore/migrations.go`

**Step 1: Write store test**

Create: `internal/taskstore/store_test.go`

```go
package taskstore

import (
	"testing"
	"time"

	"github.com/anthropics/erp-orchestrator/internal/domain"
)

func TestStore_CreateAndGetTask(t *testing.T) {
	store, err := New(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	task := &domain.Task{
		ID:          domain.TaskID{Module: "technical", EpicNum: 5},
		Title:       "Validators",
		Description: "Implement validators",
		Status:      domain.StatusNotStarted,
		Priority:    domain.PriorityHigh,
		DependsOn:   []domain.TaskID{{Module: "technical", EpicNum: 4}},
		NeedsReview: true,
		FilePath:    "/path/to/epic.md",
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}

	if err := store.UpsertTask(task); err != nil {
		t.Fatal(err)
	}

	got, err := store.GetTask("technical/E05")
	if err != nil {
		t.Fatal(err)
	}

	if got.Title != task.Title {
		t.Errorf("Title = %q, want %q", got.Title, task.Title)
	}
	if got.Priority != task.Priority {
		t.Errorf("Priority = %q, want %q", got.Priority, task.Priority)
	}
	if len(got.DependsOn) != 1 {
		t.Errorf("DependsOn count = %d, want 1", len(got.DependsOn))
	}
}

func TestStore_ListTasks(t *testing.T) {
	store, err := New(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	tasks := []*domain.Task{
		{ID: domain.TaskID{Module: "technical", EpicNum: 0}, Title: "Setup", Status: domain.StatusComplete, FilePath: "/a"},
		{ID: domain.TaskID{Module: "technical", EpicNum: 1}, Title: "Core", Status: domain.StatusNotStarted, FilePath: "/b"},
		{ID: domain.TaskID{Module: "billing", EpicNum: 0}, Title: "Setup", Status: domain.StatusNotStarted, FilePath: "/c"},
	}

	for _, task := range tasks {
		task.CreatedAt = time.Now()
		task.UpdatedAt = time.Now()
		if err := store.UpsertTask(task); err != nil {
			t.Fatal(err)
		}
	}

	// List all
	all, err := store.ListTasks(ListOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 3 {
		t.Errorf("All tasks count = %d, want 3", len(all))
	}

	// Filter by module
	techTasks, err := store.ListTasks(ListOptions{Module: "technical"})
	if err != nil {
		t.Fatal(err)
	}
	if len(techTasks) != 2 {
		t.Errorf("Technical tasks count = %d, want 2", len(techTasks))
	}

	// Filter by status
	notStarted, err := store.ListTasks(ListOptions{Status: domain.StatusNotStarted})
	if err != nil {
		t.Fatal(err)
	}
	if len(notStarted) != 2 {
		t.Errorf("Not started count = %d, want 2", len(notStarted))
	}
}

func TestStore_UpdateTaskStatus(t *testing.T) {
	store, err := New(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	task := &domain.Task{
		ID:        domain.TaskID{Module: "technical", EpicNum: 0},
		Title:     "Setup",
		Status:    domain.StatusNotStarted,
		FilePath:  "/a",
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	store.UpsertTask(task)

	if err := store.UpdateTaskStatus("technical/E00", domain.StatusInProgress); err != nil {
		t.Fatal(err)
	}

	got, _ := store.GetTask("technical/E00")
	if got.Status != domain.StatusInProgress {
		t.Errorf("Status = %q, want in_progress", got.Status)
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/taskstore/... -v`
Expected: FAIL (package doesn't exist)

**Step 3: Create migrations file**

Create: `internal/taskstore/migrations.go`

```go
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
```

**Step 4: Create store implementation**

Create: `internal/taskstore/store.go`

```go
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

type scanner interface {
	Scan(dest ...interface{}) error
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
```

**Step 5: Add SQLite dependency**

Run:
```bash
go get modernc.org/sqlite
```

**Step 6: Run tests to verify they pass**

Run: `go test ./internal/taskstore/... -v`
Expected: PASS

**Step 7: Commit**

```bash
git add internal/taskstore/ go.mod go.sum
git commit -m "feat(taskstore): add SQLite-backed task persistence"
```

---

## Phase 3: Scheduler with Dependency Resolution

### Task 3.1: Create Scheduler [DONE]

**Files:**
- Create: `internal/scheduler/scheduler.go`
- Create: `internal/scheduler/scheduler_test.go`

**Step 1: Write scheduler test**

Create: `internal/scheduler/scheduler_test.go`

```go
package scheduler

import (
	"testing"

	"github.com/anthropics/erp-orchestrator/internal/domain"
)

func TestScheduler_GetReadyTasks(t *testing.T) {
	tasks := []*domain.Task{
		{ID: domain.TaskID{Module: "tech", EpicNum: 0}, Status: domain.StatusNotStarted, DependsOn: nil},
		{ID: domain.TaskID{Module: "tech", EpicNum: 1}, Status: domain.StatusNotStarted, DependsOn: []domain.TaskID{{Module: "tech", EpicNum: 0}}},
		{ID: domain.TaskID{Module: "tech", EpicNum: 2}, Status: domain.StatusNotStarted, DependsOn: []domain.TaskID{{Module: "tech", EpicNum: 1}}},
		{ID: domain.TaskID{Module: "billing", EpicNum: 0}, Status: domain.StatusNotStarted, DependsOn: nil},
	}
	completed := map[string]bool{}

	sched := New(tasks, completed)
	ready := sched.GetReadyTasks(10)

	// E00 from both modules should be ready
	if len(ready) != 2 {
		t.Errorf("Ready count = %d, want 2", len(ready))
	}
}

func TestScheduler_GetReadyTasks_WithCompleted(t *testing.T) {
	tasks := []*domain.Task{
		{ID: domain.TaskID{Module: "tech", EpicNum: 0}, Status: domain.StatusComplete, DependsOn: nil},
		{ID: domain.TaskID{Module: "tech", EpicNum: 1}, Status: domain.StatusNotStarted, DependsOn: []domain.TaskID{{Module: "tech", EpicNum: 0}}},
		{ID: domain.TaskID{Module: "tech", EpicNum: 2}, Status: domain.StatusNotStarted, DependsOn: []domain.TaskID{{Module: "tech", EpicNum: 1}}},
	}
	completed := map[string]bool{"tech/E00": true}

	sched := New(tasks, completed)
	ready := sched.GetReadyTasks(10)

	// Only E01 should be ready (E00 complete, E02 waiting on E01)
	if len(ready) != 1 {
		t.Errorf("Ready count = %d, want 1", len(ready))
	}
	if ready[0].ID.String() != "tech/E01" {
		t.Errorf("Ready task = %s, want tech/E01", ready[0].ID.String())
	}
}

func TestScheduler_GetReadyTasks_Priority(t *testing.T) {
	tasks := []*domain.Task{
		{ID: domain.TaskID{Module: "tech", EpicNum: 0}, Status: domain.StatusNotStarted, Priority: domain.PriorityNormal},
		{ID: domain.TaskID{Module: "billing", EpicNum: 0}, Status: domain.StatusNotStarted, Priority: domain.PriorityHigh},
		{ID: domain.TaskID{Module: "pricing", EpicNum: 0}, Status: domain.StatusNotStarted, Priority: domain.PriorityLow},
	}
	completed := map[string]bool{}

	sched := New(tasks, completed)
	ready := sched.GetReadyTasks(10)

	// High priority should come first
	if ready[0].ID.Module != "billing" {
		t.Errorf("First task module = %s, want billing (high priority)", ready[0].ID.Module)
	}
}

func TestScheduler_GetReadyTasks_Limit(t *testing.T) {
	tasks := []*domain.Task{
		{ID: domain.TaskID{Module: "tech", EpicNum: 0}, Status: domain.StatusNotStarted},
		{ID: domain.TaskID{Module: "billing", EpicNum: 0}, Status: domain.StatusNotStarted},
		{ID: domain.TaskID{Module: "pricing", EpicNum: 0}, Status: domain.StatusNotStarted},
	}
	completed := map[string]bool{}

	sched := New(tasks, completed)
	ready := sched.GetReadyTasks(2)

	if len(ready) != 2 {
		t.Errorf("Ready count = %d, want 2 (limited)", len(ready))
	}
}

func TestScheduler_DependencyDepth(t *testing.T) {
	tasks := []*domain.Task{
		{ID: domain.TaskID{Module: "tech", EpicNum: 0}, Status: domain.StatusNotStarted},
		{ID: domain.TaskID{Module: "tech", EpicNum: 1}, Status: domain.StatusNotStarted, DependsOn: []domain.TaskID{{Module: "tech", EpicNum: 0}}},
		{ID: domain.TaskID{Module: "tech", EpicNum: 2}, Status: domain.StatusNotStarted, DependsOn: []domain.TaskID{{Module: "tech", EpicNum: 1}}},
		{ID: domain.TaskID{Module: "billing", EpicNum: 0}, Status: domain.StatusNotStarted},
	}

	sched := New(tasks, map[string]bool{})

	// tech/E00 unblocks more (E01, E02) than billing/E00 (nothing)
	depth := sched.dependencyDepth("tech/E00")
	if depth != 2 {
		t.Errorf("tech/E00 depth = %d, want 2", depth)
	}

	depth = sched.dependencyDepth("billing/E00")
	if depth != 0 {
		t.Errorf("billing/E00 depth = %d, want 0", depth)
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/scheduler/... -v`
Expected: FAIL (package doesn't exist)

**Step 3: Create scheduler implementation**

Create: `internal/scheduler/scheduler.go`

```go
package scheduler

import (
	"sort"

	"github.com/anthropics/erp-orchestrator/internal/domain"
)

// Scheduler determines which tasks are ready to run
type Scheduler struct {
	tasks     []*domain.Task
	taskMap   map[string]*domain.Task
	completed map[string]bool
	depGraph  map[string][]string // task -> tasks that depend on it
}

// New creates a new Scheduler
func New(tasks []*domain.Task, completed map[string]bool) *Scheduler {
	taskMap := make(map[string]*domain.Task, len(tasks))
	depGraph := make(map[string][]string)

	for _, t := range tasks {
		taskMap[t.ID.String()] = t
		for _, dep := range t.DependsOn {
			depGraph[dep.String()] = append(depGraph[dep.String()], t.ID.String())
		}
	}

	return &Scheduler{
		tasks:     tasks,
		taskMap:   taskMap,
		completed: completed,
		depGraph:  depGraph,
	}
}

// GetReadyTasks returns up to limit tasks that are ready to run
func (s *Scheduler) GetReadyTasks(limit int) []*domain.Task {
	var ready []*domain.Task

	for _, task := range s.tasks {
		if task.IsReady(s.completed) {
			ready = append(ready, task)
		}
	}

	// Sort by priority
	sort.Slice(ready, func(i, j int) bool {
		// 1. Priority (high > normal > low)
		pi, pj := priorityOrder(ready[i].Priority), priorityOrder(ready[j].Priority)
		if pi != pj {
			return pi < pj
		}

		// 2. Dependency depth (unblocks more work)
		di, dj := s.dependencyDepth(ready[i].ID.String()), s.dependencyDepth(ready[j].ID.String())
		if di != dj {
			return di > dj
		}

		// 3. Module grouping (same module as recently completed)
		// 4. Epic number (earlier epics first within same module)
		if ready[i].ID.Module != ready[j].ID.Module {
			return ready[i].ID.Module < ready[j].ID.Module
		}
		return ready[i].ID.EpicNum < ready[j].ID.EpicNum
	})

	if len(ready) > limit {
		ready = ready[:limit]
	}

	return ready
}

// dependencyDepth returns how many tasks depend (transitively) on this task
func (s *Scheduler) dependencyDepth(taskID string) int {
	visited := make(map[string]bool)
	return s.countDependents(taskID, visited)
}

func (s *Scheduler) countDependents(taskID string, visited map[string]bool) int {
	if visited[taskID] {
		return 0
	}
	visited[taskID] = true

	count := 0
	for _, depID := range s.depGraph[taskID] {
		count += 1 + s.countDependents(depID, visited)
	}
	return count
}

func priorityOrder(p domain.Priority) int {
	switch p {
	case domain.PriorityHigh:
		return 0
	case domain.PriorityLow:
		return 2
	default:
		return 1
	}
}

// TopologicalSort returns tasks in dependency order
func (s *Scheduler) TopologicalSort() ([]*domain.Task, error) {
	inDegree := make(map[string]int)
	for _, t := range s.tasks {
		inDegree[t.ID.String()] = 0
	}
	for _, t := range s.tasks {
		for _, dep := range t.DependsOn {
			inDegree[t.ID.String()]++
			_ = dep // use the dependency
		}
	}

	var queue []string
	for id, deg := range inDegree {
		if deg == 0 {
			queue = append(queue, id)
		}
	}

	var result []*domain.Task
	for len(queue) > 0 {
		id := queue[0]
		queue = queue[1:]
		result = append(result, s.taskMap[id])

		for _, depID := range s.depGraph[id] {
			inDegree[depID]--
			if inDegree[depID] == 0 {
				queue = append(queue, depID)
			}
		}
	}

	return result, nil
}
```

**Step 4: Run tests to verify they pass**

Run: `go test ./internal/scheduler/... -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/scheduler/
git commit -m "feat(scheduler): add dependency resolution and priority sorting"
```

---

## Phase 4: Executor (Worktrees and Claude Code)

### Task 4.1: Create Worktree Manager [DONE]

**Files:**
- Create: `internal/executor/worktree.go`
- Create: `internal/executor/worktree_test.go`

**Step 1: Write worktree test**

Create: `internal/executor/worktree_test.go`

```go
package executor

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/anthropics/erp-orchestrator/internal/domain"
)

func setupGitRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()

	cmds := [][]string{
		{"git", "init"},
		{"git", "config", "user.email", "test@test.com"},
		{"git", "config", "user.name", "Test"},
	}

	for _, args := range cmds {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("%v failed: %s", args, out)
		}
	}

	// Create initial commit
	readme := filepath.Join(dir, "README.md")
	os.WriteFile(readme, []byte("# Test"), 0644)

	cmd := exec.Command("git", "add", ".")
	cmd.Dir = dir
	cmd.Run()

	cmd = exec.Command("git", "commit", "-m", "Initial commit")
	cmd.Dir = dir
	cmd.Run()

	return dir
}

func TestWorktreeManager_Create(t *testing.T) {
	repoDir := setupGitRepo(t)
	worktreeDir := t.TempDir()

	mgr := NewWorktreeManager(repoDir, worktreeDir)

	taskID := domain.TaskID{Module: "technical", EpicNum: 5}
	wtPath, err := mgr.Create(taskID)
	if err != nil {
		t.Fatal(err)
	}

	// Verify worktree was created
	if _, err := os.Stat(wtPath); os.IsNotExist(err) {
		t.Error("Worktree directory not created")
	}

	// Verify branch was created
	cmd := exec.Command("git", "branch", "--list", "feat/technical-E05")
	cmd.Dir = repoDir
	out, _ := cmd.Output()
	if len(out) == 0 {
		t.Error("Branch feat/technical-E05 not created")
	}
}

func TestWorktreeManager_Remove(t *testing.T) {
	repoDir := setupGitRepo(t)
	worktreeDir := t.TempDir()

	mgr := NewWorktreeManager(repoDir, worktreeDir)

	taskID := domain.TaskID{Module: "technical", EpicNum: 5}
	wtPath, _ := mgr.Create(taskID)

	if err := mgr.Remove(wtPath); err != nil {
		t.Fatal(err)
	}

	// Verify worktree was removed
	if _, err := os.Stat(wtPath); !os.IsNotExist(err) {
		t.Error("Worktree directory still exists")
	}
}

func TestWorktreeManager_BranchName(t *testing.T) {
	tests := []struct {
		taskID domain.TaskID
		want   string
	}{
		{domain.TaskID{Module: "technical", EpicNum: 5}, "feat/technical-E05"},
		{domain.TaskID{Module: "billing", EpicNum: 0}, "feat/billing-E00"},
	}

	for _, tt := range tests {
		got := BranchName(tt.taskID)
		if got != tt.want {
			t.Errorf("BranchName(%v) = %q, want %q", tt.taskID, got, tt.want)
		}
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/executor/... -v`
Expected: FAIL (package doesn't exist)

**Step 3: Create worktree manager**

Create: `internal/executor/worktree.go`

```go
package executor

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/anthropics/erp-orchestrator/internal/domain"
)

// WorktreeManager handles git worktree operations
type WorktreeManager struct {
	repoDir     string
	worktreeDir string
}

// NewWorktreeManager creates a new WorktreeManager
func NewWorktreeManager(repoDir, worktreeDir string) *WorktreeManager {
	return &WorktreeManager{
		repoDir:     repoDir,
		worktreeDir: worktreeDir,
	}
}

// Create creates a new worktree for a task
func (m *WorktreeManager) Create(taskID domain.TaskID) (string, error) {
	// Generate unique suffix
	suffix := randomSuffix()

	// Worktree path
	dirName := fmt.Sprintf("%s-E%02d-%s", taskID.Module, taskID.EpicNum, suffix)
	wtPath := filepath.Join(m.worktreeDir, dirName)

	// Ensure worktree directory exists
	if err := os.MkdirAll(m.worktreeDir, 0755); err != nil {
		return "", fmt.Errorf("creating worktree dir: %w", err)
	}

	// Branch name
	branch := BranchName(taskID)

	// Create worktree with new branch from main
	cmd := exec.Command("git", "worktree", "add", "-b", branch, wtPath, "HEAD")
	cmd.Dir = m.repoDir
	if out, err := cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("git worktree add: %s: %w", out, err)
	}

	return wtPath, nil
}

// Remove removes a worktree
func (m *WorktreeManager) Remove(wtPath string) error {
	// Get branch name before removing
	cmd := exec.Command("git", "rev-parse", "--abbrev-ref", "HEAD")
	cmd.Dir = wtPath
	branchOut, _ := cmd.Output()
	branch := strings.TrimSpace(string(branchOut))

	// Remove worktree
	cmd = exec.Command("git", "worktree", "remove", "--force", wtPath)
	cmd.Dir = m.repoDir
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git worktree remove: %s: %w", out, err)
	}

	// Optionally delete the branch if not merged
	if branch != "" && branch != "HEAD" {
		cmd = exec.Command("git", "branch", "-D", branch)
		cmd.Dir = m.repoDir
		cmd.Run() // Ignore error if branch doesn't exist
	}

	return nil
}

// List returns all active worktree paths
func (m *WorktreeManager) List() ([]string, error) {
	cmd := exec.Command("git", "worktree", "list", "--porcelain")
	cmd.Dir = m.repoDir
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}

	var paths []string
	for _, line := range strings.Split(string(out), "\n") {
		if strings.HasPrefix(line, "worktree ") {
			path := strings.TrimPrefix(line, "worktree ")
			// Only include worktrees in our worktree directory
			if strings.HasPrefix(path, m.worktreeDir) {
				paths = append(paths, path)
			}
		}
	}

	return paths, nil
}

// BranchName returns the branch name for a task
func BranchName(taskID domain.TaskID) string {
	return fmt.Sprintf("feat/%s-E%02d", taskID.Module, taskID.EpicNum)
}

func randomSuffix() string {
	b := make([]byte, 3)
	rand.Read(b)
	return hex.EncodeToString(b)
}
```

**Step 4: Run tests to verify they pass**

Run: `go test ./internal/executor/... -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/executor/
git commit -m "feat(executor): add git worktree manager"
```

---

### Task 4.2: Create Agent Spawner [DONE]

**Files:**
- Create: `internal/executor/agent.go`
- Create: `internal/executor/agent_test.go`
- Create: `internal/executor/prompt.go`

**Step 1: Write agent test**

Create: `internal/executor/agent_test.go`

```go
package executor

import (
	"context"
	"testing"
	"time"

	"github.com/anthropics/erp-orchestrator/internal/domain"
)

func TestBuildPrompt(t *testing.T) {
	task := &domain.Task{
		ID:          domain.TaskID{Module: "technical", EpicNum: 5},
		Title:       "Validators",
		Description: "Implement input validation",
	}

	epicContent := "# Epic 05: Validators\n\nImplement validators."
	completedDeps := []string{"technical/E04"}

	prompt := BuildPrompt(task, epicContent, "", completedDeps)

	if !containsString(prompt, "Validators") {
		t.Error("Prompt should contain task title")
	}
	if !containsString(prompt, "technical/E04") {
		t.Error("Prompt should contain completed dependencies")
	}
}

func TestAgent_StatusTransitions(t *testing.T) {
	agent := &Agent{
		TaskID: domain.TaskID{Module: "tech", EpicNum: 0},
		Status: AgentQueued,
	}

	if agent.Status != AgentQueued {
		t.Errorf("Initial status = %s, want queued", agent.Status)
	}

	agent.Status = AgentRunning
	agent.StartedAt = timePtr(time.Now())

	if agent.Status != AgentRunning {
		t.Errorf("Status = %s, want running", agent.Status)
	}
}

func TestAgentManager_MaxConcurrency(t *testing.T) {
	mgr := NewAgentManager(2)

	// Add 3 agents
	for i := 0; i < 3; i++ {
		mgr.Add(&Agent{
			TaskID: domain.TaskID{Module: "tech", EpicNum: i},
			Status: AgentQueued,
		})
	}

	// Should only allow 2 to run
	running := mgr.RunningCount()
	queued := mgr.QueuedCount()

	if running > 2 {
		t.Errorf("Running = %d, should not exceed max 2", running)
	}
	if queued+running != 3 {
		t.Errorf("Total agents = %d, want 3", queued+running)
	}
}

func containsString(s, substr string) bool {
	return len(s) > 0 && len(substr) > 0 &&
		(s == substr || len(s) > len(substr) &&
		(s[:len(substr)] == substr || s[len(s)-len(substr):] == substr ||
		containsSubstring(s, substr)))
}

func containsSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func timePtr(t time.Time) *time.Time {
	return &t
}

// Integration test - requires claude CLI
func TestAgent_Run_Integration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test")
	}

	// This would test actual Claude Code invocation
	// Skip for unit tests
	t.Skip("Integration test requires Claude Code CLI")
}

var _ = context.Background // silence unused import
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/executor/... -v`
Expected: FAIL (missing types)

**Step 3: Create prompt builder**

Create: `internal/executor/prompt.go`

```go
package executor

import (
	"fmt"
	"strings"

	"github.com/anthropics/erp-orchestrator/internal/domain"
)

const promptTemplate = `You are implementing: %s

Epic: %s
%s
Dependencies completed: %s

Instructions:
1. Implement the epic requirements
2. Run tests to verify your implementation
3. Ensure all tests pass
4. When complete, create a summary of changes made

Do not ask for clarification. Make reasonable decisions based on the epic content.
`

// BuildPrompt constructs the task prompt for Claude Code
func BuildPrompt(task *domain.Task, epicContent, moduleOverview string, completedDeps []string) string {
	var moduleCtx string
	if moduleOverview != "" {
		moduleCtx = fmt.Sprintf("\nModule context:\n%s\n", moduleOverview)
	}

	depsStr := "None"
	if len(completedDeps) > 0 {
		depsStr = strings.Join(completedDeps, ", ")
	}

	return fmt.Sprintf(promptTemplate,
		task.Title,
		epicContent,
		moduleCtx,
		depsStr,
	)
}

// BuildCommitMessage creates the commit message format
func BuildCommitMessage(task *domain.Task, summary string) string {
	return fmt.Sprintf("feat(%s): implement E%02d - %s\n\n%s",
		task.ID.Module,
		task.ID.EpicNum,
		task.Title,
		summary,
	)
}
```

**Step 4: Create agent implementation**

Create: `internal/executor/agent.go`

```go
package executor

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os/exec"
	"sync"
	"time"

	"github.com/anthropics/erp-orchestrator/internal/domain"
)

// AgentStatus represents the status of an agent
type AgentStatus string

const (
	AgentQueued    AgentStatus = "queued"
	AgentRunning   AgentStatus = "running"
	AgentCompleted AgentStatus = "completed"
	AgentFailed    AgentStatus = "failed"
	AgentStuck     AgentStatus = "stuck"
)

// Agent represents a Claude Code agent working on a task
type Agent struct {
	TaskID       domain.TaskID
	WorktreePath string
	Status       AgentStatus
	StartedAt    *time.Time
	FinishedAt   *time.Time
	Prompt       string
	Output       []string
	Error        error

	cmd    *exec.Cmd
	cancel context.CancelFunc
	mu     sync.Mutex
}

// AgentManager manages concurrent agent execution
type AgentManager struct {
	maxConcurrent int
	agents        map[string]*Agent
	mu            sync.RWMutex
}

// NewAgentManager creates a new AgentManager
func NewAgentManager(maxConcurrent int) *AgentManager {
	return &AgentManager{
		maxConcurrent: maxConcurrent,
		agents:        make(map[string]*Agent),
	}
}

// Add adds an agent to the manager
func (m *AgentManager) Add(agent *Agent) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.agents[agent.TaskID.String()] = agent
}

// Get retrieves an agent by task ID
func (m *AgentManager) Get(taskID string) *Agent {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.agents[taskID]
}

// Remove removes an agent from the manager
func (m *AgentManager) Remove(taskID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.agents, taskID)
}

// RunningCount returns the number of running agents
func (m *AgentManager) RunningCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	count := 0
	for _, a := range m.agents {
		if a.Status == AgentRunning {
			count++
		}
	}
	return count
}

// QueuedCount returns the number of queued agents
func (m *AgentManager) QueuedCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	count := 0
	for _, a := range m.agents {
		if a.Status == AgentQueued {
			count++
		}
	}
	return count
}

// CanStart returns true if another agent can be started
func (m *AgentManager) CanStart() bool {
	return m.RunningCount() < m.maxConcurrent
}

// Start starts an agent with Claude Code
func (a *Agent) Start(ctx context.Context) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.Status != AgentQueued {
		return fmt.Errorf("agent not in queued state: %s", a.Status)
	}

	ctx, cancel := context.WithCancel(ctx)
	a.cancel = cancel

	// Build claude command
	a.cmd = exec.CommandContext(ctx, "claude",
		"--print", // Non-interactive mode
		"--dangerously-skip-permissions",
	)
	a.cmd.Dir = a.WorktreePath
	a.cmd.Stdin = nil // Will write prompt via stdin pipe

	// Capture output
	stdout, err := a.cmd.StdoutPipe()
	if err != nil {
		return err
	}
	stderr, err := a.cmd.StderrPipe()
	if err != nil {
		return err
	}

	// Start the process
	if err := a.cmd.Start(); err != nil {
		return fmt.Errorf("starting claude: %w", err)
	}

	now := time.Now()
	a.StartedAt = &now
	a.Status = AgentRunning

	// Stream output in background
	go a.streamOutput(stdout, stderr)

	return nil
}

func (a *Agent) streamOutput(stdout, stderr io.ReadCloser) {
	var wg sync.WaitGroup
	wg.Add(2)

	readLines := func(r io.Reader) {
		defer wg.Done()
		scanner := bufio.NewScanner(r)
		for scanner.Scan() {
			a.mu.Lock()
			a.Output = append(a.Output, scanner.Text())
			a.mu.Unlock()
		}
	}

	go readLines(stdout)
	go readLines(stderr)
	wg.Wait()

	// Wait for process to finish
	err := a.cmd.Wait()

	a.mu.Lock()
	defer a.mu.Unlock()

	now := time.Now()
	a.FinishedAt = &now

	if err != nil {
		a.Status = AgentFailed
		a.Error = err
	} else {
		a.Status = AgentCompleted
	}
}

// Stop gracefully stops the agent
func (a *Agent) Stop() {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.cancel != nil {
		a.cancel()
	}
}

// GetOutput returns a copy of the output lines
func (a *Agent) GetOutput() []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	result := make([]string, len(a.Output))
	copy(result, a.Output)
	return result
}

// Duration returns how long the agent has been running
func (a *Agent) Duration() time.Duration {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.StartedAt == nil {
		return 0
	}
	if a.FinishedAt != nil {
		return a.FinishedAt.Sub(*a.StartedAt)
	}
	return time.Since(*a.StartedAt)
}
```

**Step 5: Run tests to verify they pass**

Run: `go test ./internal/executor/... -v`
Expected: PASS

**Step 6: Commit**

```bash
git add internal/executor/
git commit -m "feat(executor): add agent manager and Claude Code spawning"
```

---

## Phase 5: PR Automation

### Task 5.1: Create PR Bot [DONE]

**Files:**
- Create: `internal/prbot/prbot.go`
- Create: `internal/prbot/prbot_test.go`
- Create: `internal/prbot/semantic.go`

**Step 1: Write PR bot test**

Create: `internal/prbot/prbot_test.go`

```go
package prbot

import (
	"testing"

	"github.com/anthropics/erp-orchestrator/internal/domain"
)

func TestBuildPRBody(t *testing.T) {
	task := &domain.Task{
		ID:       domain.TaskID{Module: "technical", EpicNum: 5},
		Title:    "Validators",
		FilePath: "docs/plans/technical-module/epic-05-validators.md",
	}

	body := BuildPRBody(task, "Added validation functions", 15, "2m30s")

	if !containsString(body, "Validators") {
		t.Error("Body should contain task title")
	}
	if !containsString(body, "15 tests passed") {
		t.Error("Body should contain test count")
	}
	if !containsString(body, "ERP Orchestrator") {
		t.Error("Body should contain attribution")
	}
}

func TestAnalyzeDiff_Security(t *testing.T) {
	diff := `
diff --git a/auth/login.go b/auth/login.go
+func validatePassword(password string) bool {
+    return bcrypt.CompareHashAndPassword(hash, []byte(password))
+}
`
	category := AnalyzeDiff(diff)
	if category != CategorySecurity {
		t.Errorf("Category = %s, want security", category)
	}
}

func TestAnalyzeDiff_Architecture(t *testing.T) {
	diff := `
diff --git a/go.mod b/go.mod
+require github.com/newdep/pkg v1.0.0
`
	category := AnalyzeDiff(diff)
	if category != CategoryArchitecture {
		t.Errorf("Category = %s, want architecture", category)
	}
}

func TestAnalyzeDiff_Migrations(t *testing.T) {
	diff := `
diff --git a/migrations/001_create_users.sql b/migrations/001_create_users.sql
+CREATE TABLE users (
+    id SERIAL PRIMARY KEY
+);
`
	category := AnalyzeDiff(diff)
	if category != CategoryMigrations {
		t.Errorf("Category = %s, want migrations", category)
	}
}

func TestAnalyzeDiff_Routine(t *testing.T) {
	diff := `
diff --git a/utils/format.go b/utils/format.go
+func FormatDate(t time.Time) string {
+    return t.Format("2006-01-02")
+}
`
	category := AnalyzeDiff(diff)
	if category != CategoryRoutine {
		t.Errorf("Category = %s, want routine", category)
	}
}

func TestShouldAutoMerge(t *testing.T) {
	tests := []struct {
		category    Category
		needsReview bool
		want        bool
	}{
		{CategoryRoutine, false, true},
		{CategoryRoutine, true, false},
		{CategorySecurity, false, false},
		{CategoryArchitecture, false, false},
		{CategoryMigrations, false, false},
	}

	for _, tt := range tests {
		got := ShouldAutoMerge(tt.category, tt.needsReview)
		if got != tt.want {
			t.Errorf("ShouldAutoMerge(%s, %v) = %v, want %v",
				tt.category, tt.needsReview, got, tt.want)
		}
	}
}

func containsString(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/prbot/... -v`
Expected: FAIL (package doesn't exist)

**Step 3: Create semantic analyzer**

Create: `internal/prbot/semantic.go`

```go
package prbot

import (
	"regexp"
	"strings"
)

// Category represents the type of changes in a PR
type Category string

const (
	CategorySecurity     Category = "security"
	CategoryArchitecture Category = "architecture"
	CategoryMigrations   Category = "migrations"
	CategoryRoutine      Category = "routine"
)

var (
	securityPatterns = []string{
		`(?i)auth`,
		`(?i)password`,
		`(?i)credential`,
		`(?i)secret`,
		`(?i)token`,
		`(?i)encrypt`,
		`(?i)decrypt`,
		`(?i)permission`,
		`(?i)bcrypt`,
		`(?i)jwt`,
		`(?i)oauth`,
		`(?i)session`,
	}

	architecturePatterns = []string{
		`go\.mod`,
		`go\.sum`,
		`package\.json`,
		`(?i)api/`,
		`(?i)interface\s+\w+`,
		`(?i)public\s+(func|type)`,
	}

	migrationPatterns = []string{
		`migrations/`,
		`(?i)CREATE\s+TABLE`,
		`(?i)ALTER\s+TABLE`,
		`(?i)DROP\s+TABLE`,
		`(?i)\.sql$`,
	}
)

// AnalyzeDiff categorizes a diff by its content
func AnalyzeDiff(diff string) Category {
	// Check in order of priority
	if matchesAny(diff, securityPatterns) {
		return CategorySecurity
	}
	if matchesAny(diff, migrationPatterns) {
		return CategoryMigrations
	}
	if matchesAny(diff, architecturePatterns) {
		return CategoryArchitecture
	}
	return CategoryRoutine
}

func matchesAny(text string, patterns []string) bool {
	for _, pattern := range patterns {
		re := regexp.MustCompile(pattern)
		if re.MatchString(text) {
			return true
		}
	}
	return false
}

// ShouldAutoMerge returns true if the PR should be auto-merged
func ShouldAutoMerge(category Category, needsReview bool) bool {
	if needsReview {
		return false
	}
	return category == CategoryRoutine
}

// GetLabels returns labels to apply based on category
func GetLabels(category Category) []string {
	switch category {
	case CategorySecurity:
		return []string{"needs-human-review", "security"}
	case CategoryArchitecture:
		return []string{"needs-human-review", "architecture"}
	case CategoryMigrations:
		return []string{"needs-human-review", "database"}
	default:
		return []string{"auto-merge"}
	}
}

// ExtractChangeSummary attempts to summarize changes from diff
func ExtractChangeSummary(diff string) string {
	var files []string
	lines := strings.Split(diff, "\n")

	for _, line := range lines {
		if strings.HasPrefix(line, "diff --git") {
			parts := strings.Split(line, " ")
			if len(parts) >= 4 {
				file := strings.TrimPrefix(parts[3], "b/")
				files = append(files, file)
			}
		}
	}

	if len(files) == 0 {
		return "Changes made"
	}
	if len(files) == 1 {
		return "Modified " + files[0]
	}
	return "Modified " + strings.Join(files[:min(3, len(files))], ", ")
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
```

**Step 4: Create PR bot implementation**

Create: `internal/prbot/prbot.go`

```go
package prbot

import (
	"fmt"
	"os/exec"
	"strings"

	"github.com/anthropics/erp-orchestrator/internal/domain"
)

const prBodyTemplate = `## Summary
Implements %s

## Changes
%s

## Test Results
- ✅ %d tests passed
- 🕐 %s

## Epic Reference
[%s](%s)

---
🤖 Autonomous implementation by ERP Orchestrator
`

// PRBot handles PR creation and management
type PRBot struct {
	repoDir string
}

// NewPRBot creates a new PRBot
func NewPRBot(repoDir string) *PRBot {
	return &PRBot{repoDir: repoDir}
}

// BuildPRBody constructs the PR body
func BuildPRBody(task *domain.Task, changeSummary string, testsPassed int, duration string) string {
	return fmt.Sprintf(prBodyTemplate,
		task.Title,
		changeSummary,
		testsPassed,
		duration,
		task.FilePath,
		task.FilePath,
	)
}

// CreatePR creates a pull request using gh CLI
func (p *PRBot) CreatePR(worktreePath string, task *domain.Task, body string) (int, string, error) {
	title := fmt.Sprintf("feat(%s): implement E%02d - %s",
		task.ID.Module,
		task.ID.EpicNum,
		task.Title,
	)

	// Push the branch first
	branch := fmt.Sprintf("feat/%s-E%02d", task.ID.Module, task.ID.EpicNum)
	pushCmd := exec.Command("git", "push", "-u", "origin", branch)
	pushCmd.Dir = worktreePath
	if out, err := pushCmd.CombinedOutput(); err != nil {
		return 0, "", fmt.Errorf("git push: %s: %w", out, err)
	}

	// Create PR
	cmd := exec.Command("gh", "pr", "create",
		"--title", title,
		"--body", body,
		"--head", branch,
	)
	cmd.Dir = worktreePath
	out, err := cmd.CombinedOutput()
	if err != nil {
		return 0, "", fmt.Errorf("gh pr create: %s: %w", out, err)
	}

	// Parse PR URL from output
	url := strings.TrimSpace(string(out))
	prNum := extractPRNumber(url)

	return prNum, url, nil
}

// AddLabels adds labels to a PR
func (p *PRBot) AddLabels(prNumber int, labels []string) error {
	args := []string{"pr", "edit", fmt.Sprintf("%d", prNumber)}
	for _, label := range labels {
		args = append(args, "--add-label", label)
	}

	cmd := exec.Command("gh", args...)
	cmd.Dir = p.repoDir
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("gh pr edit: %s: %w", out, err)
	}
	return nil
}

// MergePR merges a PR with squash
func (p *PRBot) MergePR(prNumber int) error {
	cmd := exec.Command("gh", "pr", "merge", fmt.Sprintf("%d", prNumber),
		"--squash",
		"--delete-branch",
	)
	cmd.Dir = p.repoDir
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("gh pr merge: %s: %w", out, err)
	}
	return nil
}

// GetDiff gets the diff for a PR
func (p *PRBot) GetDiff(prNumber int) (string, error) {
	cmd := exec.Command("gh", "pr", "diff", fmt.Sprintf("%d", prNumber))
	cmd.Dir = p.repoDir
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return string(out), nil
}

func extractPRNumber(url string) int {
	// URL format: https://github.com/owner/repo/pull/123
	parts := strings.Split(url, "/")
	if len(parts) > 0 {
		var num int
		fmt.Sscanf(parts[len(parts)-1], "%d", &num)
		return num
	}
	return 0
}
```

**Step 5: Run tests to verify they pass**

Run: `go test ./internal/prbot/... -v`
Expected: PASS

**Step 6: Commit**

```bash
git add internal/prbot/
git commit -m "feat(prbot): add PR creation and semantic analysis"
```

---

## Phase 6: Observer and CLI

### Task 6.1: Create Observer

**Files:**
- Create: `internal/observer/observer.go`
- Create: `internal/observer/observer_test.go`

**Step 1: Write observer test**

Create: `internal/observer/observer_test.go`

```go
package observer

import (
	"testing"
	"time"

	"github.com/anthropics/erp-orchestrator/internal/domain"
	"github.com/anthropics/erp-orchestrator/internal/executor"
)

func TestObserver_DetectStuck(t *testing.T) {
	obs := New(5 * time.Minute)

	agent := &executor.Agent{
		TaskID: domain.TaskID{Module: "tech", EpicNum: 0},
		Status: executor.AgentRunning,
	}

	// Set started time to 10 minutes ago
	started := time.Now().Add(-10 * time.Minute)
	agent.StartedAt = &started

	if !obs.IsStuck(agent) {
		t.Error("Agent running for 10 minutes should be detected as stuck")
	}
}

func TestObserver_NotStuck(t *testing.T) {
	obs := New(5 * time.Minute)

	agent := &executor.Agent{
		TaskID: domain.TaskID{Module: "tech", EpicNum: 0},
		Status: executor.AgentRunning,
	}

	// Set started time to 2 minutes ago
	started := time.Now().Add(-2 * time.Minute)
	agent.StartedAt = &started

	if obs.IsStuck(agent) {
		t.Error("Agent running for 2 minutes should not be stuck")
	}
}

func TestObserver_Metrics(t *testing.T) {
	obs := New(5 * time.Minute)

	obs.RecordCompletion("tech/E00", 5*time.Minute, 1000, 500)
	obs.RecordCompletion("tech/E01", 10*time.Minute, 2000, 1000)

	metrics := obs.GetMetrics()

	if metrics.TotalCompleted != 2 {
		t.Errorf("TotalCompleted = %d, want 2", metrics.TotalCompleted)
	}
	if metrics.TotalTokensInput != 3000 {
		t.Errorf("TotalTokensInput = %d, want 3000", metrics.TotalTokensInput)
	}
	if metrics.AvgDuration != 7*time.Minute+30*time.Second {
		t.Errorf("AvgDuration = %v, want 7m30s", metrics.AvgDuration)
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/observer/... -v`
Expected: FAIL (package doesn't exist)

**Step 3: Create observer implementation**

Create: `internal/observer/observer.go`

```go
package observer

import (
	"sync"
	"time"

	"github.com/anthropics/erp-orchestrator/internal/executor"
)

// Observer monitors agent execution and collects metrics
type Observer struct {
	stuckThreshold time.Duration

	completions []completion
	mu          sync.RWMutex
}

type completion struct {
	TaskID       string
	Duration     time.Duration
	TokensInput  int
	TokensOutput int
	CompletedAt  time.Time
}

// Metrics holds aggregated metrics
type Metrics struct {
	TotalCompleted   int
	TotalFailed      int
	TotalTokensInput int
	TotalTokensOutput int
	AvgDuration      time.Duration
}

// New creates a new Observer
func New(stuckThreshold time.Duration) *Observer {
	return &Observer{
		stuckThreshold: stuckThreshold,
	}
}

// IsStuck returns true if an agent appears to be stuck
func (o *Observer) IsStuck(agent *executor.Agent) bool {
	if agent.Status != executor.AgentRunning {
		return false
	}
	if agent.StartedAt == nil {
		return false
	}
	return time.Since(*agent.StartedAt) > o.stuckThreshold
}

// RecordCompletion records a task completion
func (o *Observer) RecordCompletion(taskID string, duration time.Duration, tokensIn, tokensOut int) {
	o.mu.Lock()
	defer o.mu.Unlock()

	o.completions = append(o.completions, completion{
		TaskID:       taskID,
		Duration:     duration,
		TokensInput:  tokensIn,
		TokensOutput: tokensOut,
		CompletedAt:  time.Now(),
	})
}

// GetMetrics returns aggregated metrics
func (o *Observer) GetMetrics() Metrics {
	o.mu.RLock()
	defer o.mu.RUnlock()

	var metrics Metrics
	var totalDuration time.Duration

	for _, c := range o.completions {
		metrics.TotalCompleted++
		metrics.TotalTokensInput += c.TokensInput
		metrics.TotalTokensOutput += c.TokensOutput
		totalDuration += c.Duration
	}

	if metrics.TotalCompleted > 0 {
		metrics.AvgDuration = totalDuration / time.Duration(metrics.TotalCompleted)
	}

	return metrics
}

// GetRecentCompletions returns completions from the last duration
func (o *Observer) GetRecentCompletions(since time.Duration) []string {
	o.mu.RLock()
	defer o.mu.RUnlock()

	cutoff := time.Now().Add(-since)
	var result []string

	for _, c := range o.completions {
		if c.CompletedAt.After(cutoff) {
			result = append(result, c.TaskID)
		}
	}

	return result
}
```

**Step 4: Run tests to verify they pass**

Run: `go test ./internal/observer/... -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/observer/
git commit -m "feat(observer): add metrics collection and stuck detection"
```

---

### Task 6.2: Create CLI

**Files:**
- Create: `cmd/erp-orch/main.go`
- Create: `cmd/erp-orch/commands.go`

**Step 1: Create main entrypoint**

Create: `cmd/erp-orch/main.go`

```go
package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var (
	configPath string
	rootCmd    = &cobra.Command{
		Use:   "erp-orch",
		Short: "ERP Orchestrator - Autonomous development manager",
		Long: `ERP Orchestrator manages Claude Code agents working on EnergyERP tasks.
It parses markdown plans, dispatches work to agents in git worktrees,
and handles the full PR lifecycle through to merge.`,
	}
)

func init() {
	rootCmd.PersistentFlags().StringVar(&configPath, "config", "", "config file path")
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
```

**Step 2: Create commands**

Create: `cmd/erp-orch/commands.go`

```go
package main

import (
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/anthropics/erp-orchestrator/internal/config"
	"github.com/anthropics/erp-orchestrator/internal/domain"
	"github.com/anthropics/erp-orchestrator/internal/parser"
	"github.com/anthropics/erp-orchestrator/internal/scheduler"
	"github.com/anthropics/erp-orchestrator/internal/taskstore"
	"github.com/spf13/cobra"
)

var (
	startCount  int
	startModule string
	listStatus  string
	listModule  string
)

func init() {
	// start command
	startCmd := &cobra.Command{
		Use:   "start [TASK...]",
		Short: "Start tasks",
		RunE:  runStart,
	}
	startCmd.Flags().IntVar(&startCount, "count", 3, "number of tasks to start")
	startCmd.Flags().StringVar(&startModule, "module", "", "filter by module")
	rootCmd.AddCommand(startCmd)

	// status command
	statusCmd := &cobra.Command{
		Use:   "status",
		Short: "Show current status",
		RunE:  runStatus,
	}
	rootCmd.AddCommand(statusCmd)

	// list command
	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List tasks",
		RunE:  runList,
	}
	listCmd.Flags().StringVar(&listStatus, "status", "", "filter by status")
	listCmd.Flags().StringVar(&listModule, "module", "", "filter by module")
	rootCmd.AddCommand(listCmd)

	// sync command
	syncCmd := &cobra.Command{
		Use:   "sync",
		Short: "Sync tasks from markdown files",
		RunE:  runSync,
	}
	rootCmd.AddCommand(syncCmd)

	// logs command
	logsCmd := &cobra.Command{
		Use:   "logs TASK",
		Short: "View logs for a task",
		Args:  cobra.ExactArgs(1),
		RunE:  runLogs,
	}
	rootCmd.AddCommand(logsCmd)
}

func loadConfig() (*config.Config, error) {
	path := configPath
	if path == "" {
		path = config.DefaultConfigPath()
	}
	return config.Load(path)
}

func runStart(cmd *cobra.Command, args []string) error {
	cfg, err := loadConfig()
	if err != nil {
		return err
	}

	store, err := taskstore.New(cfg.General.DatabasePath)
	if err != nil {
		return err
	}
	defer store.Close()

	// If specific tasks provided, start those
	if len(args) > 0 {
		for _, taskID := range args {
			fmt.Printf("Starting task: %s\n", taskID)
			// TODO: Actually start the task
		}
		return nil
	}

	// Otherwise, get ready tasks from scheduler
	tasks, err := store.ListTasks(taskstore.ListOptions{Module: startModule})
	if err != nil {
		return err
	}

	completed, err := store.GetCompletedTaskIDs()
	if err != nil {
		return err
	}

	sched := scheduler.New(tasks, completed)
	ready := sched.GetReadyTasks(startCount)

	if len(ready) == 0 {
		fmt.Println("No tasks ready to start")
		return nil
	}

	fmt.Printf("Starting %d tasks:\n", len(ready))
	for _, task := range ready {
		fmt.Printf("  - %s: %s\n", task.ID.String(), task.Title)
	}

	return nil
}

func runStatus(cmd *cobra.Command, args []string) error {
	cfg, err := loadConfig()
	if err != nil {
		return err
	}

	store, err := taskstore.New(cfg.General.DatabasePath)
	if err != nil {
		return err
	}
	defer store.Close()

	tasks, err := store.ListTasks(taskstore.ListOptions{})
	if err != nil {
		return err
	}

	var notStarted, inProgress, complete int
	for _, t := range tasks {
		switch t.Status {
		case domain.StatusNotStarted:
			notStarted++
		case domain.StatusInProgress:
			inProgress++
		case domain.StatusComplete:
			complete++
		}
	}

	fmt.Printf("Tasks: %d total | %d not started | %d in progress | %d complete\n",
		len(tasks), notStarted, inProgress, complete)

	return nil
}

func runList(cmd *cobra.Command, args []string) error {
	cfg, err := loadConfig()
	if err != nil {
		return err
	}

	store, err := taskstore.New(cfg.General.DatabasePath)
	if err != nil {
		return err
	}
	defer store.Close()

	opts := taskstore.ListOptions{
		Module: listModule,
		Status: domain.TaskStatus(listStatus),
	}

	tasks, err := store.ListTasks(opts)
	if err != nil {
		return err
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "ID\tTITLE\tSTATUS\tPRIORITY")
	for _, t := range tasks {
		priority := string(t.Priority)
		if priority == "" {
			priority = "-"
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\n",
			t.ID.String(), t.Title, t.Status, priority)
	}
	w.Flush()

	return nil
}

func runSync(cmd *cobra.Command, args []string) error {
	cfg, err := loadConfig()
	if err != nil {
		return err
	}

	if cfg.General.ProjectRoot == "" {
		return fmt.Errorf("project_root not configured")
	}

	plansDir := cfg.General.ProjectRoot + "/docs/plans"
	tasks, err := parser.ParsePlansDir(plansDir)
	if err != nil {
		return err
	}

	store, err := taskstore.New(cfg.General.DatabasePath)
	if err != nil {
		return err
	}
	defer store.Close()

	for _, task := range tasks {
		if err := store.UpsertTask(task); err != nil {
			return fmt.Errorf("upserting %s: %w", task.ID.String(), err)
		}
	}

	fmt.Printf("Synced %d tasks from %s\n", len(tasks), plansDir)
	return nil
}

func runLogs(cmd *cobra.Command, args []string) error {
	taskID := args[0]
	fmt.Printf("Logs for task: %s\n", taskID)
	fmt.Println("(not implemented)")
	return nil
}
```

**Step 3: Add Cobra dependency**

Run:
```bash
go get github.com/spf13/cobra
```

**Step 4: Build and verify**

Run:
```bash
go build -o erp-orch ./cmd/erp-orch
./erp-orch --help
```

Expected: Help output showing available commands

**Step 5: Commit**

```bash
git add cmd/erp-orch/ go.mod go.sum
git commit -m "feat(cli): add CLI with start, status, list, sync commands"
```

---

## Phase 7: TUI Dashboard

### Task 7.1: Create TUI Model

**Files:**
- Create: `tui/model.go`
- Create: `tui/view.go`
- Create: `tui/update.go`

**Step 1: Add bubbletea dependencies**

Run:
```bash
go get github.com/charmbracelet/bubbletea
go get github.com/charmbracelet/lipgloss
```

**Step 2: Create TUI model**

Create: `tui/model.go`

```go
package tui

import (
	"time"

	"github.com/anthropics/erp-orchestrator/internal/domain"
	"github.com/anthropics/erp-orchestrator/internal/executor"
	tea "github.com/charmbracelet/bubbletea"
)

// Model is the TUI application model
type Model struct {
	// Data
	agents    []*AgentView
	queued    []*domain.Task
	flagged   []*FlaggedPR

	// Stats
	activeCount    int
	maxActive      int
	completedToday int

	// UI state
	width      int
	height     int
	activeTab  int
	selectedRow int

	// Refresh
	lastRefresh time.Time
}

// AgentView represents an agent in the TUI
type AgentView struct {
	TaskID   string
	Title    string
	Duration time.Duration
	Status   executor.AgentStatus
	Progress string
}

// FlaggedPR represents a PR needing attention
type FlaggedPR struct {
	TaskID   string
	PRNumber int
	Reason   string
}

// NewModel creates a new TUI model
func NewModel(maxActive int) Model {
	return Model{
		maxActive: maxActive,
		activeTab: 0,
	}
}

// Init initializes the model
func (m Model) Init() tea.Cmd {
	return tea.Batch(
		tickCmd(),
	)
}

// TickMsg triggers a refresh
type TickMsg time.Time

func tickCmd() tea.Cmd {
	return tea.Tick(time.Second, func(t time.Time) tea.Msg {
		return TickMsg(t)
	})
}
```

**Step 3: Create TUI view**

Create: `tui/view.go`

```go
package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

var (
	titleStyle = lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("205")).
		Padding(0, 1)

	headerStyle = lipgloss.NewStyle().
		Background(lipgloss.Color("236")).
		Foreground(lipgloss.Color("255")).
		Padding(0, 1)

	sectionStyle = lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("240")).
		Padding(0, 1)

	runningStyle = lipgloss.NewStyle().
		Foreground(lipgloss.Color("42"))

	queuedStyle = lipgloss.NewStyle().
		Foreground(lipgloss.Color("244"))

	warningStyle = lipgloss.NewStyle().
		Foreground(lipgloss.Color("214"))

	statusBarStyle = lipgloss.NewStyle().
		Background(lipgloss.Color("236")).
		Foreground(lipgloss.Color("255"))
)

// View renders the TUI
func (m Model) View() string {
	if m.width == 0 {
		return "Loading..."
	}

	var b strings.Builder

	// Header
	header := fmt.Sprintf(" ERP Orchestrator │ Active: %d/%d │ Queued: %d │ Completed today: %d │ Flagged: %d ",
		m.activeCount, m.maxActive, len(m.queued), m.completedToday, len(m.flagged))
	b.WriteString(headerStyle.Width(m.width).Render(header))
	b.WriteString("\n")

	// Running section
	runningSection := m.renderRunning()
	b.WriteString(sectionStyle.Width(m.width - 2).Render(runningSection))
	b.WriteString("\n")

	// Queued section
	queuedSection := m.renderQueued()
	b.WriteString(sectionStyle.Width(m.width - 2).Render(queuedSection))
	b.WriteString("\n")

	// Attention section
	if len(m.flagged) > 0 {
		attentionSection := m.renderAttention()
		b.WriteString(sectionStyle.Width(m.width - 2).Render(attentionSection))
		b.WriteString("\n")
	}

	// Status bar
	statusBar := " [r]efresh [l]ogs [s]tart batch [p]ause [q]uit "
	b.WriteString(statusBarStyle.Width(m.width).Render(statusBar))

	return b.String()
}

func (m Model) renderRunning() string {
	var b strings.Builder
	b.WriteString(titleStyle.Render("RUNNING"))
	b.WriteString("\n")

	if len(m.agents) == 0 {
		b.WriteString(queuedStyle.Render("  No agents running"))
		return b.String()
	}

	for _, agent := range m.agents {
		if agent.Status == "running" {
			line := fmt.Sprintf("  ● %-15s %-20s %5s  %s",
				agent.TaskID, truncate(agent.Title, 20),
				formatDuration(agent.Duration), agent.Progress)
			b.WriteString(runningStyle.Render(line))
			b.WriteString("\n")
		}
	}

	return strings.TrimSuffix(b.String(), "\n")
}

func (m Model) renderQueued() string {
	var b strings.Builder
	b.WriteString(titleStyle.Render("QUEUED (next 5)"))
	b.WriteString("\n")

	if len(m.queued) == 0 {
		b.WriteString(queuedStyle.Render("  No tasks queued"))
		return b.String()
	}

	limit := 5
	if len(m.queued) < limit {
		limit = len(m.queued)
	}

	for i := 0; i < limit; i++ {
		task := m.queued[i]
		waiting := ""
		if len(task.DependsOn) > 0 {
			waiting = fmt.Sprintf("(waiting: %s)", task.DependsOn[0].String())
		} else {
			waiting = "(ready)"
		}
		line := fmt.Sprintf("  ○ %-15s %-20s %s",
			task.ID.String(), truncate(task.Title, 20), waiting)
		b.WriteString(queuedStyle.Render(line))
		b.WriteString("\n")
	}

	return strings.TrimSuffix(b.String(), "\n")
}

func (m Model) renderAttention() string {
	var b strings.Builder
	b.WriteString(titleStyle.Render("NEEDS ATTENTION"))
	b.WriteString("\n")

	for _, pr := range m.flagged {
		line := fmt.Sprintf("  ⚠ %-15s PR #%d %s",
			pr.TaskID, pr.PRNumber, pr.Reason)
		b.WriteString(warningStyle.Render(line))
		b.WriteString("\n")
	}

	return strings.TrimSuffix(b.String(), "\n")
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max-3] + "..."
}

func formatDuration(d time.Duration) string {
	m := int(d.Minutes())
	return fmt.Sprintf("%dm", m)
}
```

**Step 4: Create TUI update**

Create: `tui/update.go`

```go
package tui

import (
	tea "github.com/charmbracelet/bubbletea"
)

// Update handles messages
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			return m, tea.Quit
		case "r":
			return m, tickCmd()
		case "j", "down":
			m.selectedRow++
		case "k", "up":
			if m.selectedRow > 0 {
				m.selectedRow--
			}
		case "tab":
			m.activeTab = (m.activeTab + 1) % 3
		}

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height

	case TickMsg:
		// Refresh data would happen here
		return m, tickCmd()
	}

	return m, nil
}

// SetAgents updates the agents list
func (m *Model) SetAgents(agents []*AgentView) {
	m.agents = agents
	m.activeCount = 0
	for _, a := range agents {
		if a.Status == "running" {
			m.activeCount++
		}
	}
}
```

**Step 5: Add TUI command to CLI**

Edit `cmd/erp-orch/commands.go` to add TUI command:

Add to init():
```go
	// tui command
	tuiCmd := &cobra.Command{
		Use:   "tui",
		Short: "Launch TUI dashboard",
		RunE:  runTUI,
	}
	rootCmd.AddCommand(tuiCmd)
```

Add runTUI function:
```go
func runTUI(cmd *cobra.Command, args []string) error {
	cfg, err := loadConfig()
	if err != nil {
		return err
	}

	model := tui.NewModel(cfg.General.MaxParallelAgents)
	p := tea.NewProgram(model, tea.WithAltScreen())

	if _, err := p.Run(); err != nil {
		return err
	}

	return nil
}
```

Add import for tui package.

**Step 6: Build and test TUI**

Run:
```bash
go build -o erp-orch ./cmd/erp-orch
./erp-orch tui
```

Expected: TUI dashboard renders (press q to quit)

**Step 7: Commit**

```bash
git add tui/ cmd/erp-orch/
git commit -m "feat(tui): add bubbletea TUI dashboard"
```

---

## Verification Checklist

After completing all phases:

- [ ] `go test ./...` passes
- [ ] `go build ./cmd/erp-orch` succeeds
- [ ] `./erp-orch --help` shows all commands
- [ ] `./erp-orch tui` launches dashboard
- [ ] Database migrations run on first use

## Next Steps (Future Work)

1. **Web UI**: Svelte SPA with real-time updates
2. **Status Sync**: Update README.md with task status
3. **Scheduled Batches**: Cron-based task scheduling
4. **Notifications**: Desktop and Slack notifications
