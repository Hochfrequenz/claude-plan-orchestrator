# ERP Orchestrator Phase 2 Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add Web UI, status sync, scheduled batches, and notifications to complete the orchestrator's feature set.

**Architecture:** The Web UI is a Svelte SPA served by a Go HTTP API with SSE for real-time updates. Status sync writes back to markdown files. Scheduled batches use cron expressions. Notifications support desktop (via libnotify/osascript) and Slack webhooks.

**Tech Stack:** Svelte 5, Go net/http, Server-Sent Events (SSE), robfig/cron, beeep (desktop notifications)

**Prerequisites:** Complete Phase 1 (core orchestrator) before starting this plan.

---

## Phase 8: Status Sync

### Task 8.1: Create Sync Package

**Files:**
- Create: `internal/sync/sync.go`
- Create: `internal/sync/sync_test.go`
- Create: `internal/sync/readme.go`

**Step 1: Write sync test**

Create: `internal/sync/sync_test.go`

```go
package sync

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/anthropics/erp-orchestrator/internal/domain"
)

func TestUpdateREADMEStatus(t *testing.T) {
	dir := t.TempDir()
	readmePath := filepath.Join(dir, "README.md")

	content := `# Project

## Technical Module

| Epic | Status | Description |
|------|--------|-------------|
| E00 | 🔴 | Scaffolding |
| E01 | 🔴 | Entities |
| E02 | 🔴 | Validators |

## Billing Module

| Epic | Status | Description |
|------|--------|-------------|
| E00 | 🔴 | Setup |
`
	os.WriteFile(readmePath, []byte(content), 0644)

	syncer := New(dir)

	// Update E00 to in_progress
	err := syncer.UpdateTaskStatus(domain.TaskID{Module: "technical", EpicNum: 0}, domain.StatusInProgress)
	if err != nil {
		t.Fatal(err)
	}

	// Read back
	updated, _ := os.ReadFile(readmePath)
	if !containsString(string(updated), "| E00 | 🟡 |") {
		t.Error("E00 should be updated to 🟡")
	}

	// Update E00 to complete
	err = syncer.UpdateTaskStatus(domain.TaskID{Module: "technical", EpicNum: 0}, domain.StatusComplete)
	if err != nil {
		t.Fatal(err)
	}

	updated, _ = os.ReadFile(readmePath)
	if !containsString(string(updated), "| E00 | 🟢 |") {
		t.Error("E00 should be updated to 🟢")
	}
}

func TestUpdateEpicFile(t *testing.T) {
	dir := t.TempDir()
	moduleDir := filepath.Join(dir, "technical-module")
	os.MkdirAll(moduleDir, 0755)

	epicPath := filepath.Join(moduleDir, "epic-05-validators.md")
	content := `# Epic 05: Validators

Implement input validation.
`
	os.WriteFile(epicPath, []byte(content), 0644)

	syncer := New(dir)
	err := syncer.UpdateEpicStatus(epicPath, domain.StatusComplete, 142, "2026-01-12")
	if err != nil {
		t.Fatal(err)
	}

	updated, _ := os.ReadFile(epicPath)
	if !containsString(string(updated), "status=complete") {
		t.Error("Epic should have status comment")
	}
	if !containsString(string(updated), "pr=#142") {
		t.Error("Epic should have PR reference")
	}
}

func TestParseStatusEmoji(t *testing.T) {
	tests := []struct {
		status domain.TaskStatus
		want   string
	}{
		{domain.StatusNotStarted, "🔴"},
		{domain.StatusInProgress, "🟡"},
		{domain.StatusComplete, "🟢"},
	}

	for _, tt := range tests {
		got := StatusEmoji(tt.status)
		if got != tt.want {
			t.Errorf("StatusEmoji(%s) = %s, want %s", tt.status, got, tt.want)
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

Run: `go test ./internal/sync/... -v`
Expected: FAIL (package doesn't exist)

**Step 3: Create sync implementation**

Create: `internal/sync/sync.go`

```go
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
		content = re.ReplaceAll(content, []byte(statusComment+"$1"), 1)
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
```

**Step 4: Create README helper**

Create: `internal/sync/readme.go`

```go
package sync

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/anthropics/erp-orchestrator/internal/domain"
)

// GenerateStatusTable generates a markdown status table for a module
func GenerateStatusTable(tasks []*domain.Task) string {
	var b strings.Builder

	b.WriteString("| Epic | Status | Title |\n")
	b.WriteString("|------|--------|-------|\n")

	for _, task := range tasks {
		emoji := StatusEmoji(task.Status)
		b.WriteString(fmt.Sprintf("| E%02d | %s | %s |\n",
			task.ID.EpicNum, emoji, task.Title))
	}

	return b.String()
}

