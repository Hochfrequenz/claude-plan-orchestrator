package tui

import (
	"fmt"
	"time"

	"github.com/hochfrequenz/claude-plan-orchestrator/internal/domain"
	"github.com/hochfrequenz/claude-plan-orchestrator/internal/executor"
	tea "github.com/charmbracelet/bubbletea"
)

// ViewMode determines how tasks are displayed
type ViewMode int

const (
	ViewByPriority ViewMode = iota
	ViewByModule
)

// ModuleSummary holds aggregated data for a module
type ModuleSummary struct {
	Name           string
	TotalEpics     int
	CompletedEpics int
	InProgressEpics int
	TotalTests     int
	PassedTests    int
	FailedTests    int
	Coverage       string
}

// Model is the TUI application model
type Model struct {
	// Data
	agents   []*AgentView
	queued   []*domain.Task
	allTasks []*domain.Task
	flagged  []*FlaggedPR
	modules  []*ModuleSummary

	// Stats
	activeCount    int
	maxActive      int
	completedToday int

	// UI state
	width          int
	height         int
	activeTab      int
	selectedRow    int
	viewMode       ViewMode
	taskScroll     int
	selectedModule int
	testRunning    bool
	testOutput     string

	// Config for test execution
	projectRoot string

	// Config state
	configChanged bool

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

// ModelConfig holds initial data for the TUI model
type ModelConfig struct {
	MaxActive   int
	AllTasks    []*domain.Task
	Queued      []*domain.Task
	Agents      []*AgentView
	Flagged     []*FlaggedPR
	ProjectRoot string
}

// NewModel creates a new TUI model
func NewModel(cfg ModelConfig) Model {
	activeCount := 0
	for _, a := range cfg.Agents {
		if a.Status == "running" {
			activeCount++
		}
	}

	// Compute module summaries
	modules := computeModuleSummaries(cfg.AllTasks)

	return Model{
		maxActive:   cfg.MaxActive,
		allTasks:    cfg.AllTasks,
		queued:      cfg.Queued,
		agents:      cfg.Agents,
		flagged:     cfg.Flagged,
		modules:     modules,
		activeCount: activeCount,
		activeTab:   0,
		projectRoot: cfg.ProjectRoot,
	}
}

// computeModuleSummaries aggregates task data by module
func computeModuleSummaries(tasks []*domain.Task) []*ModuleSummary {
	moduleMap := make(map[string]*ModuleSummary)

	for _, task := range tasks {
		mod := task.ID.Module
		if _, exists := moduleMap[mod]; !exists {
			moduleMap[mod] = &ModuleSummary{Name: mod}
		}
		ms := moduleMap[mod]
		ms.TotalEpics++

		switch task.Status {
		case domain.StatusComplete:
			ms.CompletedEpics++
		case domain.StatusInProgress:
			ms.InProgressEpics++
		}

		// Aggregate test summary if available
		if task.TestSummary != nil {
			ms.TotalTests += task.TestSummary.Tests
			ms.PassedTests += task.TestSummary.Passed
			ms.FailedTests += task.TestSummary.Failed
		}
	}

	// Convert to slice and sort
	var result []*ModuleSummary
	for _, ms := range moduleMap {
		// Calculate coverage (simplified - just take average if we had multiple)
		if ms.TotalTests > 0 && ms.PassedTests > 0 {
			pct := float64(ms.PassedTests) / float64(ms.TotalTests) * 100
			ms.Coverage = fmt.Sprintf("%.0f%%", pct)
		}
		result = append(result, ms)
	}

	// Sort by name
	for i := 0; i < len(result)-1; i++ {
		for j := i + 1; j < len(result); j++ {
			if result[i].Name > result[j].Name {
				result[i], result[j] = result[j], result[i]
			}
		}
	}

	return result
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
