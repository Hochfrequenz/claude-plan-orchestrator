package tui

import (
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

// Model is the TUI application model
type Model struct {
	// Data
	agents   []*AgentView
	queued   []*domain.Task
	allTasks []*domain.Task
	flagged  []*FlaggedPR

	// Stats
	activeCount    int
	maxActive      int
	completedToday int

	// UI state
	width       int
	height      int
	activeTab   int
	selectedRow int
	viewMode    ViewMode
	taskScroll  int

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
	MaxActive int
	AllTasks  []*domain.Task
	Queued    []*domain.Task
	Agents    []*AgentView
	Flagged   []*FlaggedPR
}

// NewModel creates a new TUI model
func NewModel(cfg ModelConfig) Model {
	activeCount := 0
	for _, a := range cfg.Agents {
		if a.Status == "running" {
			activeCount++
		}
	}

	return Model{
		maxActive:   cfg.MaxActive,
		allTasks:    cfg.AllTasks,
		queued:      cfg.Queued,
		agents:      cfg.Agents,
		flagged:     cfg.Flagged,
		activeCount: activeCount,
		activeTab:   0,
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