// EnsureREADME creates a README.md if it doesn't exist
func (s *Syncer) EnsureREADME() error {
	readmePath := filepath.Join(s.plansDir, "README.md")
	if _, err := os.Stat(readmePath); err == nil {
		return nil // Already exists
	}

	content := `# EnergyERP Development Plans

This directory contains implementation plans organized by module.

## Status Legend

- 🔴 Not started
- 🟡 In progress
- 🟢 Complete

---

`
	return os.WriteFile(readmePath, []byte(content), 0644)
}

// AppendModuleSection adds a module section to README
func (s *Syncer) AppendModuleSection(module string, tasks []*domain.Task) error {
	readmePath := filepath.Join(s.plansDir, "README.md")
	content, err := os.ReadFile(readmePath)
	if err != nil {
		return err
	}

	// Check if module section exists
	sectionHeader := fmt.Sprintf("## %s Module", strings.Title(module))
	if strings.Contains(string(content), sectionHeader) {
		return nil // Already exists
	}

	// Append new section
	var b strings.Builder
	b.Write(content)
	b.WriteString(fmt.Sprintf("\n%s\n\n", sectionHeader))
	b.WriteString(GenerateStatusTable(tasks))
	b.WriteString("\n")

	return os.WriteFile(readmePath, []byte(b.String()), 0644)
}
```

**Step 5: Run tests to verify they pass**

Run: `go test ./internal/sync/... -v`
Expected: PASS

**Step 6: Commit**

```bash
git add internal/sync/
git commit -m "feat(sync): add status sync to README and epic files"
```

---

## Phase 9: Scheduled Batches

### Task 9.1: Create Batch Scheduler

**Files:**
- Create: `internal/batch/scheduler.go`
- Create: `internal/batch/scheduler_test.go`
- Create: `internal/batch/config.go`

**Step 1: Write batch scheduler test**

Create: `internal/batch/scheduler_test.go`

```go
package batch

import (
	"testing"
	"time"
)

func TestParseCron(t *testing.T) {
	tests := []struct {
		expr    string
		wantErr bool
	}{
		{"0 22 * * *", false},      // 10 PM daily
		{"0 12 * * 1-5", false},    // noon weekdays
		{"*/5 * * * *", false},     // every 5 minutes
		{"invalid", true},
	}

	for _, tt := range tests {
		_, err := ParseCron(tt.expr)
		if (err != nil) != tt.wantErr {
			t.Errorf("ParseCron(%q) error = %v, wantErr %v", tt.expr, err, tt.wantErr)
		}
	}
}

func TestBatchConfig_Validate(t *testing.T) {
	cfg := BatchConfig{
		Name:        "overnight",
		Cron:        "0 22 * * *",
		MaxTasks:    10,
		MaxDuration: 8 * time.Hour,
	}

	if err := cfg.Validate(); err != nil {
		t.Errorf("Valid config should not error: %v", err)
	}

	cfg.Name = ""
	if err := cfg.Validate(); err == nil {
		t.Error("Empty name should error")
	}
}

func TestBatchScheduler_NextRun(t *testing.T) {
	cfg := BatchConfig{
		Name:     "test",
		Cron:     "0 22 * * *", // 10 PM daily
		MaxTasks: 5,
	}

	sched, err := NewScheduler([]BatchConfig{cfg})
	if err != nil {
		t.Fatal(err)
	}

	next := sched.NextRun("test")
	if next.IsZero() {
		t.Error("NextRun should return a time")
	}

	// Should be in the future
	if !next.After(time.Now()) {
		t.Error("NextRun should be in the future")
	}
}

func TestBatchScheduler_ShouldRun(t *testing.T) {
	cfg := BatchConfig{
		Name:        "test",
		Cron:        "* * * * *", // Every minute
		MaxTasks:    5,
		MaxDuration: time.Hour,
	}

	sched, err := NewScheduler([]BatchConfig{cfg})
	if err != nil {
		t.Fatal(err)
	}

	// Mark as last run a minute ago
	sched.lastRun["test"] = time.Now().Add(-2 * time.Minute)

	if !sched.ShouldRun("test") {
		t.Error("Should run after cron interval passed")
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/batch/... -v`
Expected: FAIL (package doesn't exist)

**Step 3: Create batch config**

Create: `internal/batch/config.go`

```go
package batch

import (
	"fmt"
	"os"
	"time"

	"github.com/pelletier/go-toml/v2"
)

// BatchConfig represents a scheduled batch configuration
type BatchConfig struct {
	Name             string        `toml:"name"`
	Cron             string        `toml:"cron"`
	MaxTasks         int           `toml:"max_tasks"`
	MaxDuration      time.Duration `toml:"max_duration"`
	NotifyOnComplete bool          `toml:"notify_on_complete"`
}

// ScheduleConfig holds all batch configurations
type ScheduleConfig struct {
	Batches []BatchConfig `toml:"batch"`
}

// Validate checks if the config is valid
func (c *BatchConfig) Validate() error {
	if c.Name == "" {
		return fmt.Errorf("batch name is required")
	}
	if c.Cron == "" {
		return fmt.Errorf("cron expression is required")
	}
	if _, err := ParseCron(c.Cron); err != nil {
		return fmt.Errorf("invalid cron expression: %w", err)
	}
	if c.MaxTasks <= 0 {
		c.MaxTasks = 10 // Default
	}
	if c.MaxDuration <= 0 {
		c.MaxDuration = 4 * time.Hour // Default
	}
	return nil
}

// LoadScheduleConfig loads batch configuration from a TOML file
func LoadScheduleConfig(path string) (*ScheduleConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &ScheduleConfig{}, nil
		}
		return nil, err
	}

	var cfg ScheduleConfig
	if err := toml.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}

	// Validate all batches
	for i := range cfg.Batches {
		if err := cfg.Batches[i].Validate(); err != nil {
			return nil, fmt.Errorf("batch %d: %w", i, err)
		}
	}

	return &cfg, nil
}
```

**Step 4: Create batch scheduler**

Create: `internal/batch/scheduler.go`

```go
package batch

import (
	"fmt"
	"sync"
	"time"

	"github.com/robfig/cron/v3"
)

// Scheduler manages scheduled batch runs
type Scheduler struct {
	configs  map[string]BatchConfig
	parser   cron.Parser
	lastRun  map[string]time.Time
	running  map[string]bool
	mu       sync.RWMutex
	stopChan chan struct{}
}

// NewScheduler creates a new batch scheduler
func NewScheduler(configs []BatchConfig) (*Scheduler, error) {
	s := &Scheduler{
		configs:  make(map[string]BatchConfig),
		parser:   cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow),
		lastRun:  make(map[string]time.Time),
		running:  make(map[string]bool),
		stopChan: make(chan struct{}),
	}

	for _, cfg := range configs {
		if err := cfg.Validate(); err != nil {
			return nil, err
		}
		s.configs[cfg.Name] = cfg
	}

	return s, nil
}

// ParseCron parses a cron expression
func ParseCron(expr string) (cron.Schedule, error) {
	parser := cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)
	return parser.Parse(expr)
}

// NextRun returns the next scheduled run time for a batch
func (s *Scheduler) NextRun(name string) time.Time {
	s.mu.RLock()
	defer s.mu.RUnlock()

	cfg, ok := s.configs[name]
	if !ok {
		return time.Time{}
	}

	sched, err := s.parser.Parse(cfg.Cron)
	if err != nil {
		return time.Time{}
	}

	return sched.Next(time.Now())
}

// ShouldRun returns true if a batch should run now
func (s *Scheduler) ShouldRun(name string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()

	cfg, ok := s.configs[name]
	if !ok {
		return false
	}

	if s.running[name] {
		return false
	}

	sched, err := s.parser.Parse(cfg.Cron)
	if err != nil {
		return false
	}

	lastRun := s.lastRun[name]
	if lastRun.IsZero() {
		lastRun = time.Now().Add(-24 * time.Hour)
	}

	nextRun := sched.Next(lastRun)
	return time.Now().After(nextRun)
}

// MarkRunning marks a batch as currently running
func (s *Scheduler) MarkRunning(name string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.running[name] = true
}

// MarkComplete marks a batch as complete
func (s *Scheduler) MarkComplete(name string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.running[name] = false
	s.lastRun[name] = time.Now()
}

// GetConfig returns the config for a batch
func (s *Scheduler) GetConfig(name string) (BatchConfig, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	cfg, ok := s.configs[name]
	return cfg, ok
}

// ListBatches returns all batch names
func (s *Scheduler) ListBatches() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	names := make([]string, 0, len(s.configs))
	for name := range s.configs {
		names = append(names, name)
	}
	return names
}

// Start begins the scheduler loop
func (s *Scheduler) Start(runFunc func(BatchConfig) error) {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-s.stopChan:
			return
		case <-ticker.C:
			for name := range s.configs {
				if s.ShouldRun(name) {
					cfg, _ := s.GetConfig(name)
					s.MarkRunning(name)
					go func(c BatchConfig) {
						if err := runFunc(c); err != nil {
							fmt.Printf("Batch %s failed: %v\n", c.Name, err)
						}
						s.MarkComplete(c.Name)
					}(cfg)
				}
			}
		}
	}
}

// Stop stops the scheduler
func (s *Scheduler) Stop() {
	close(s.stopChan)
}
```

**Step 5: Add cron dependency**

Run:
```bash
go get github.com/robfig/cron/v3
```

**Step 6: Run tests to verify they pass**

Run: `go test ./internal/batch/... -v`
Expected: PASS

**Step 7: Commit**

```bash
git add internal/batch/ go.mod go.sum
git commit -m "feat(batch): add scheduled batch support with cron expressions"
```

---

## Phase 10: Notifications

### Task 10.1: Create Notification System

**Files:**
- Create: `internal/notify/notify.go`
- Create: `internal/notify/notify_test.go`
- Create: `internal/notify/slack.go`
- Create: `internal/notify/desktop.go`

**Step 1: Write notification test**

Create: `internal/notify/notify_test.go`

```go
package notify

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSlackMessage_Build(t *testing.T) {
	msg := SlackMessage{
		Text: "Task completed",
		Attachments: []SlackAttachment{
			{
				Color: "good",
				Title: "technical/E05",
				Text:  "Validators implemented",
			},
		},
	}

	payload, err := msg.ToJSON()
	if err != nil {
		t.Fatal(err)
	}

	if len(payload) == 0 {
		t.Error("Payload should not be empty")
	}
}

func TestSlackNotifier_Send(t *testing.T) {
	// Mock Slack server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			t.Errorf("Expected POST, got %s", r.Method)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	notifier := NewSlackNotifier(server.URL)
	err := notifier.Send(Notification{
		Title:   "Test",
		Message: "Test message",
		Type:    NotifyInfo,
	})

	if err != nil {
		t.Errorf("Send failed: %v", err)
	}
}

func TestNotificationTypeColors(t *testing.T) {
	tests := []struct {
		typ  NotificationType
		want string
	}{
		{NotifySuccess, "good"},
		{NotifyWarning, "warning"},
		{NotifyError, "danger"},
		{NotifyInfo, "#439FE0"},
	}

	for _, tt := range tests {
		got := SlackColor(tt.typ)
		if got != tt.want {
			t.Errorf("SlackColor(%v) = %s, want %s", tt.typ, got, tt.want)
		}
	}
}

func TestMultiNotifier(t *testing.T) {
	var called []string

	mock1 := &mockNotifier{name: "mock1", calls: &called}
	mock2 := &mockNotifier{name: "mock2", calls: &called}

	multi := NewMultiNotifier(mock1, mock2)
	multi.Send(Notification{Title: "Test"})

	if len(called) != 2 {
		t.Errorf("Expected 2 calls, got %d", len(called))
	}
}

type mockNotifier struct {
	name  string
	calls *[]string
}

func (m *mockNotifier) Send(n Notification) error {
	*m.calls = append(*m.calls, m.name)
	return nil
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/notify/... -v`
Expected: FAIL (package doesn't exist)

**Step 3: Create notify types**

Create: `internal/notify/notify.go`

```go
package notify

// NotificationType represents the type of notification
type NotificationType int

const (
	NotifyInfo NotificationType = iota
	NotifySuccess
	NotifyWarning
	NotifyError
)

// Notification represents a notification to be sent
type Notification struct {
	Title   string
	Message string
	Type    NotificationType
	TaskID  string // Optional task reference
	PRURL   string // Optional PR URL
}

// Notifier is the interface for sending notifications
type Notifier interface {
	Send(n Notification) error
}

// MultiNotifier sends to multiple notifiers
type MultiNotifier struct {
	notifiers []Notifier
}

// NewMultiNotifier creates a notifier that sends to all provided notifiers
func NewMultiNotifier(notifiers ...Notifier) *MultiNotifier {
	return &MultiNotifier{notifiers: notifiers}
}

// Send sends the notification to all notifiers
func (m *MultiNotifier) Send(n Notification) error {
	var lastErr error
	for _, notifier := range m.notifiers {
		if err := notifier.Send(n); err != nil {
			lastErr = err
		}
	}
	return lastErr
}

// NoopNotifier does nothing (for testing or disabled notifications)
type NoopNotifier struct{}

func (NoopNotifier) Send(n Notification) error { return nil }
```

**Step 4: Create Slack notifier**

Create: `internal/notify/slack.go`

```go
package notify

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// SlackNotifier sends notifications to Slack
type SlackNotifier struct {
	webhookURL string
	client     *http.Client
}

// SlackMessage represents a Slack message payload
type SlackMessage struct {
	Text        string            `json:"text"`
	Attachments []SlackAttachment `json:"attachments,omitempty"`
}

// SlackAttachment represents a Slack message attachment
type SlackAttachment struct {
	Color  string `json:"color"`
	Title  string `json:"title"`
	Text   string `json:"text"`
	Footer string `json:"footer,omitempty"`
}

// NewSlackNotifier creates a new Slack notifier
func NewSlackNotifier(webhookURL string) *SlackNotifier {
	return &SlackNotifier{
		webhookURL: webhookURL,
		client: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

// ToJSON converts the message to JSON
func (m *SlackMessage) ToJSON() ([]byte, error) {
	return json.Marshal(m)
}

// SlackColor returns the Slack color for a notification type
func SlackColor(t NotificationType) string {
	switch t {
	case NotifySuccess:
		return "good"
	case NotifyWarning:
		return "warning"
	case NotifyError:
		return "danger"
	default:
		return "#439FE0"
	}
}

// Send sends a notification to Slack
func (s *SlackNotifier) Send(n Notification) error {
	if s.webhookURL == "" {
		return nil // Disabled
	}

	msg := SlackMessage{
		Text: n.Title,
		Attachments: []SlackAttachment{
			{
				Color:  SlackColor(n.Type),
				Text:   n.Message,
				Footer: "ERP Orchestrator",
			},
		},
	}

	if n.TaskID != "" {
		msg.Attachments[0].Title = n.TaskID
	}

	payload, err := msg.ToJSON()
	if err != nil {
		return err
	}

	resp, err := s.client.Post(s.webhookURL, "application/json", bytes.NewReader(payload))
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("slack returned %d", resp.StatusCode)
	}

	return nil
}
```

**Step 5: Create desktop notifier**

Create: `internal/notify/desktop.go`

```go
package notify

import (
	"os/exec"
	"runtime"
)

// DesktopNotifier sends desktop notifications
type DesktopNotifier struct {
	enabled bool
}

// NewDesktopNotifier creates a new desktop notifier
func NewDesktopNotifier(enabled bool) *DesktopNotifier {
	return &DesktopNotifier{enabled: enabled}
}

// Send sends a desktop notification
func (d *DesktopNotifier) Send(n Notification) error {
	if !d.enabled {
		return nil
	}

	switch runtime.GOOS {
	case "darwin":
		return d.sendMacOS(n)
	case "linux":
		return d.sendLinux(n)
	default:
		return nil // Unsupported
	}
}

func (d *DesktopNotifier) sendMacOS(n Notification) error {
	script := `display notification "` + n.Message + `" with title "` + n.Title + `"`
	cmd := exec.Command("osascript", "-e", script)
	return cmd.Run()
}

func (d *DesktopNotifier) sendLinux(n Notification) error {
	// Try notify-send (most common)
	cmd := exec.Command("notify-send", n.Title, n.Message)
	return cmd.Run()
}

// IconForType returns an icon name for the notification type
func IconForType(t NotificationType) string {
	switch t {
	case NotifySuccess:
		return "dialog-positive"
	case NotifyWarning:
		return "dialog-warning"
	case NotifyError:
		return "dialog-error"
	default:
		return "dialog-information"
	}
}
```

**Step 6: Run tests to verify they pass**

Run: `go test ./internal/notify/... -v`
Expected: PASS

**Step 7: Commit**

```bash
git add internal/notify/
git commit -m "feat(notify): add desktop and Slack notifications"
```

---

## Phase 11: Web UI API

### Task 11.1: Create HTTP API

**Files:**
- Create: `web/api/server.go`
- Create: `web/api/handlers.go`
- Create: `web/api/sse.go`
- Create: `web/api/server_test.go`

**Step 1: Write API test**

Create: `web/api/server_test.go`

```go
package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/anthropics/erp-orchestrator/internal/domain"
)

func TestListTasksHandler(t *testing.T) {
	store := &mockStore{
		tasks: []*domain.Task{
			{ID: domain.TaskID{Module: "tech", EpicNum: 0}, Title: "Setup", Status: domain.StatusComplete},
			{ID: domain.TaskID{Module: "tech", EpicNum: 1}, Title: "Core", Status: domain.StatusNotStarted},
		},
	}

	server := NewServer(store, nil, ":8080")
	handler := server.listTasksHandler()

	req := httptest.NewRequest("GET", "/api/tasks", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Status = %d, want 200", w.Code)
	}

	var tasks []TaskResponse
	json.NewDecoder(w.Body).Decode(&tasks)

	if len(tasks) != 2 {
		t.Errorf("Task count = %d, want 2", len(tasks))
	}
}

func TestStatusHandler(t *testing.T) {
	store := &mockStore{
		tasks: []*domain.Task{
			{ID: domain.TaskID{Module: "tech", EpicNum: 0}, Status: domain.StatusComplete},
			{ID: domain.TaskID{Module: "tech", EpicNum: 1}, Status: domain.StatusInProgress},
			{ID: domain.TaskID{Module: "tech", EpicNum: 2}, Status: domain.StatusNotStarted},
		},
	}

	server := NewServer(store, nil, ":8080")
	handler := server.statusHandler()

	req := httptest.NewRequest("GET", "/api/status", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	var status StatusResponse
	json.NewDecoder(w.Body).Decode(&status)

	if status.Complete != 1 {
		t.Errorf("Complete = %d, want 1", status.Complete)
	}
	if status.InProgress != 1 {
		t.Errorf("InProgress = %d, want 1", status.InProgress)
	}
}

type mockStore struct {
	tasks []*domain.Task
}

func (m *mockStore) ListTasks(opts interface{}) ([]*domain.Task, error) {
	return m.tasks, nil
}

func (m *mockStore) GetTask(id string) (*domain.Task, error) {
	for _, t := range m.tasks {
		if t.ID.String() == id {
			return t, nil
		}
	}
	return nil, nil
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./web/api/... -v`
Expected: FAIL (package doesn't exist)

**Step 3: Create API server**

Create: `web/api/server.go`

```go
package api

import (
	"encoding/json"
	"net/http"

	"github.com/anthropics/erp-orchestrator/internal/domain"
	"github.com/anthropics/erp-orchestrator/internal/executor"
)

// Store interface for database operations
type Store interface {
	ListTasks(opts interface{}) ([]*domain.Task, error)
	GetTask(id string) (*domain.Task, error)
}

// Server is the HTTP API server
type Server struct {
	store   Store
	agents  *executor.AgentManager
	addr    string
	mux     *http.ServeMux
	sseHub  *SSEHub
}

// NewServer creates a new API server
func NewServer(store Store, agents *executor.AgentManager, addr string) *Server {
	s := &Server{
		store:  store,
		agents: agents,
		addr:   addr,
		mux:    http.NewServeMux(),
		sseHub: NewSSEHub(),
	}
	s.setupRoutes()
	return s
}

func (s *Server) setupRoutes() {
	// API routes
	s.mux.HandleFunc("/api/status", s.statusHandler())
	s.mux.HandleFunc("/api/tasks", s.listTasksHandler())
	s.mux.HandleFunc("/api/tasks/", s.getTaskHandler())
	s.mux.HandleFunc("/api/agents", s.listAgentsHandler())
	s.mux.HandleFunc("/api/events", s.sseHandler())

	// Static files (Svelte build output)
	s.mux.Handle("/", http.FileServer(http.Dir("web/ui/build")))
}

// Start starts the HTTP server
func (s *Server) Start() error {
	go s.sseHub.Run()
	return http.ListenAndServe(s.addr, s.mux)
}

// Broadcast sends an event to all SSE clients
func (s *Server) Broadcast(event SSEEvent) {
	s.sseHub.Broadcast(event)
}

func writeJSON(w http.ResponseWriter, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(data)
}

func writeError(w http.ResponseWriter, code int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{"error": message})
}
```

**Step 4: Create API handlers**

Create: `web/api/handlers.go`

```go
package api

import (
	"net/http"
	"strings"

	"github.com/anthropics/erp-orchestrator/internal/domain"
)

// TaskResponse is the API response for a task
type TaskResponse struct {
	ID          string   `json:"id"`
	Module      string   `json:"module"`
	EpicNum     int      `json:"epic_num"`
	Title       string   `json:"title"`
	Description string   `json:"description,omitempty"`
	Status      string   `json:"status"`
	Priority    string   `json:"priority,omitempty"`
	DependsOn   []string `json:"depends_on,omitempty"`
	NeedsReview bool     `json:"needs_review"`
}

// StatusResponse is the API response for overall status
type StatusResponse struct {
	Total      int `json:"total"`
	NotStarted int `json:"not_started"`
	InProgress int `json:"in_progress"`
	Complete   int `json:"complete"`
	Agents     int `json:"agents_running"`
}

// AgentResponse is the API response for an agent
type AgentResponse struct {
	TaskID   string `json:"task_id"`
	Status   string `json:"status"`
	Duration string `json:"duration"`
}

func taskToResponse(t *domain.Task) TaskResponse {
	deps := make([]string, len(t.DependsOn))
	for i, d := range t.DependsOn {
		deps[i] = d.String()
	}

	return TaskResponse{
		ID:          t.ID.String(),
		Module:      t.ID.Module,
		EpicNum:     t.ID.EpicNum,
		Title:       t.Title,
		Description: t.Description,
		Status:      string(t.Status),
		Priority:    string(t.Priority),
		DependsOn:   deps,
		NeedsReview: t.NeedsReview,
	}
}

func (s *Server) statusHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}

		tasks, err := s.store.ListTasks(nil)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}

		var status StatusResponse
		status.Total = len(tasks)

		for _, t := range tasks {
			switch t.Status {
			case domain.StatusNotStarted:
				status.NotStarted++
			case domain.StatusInProgress:
				status.InProgress++
			case domain.StatusComplete:
				status.Complete++
			}
		}

		if s.agents != nil {
			status.Agents = s.agents.RunningCount()
		}

		writeJSON(w, status)
	}
}

func (s *Server) listTasksHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}

		tasks, err := s.store.ListTasks(nil)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}

		responses := make([]TaskResponse, len(tasks))
		for i, t := range tasks {
			responses[i] = taskToResponse(t)
		}

		writeJSON(w, responses)
	}
}

func (s *Server) getTaskHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}

		// Extract task ID from path: /api/tasks/{module}/E{num}
		path := strings.TrimPrefix(r.URL.Path, "/api/tasks/")
		if path == "" {
			writeError(w, http.StatusBadRequest, "task ID required")
			return
		}

		task, err := s.store.GetTask(path)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if task == nil {
			writeError(w, http.StatusNotFound, "task not found")
			return
		}

		writeJSON(w, taskToResponse(task))
	}
}

func (s *Server) listAgentsHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}

		// Would return agent status from AgentManager
		writeJSON(w, []AgentResponse{})
	}
}
```

**Step 5: Create SSE handler**

Create: `web/api/sse.go`

```go
package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
)

// SSEEvent represents a server-sent event
type SSEEvent struct {
	Type string      `json:"type"`
	Data interface{} `json:"data"`
}

// SSEHub manages SSE connections
type SSEHub struct {
	clients    map[chan SSEEvent]bool
	broadcast  chan SSEEvent
	register   chan chan SSEEvent
	unregister chan chan SSEEvent
	mu         sync.RWMutex
}

// NewSSEHub creates a new SSE hub
func NewSSEHub() *SSEHub {
	return &SSEHub{
		clients:    make(map[chan SSEEvent]bool),
		broadcast:  make(chan SSEEvent),
		register:   make(chan chan SSEEvent),
		unregister: make(chan chan SSEEvent),
	}
}

// Run starts the SSE hub
func (h *SSEHub) Run() {
	for {
		select {
		case client := <-h.register:
			h.mu.Lock()
			h.clients[client] = true
			h.mu.Unlock()

		case client := <-h.unregister:
			h.mu.Lock()
			if _, ok := h.clients[client]; ok {
				delete(h.clients, client)
				close(client)
			}
			h.mu.Unlock()

		case event := <-h.broadcast:
			h.mu.RLock()
			for client := range h.clients {
				select {
				case client <- event:
				default:
					close(client)
					delete(h.clients, client)
				}
			}
			h.mu.RUnlock()
		}
	}
}

// Broadcast sends an event to all clients
func (h *SSEHub) Broadcast(event SSEEvent) {
	h.broadcast <- event
}

func (s *Server) sseHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Set SSE headers
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.Header().Set("Access-Control-Allow-Origin", "*")

		// Create client channel
		client := make(chan SSEEvent)
		s.sseHub.register <- client

		// Cleanup on disconnect
		notify := r.Context().Done()
		go func() {
			<-notify
			s.sseHub.unregister <- client
		}()

		// Stream events
		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "Streaming not supported", http.StatusInternalServerError)
			return
		}

		for event := range client {
			data, _ := json.Marshal(event)
			fmt.Fprintf(w, "event: %s\n", event.Type)
			fmt.Fprintf(w, "data: %s\n\n", data)
			flusher.Flush()
		}
	}
}
```

**Step 6: Run tests to verify they pass**

Run: `go test ./web/api/... -v`
Expected: PASS

**Step 7: Commit**

```bash
git add web/api/
git commit -m "feat(api): add HTTP API with SSE for real-time updates"
```

---

## Phase 12: Svelte Web UI

### Task 12.1: Create Svelte Project

**Files:**
- Create: `web/ui/package.json`
- Create: `web/ui/svelte.config.js`
- Create: `web/ui/src/App.svelte`
- Create: `web/ui/src/lib/api.js`

**Step 1: Initialize Svelte project**

Run:
```bash
cd web/ui
npm create svelte@latest . -- --template skeleton --typescript
npm install
```

**Step 2: Create API client**

Create: `web/ui/src/lib/api.js`

```javascript
const API_BASE = '/api';

export async function fetchStatus() {
    const res = await fetch(`${API_BASE}/status`);
    return res.json();
}

export async function fetchTasks(params = {}) {
    const query = new URLSearchParams(params).toString();
    const url = query ? `${API_BASE}/tasks?${query}` : `${API_BASE}/tasks`;
    const res = await fetch(url);
    return res.json();
}

export async function fetchTask(id) {
    const res = await fetch(`${API_BASE}/tasks/${id}`);
    return res.json();
}

export function createEventSource() {
    return new EventSource(`${API_BASE}/events`);
}
```

**Step 3: Create main App component**

Create: `web/ui/src/App.svelte`

```svelte
<script>
    import { onMount, onDestroy } from 'svelte';
    import { fetchStatus, fetchTasks, createEventSource } from './lib/api.js';

    let status = { total: 0, not_started: 0, in_progress: 0, complete: 0, agents_running: 0 };
    let tasks = [];
    let eventSource = null;

    onMount(async () => {
        status = await fetchStatus();
        tasks = await fetchTasks();

        // Connect to SSE
        eventSource = createEventSource();
        eventSource.onmessage = (event) => {
            const data = JSON.parse(event.data);
            handleEvent(data);
        };
    });

    onDestroy(() => {
        if (eventSource) {
            eventSource.close();
        }
    });

    function handleEvent(event) {
        if (event.type === 'status_update') {
            status = event.data;
        } else if (event.type === 'task_update') {
            const idx = tasks.findIndex(t => t.id === event.data.id);
            if (idx >= 0) {
                tasks[idx] = event.data;
                tasks = tasks;
            }
        }
    }

    function statusEmoji(s) {
        switch (s) {
            case 'not_started': return '🔴';
            case 'in_progress': return '🟡';
            case 'complete': return '🟢';
            default: return '⚪';
        }
    }
</script>

<main>
    <header>
        <h1>ERP Orchestrator</h1>
        <div class="stats">
            <span>Total: {status.total}</span>
            <span>🔴 {status.not_started}</span>
            <span>🟡 {status.in_progress}</span>
            <span>🟢 {status.complete}</span>
            <span>Agents: {status.agents_running}</span>
        </div>
    </header>

    <section class="tasks">
        <h2>Tasks</h2>
        <table>
            <thead>
                <tr>
                    <th>ID</th>
                    <th>Title</th>
                    <th>Status</th>
                    <th>Priority</th>
                </tr>
            </thead>
            <tbody>
                {#each tasks as task}
                    <tr>
                        <td>{task.id}</td>
                        <td>{task.title}</td>
                        <td>{statusEmoji(task.status)}</td>
                        <td>{task.priority || '-'}</td>
                    </tr>
                {/each}
            </tbody>
        </table>
    </section>
</main>

<style>
    main {
        max-width: 1200px;
        margin: 0 auto;
        padding: 1rem;
        font-family: system-ui, sans-serif;
    }

    header {
        display: flex;
        justify-content: space-between;
        align-items: center;
        margin-bottom: 2rem;
        padding-bottom: 1rem;
        border-bottom: 1px solid #ddd;
    }

    .stats {
        display: flex;
        gap: 1rem;
    }

    .stats span {
        padding: 0.5rem 1rem;
        background: #f5f5f5;
        border-radius: 4px;
    }

    table {
        width: 100%;
        border-collapse: collapse;
    }

    th, td {
        text-align: left;
        padding: 0.75rem;
        border-bottom: 1px solid #eee;
    }

    th {
        background: #f9f9f9;
        font-weight: 600;
    }

    tr:hover {
        background: #f5f5f5;
    }
</style>
```

**Step 4: Build Svelte app**

Run:
```bash
cd web/ui
npm run build
```

**Step 5: Add serve command to CLI**

Add to `cmd/erp-orch/commands.go`:

```go
	// serve command
	serveCmd := &cobra.Command{
		Use:   "serve",
		Short: "Start web UI server",
		RunE:  runServe,
	}
	serveCmd.Flags().IntVar(&servePort, "port", 8080, "port to listen on")
	rootCmd.AddCommand(serveCmd)
```

Add handler:

```go
var servePort int

func runServe(cmd *cobra.Command, args []string) error {
	cfg, err := loadConfig()
	if err != nil {
		return err
	}

	store, err := taskstore.New(cfg.General.DatabasePath)
	if err != nil {
		return err
	}

	addr := fmt.Sprintf("%s:%d", cfg.Web.Host, servePort)
	server := api.NewServer(store, nil, addr)

	fmt.Printf("Starting web UI at http://%s\n", addr)
	return server.Start()
}
```

**Step 6: Commit**

```bash
git add web/ui/ cmd/erp-orch/
git commit -m "feat(web): add Svelte web UI with real-time updates"
```

---

## Verification Checklist

After completing all phases:

- [ ] `go test ./...` passes
- [ ] `./erp-orch sync` syncs tasks and updates README
- [ ] `./erp-orch serve` starts web UI
- [ ] Web UI shows task list with real-time updates
- [ ] Desktop notifications work on macOS/Linux
- [ ] Slack notifications work with webhook URL
- [ ] Scheduled batches run at configured times

## Integration Points

| Component | Integrates With |
|-----------|-----------------|
| Sync | Parser, TaskStore |
| Batch | Scheduler, Executor, Notify |
| Notify | Observer, PRBot |
| Web API | TaskStore, AgentManager |
| Web UI | Web API (SSE) |
