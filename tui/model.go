package tui

import (
	"fmt"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/hochfrequenz/claude-plan-orchestrator/internal/domain"
	"github.com/hochfrequenz/claude-plan-orchestrator/internal/executor"
	"github.com/hochfrequenz/claude-plan-orchestrator/internal/observer"
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
	selectedAgent  int
	showAgentDetail bool
	testRunning    bool
	testOutput     string

	// Config for test execution
	projectRoot string
	worktreeDir string

	// Executor managers
	agentManager    *executor.AgentManager
	worktreeManager *executor.WorktreeManager

	// Plan file watcher
	planWatcher     *observer.PlanWatcher
	planChangeChan  chan PlanSyncMsg

	// Config state
	configChanged bool

	// Batch execution state
	batchRunning bool
	batchPaused  bool
	statusMsg    string

	// Refresh
	lastRefresh time.Time
}

// AgentView represents an agent in the TUI
type AgentView struct {
	TaskID       string
	Title        string
	Duration     time.Duration
	Status       executor.AgentStatus
	Progress     string
	WorktreePath string
	Error        string
	Output       []string // Last N lines of output
}

// FlaggedPR represents a PR needing attention
type FlaggedPR struct {
	TaskID   string
	PRNumber int
	Reason   string
}

// ModelConfig holds initial data for the TUI model
type ModelConfig struct {
	MaxActive       int
	AllTasks        []*domain.Task
	Queued          []*domain.Task
	Agents          []*AgentView
	Flagged         []*FlaggedPR
	ProjectRoot     string
	WorktreeDir     string
	AgentManager    *executor.AgentManager
	WorktreeManager *executor.WorktreeManager
	RecoveredAgents []*AgentView // Agents recovered from previous session
	PlanWatcher     *observer.PlanWatcher
	PlanChangeChan  chan PlanSyncMsg
}

// NewModel creates a new TUI model
func NewModel(cfg ModelConfig) Model {
	// Merge recovered agents with any provided agents
	agents := cfg.Agents
	if len(cfg.RecoveredAgents) > 0 {
		agents = append(agents, cfg.RecoveredAgents...)
	}

	activeCount := 0
	for _, a := range agents {
		if a.Status == executor.AgentRunning {
			activeCount++
		}
	}

	// Compute module summaries
	modules := computeModuleSummaries(cfg.AllTasks)

	// Use provided managers or create defaults
	agentMgr := cfg.AgentManager
	if agentMgr == nil {
		agentMgr = executor.NewAgentManager(cfg.MaxActive)
	}

	worktreeMgr := cfg.WorktreeManager
	if worktreeMgr == nil && cfg.ProjectRoot != "" && cfg.WorktreeDir != "" {
		worktreeMgr = executor.NewWorktreeManager(cfg.ProjectRoot, cfg.WorktreeDir)
	}

	// Set status message if we recovered agents
	statusMsg := ""
	if len(cfg.RecoveredAgents) > 0 {
		statusMsg = fmt.Sprintf("Recovered %d agent(s) from previous session", len(cfg.RecoveredAgents))
	}

	return Model{
		maxActive:       cfg.MaxActive,
		allTasks:        cfg.AllTasks,
		queued:          cfg.Queued,
		agents:          agents,
		flagged:         cfg.Flagged,
		modules:         modules,
		activeCount:     activeCount,
		activeTab:       0,
		projectRoot:     cfg.ProjectRoot,
		worktreeDir:     cfg.WorktreeDir,
		agentManager:    agentMgr,
		worktreeManager: worktreeMgr,
		planWatcher:     cfg.PlanWatcher,
		planChangeChan:  cfg.PlanChangeChan,
		statusMsg:       statusMsg,
	}
}

// GetPlanChangeChan returns the channel for receiving plan change messages
func (m Model) GetPlanChangeChan() chan PlanSyncMsg {
	return m.planChangeChan
}

// GetPlanWatcher returns the plan watcher
func (m Model) GetPlanWatcher() *observer.PlanWatcher {
	return m.planWatcher
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
	cmds := []tea.Cmd{tickCmd()}

	// Start listening for plan changes if watcher is configured
	if m.planChangeChan != nil {
		cmds = append(cmds, waitForPlanChange(m.planChangeChan))
	}

	return tea.Batch(cmds...)
}

// waitForPlanChange returns a command that waits for plan file changes
func waitForPlanChange(ch chan PlanSyncMsg) tea.Cmd {
	return func() tea.Msg {
		return <-ch
	}
}

// TickMsg triggers a refresh
type TickMsg time.Time

func tickCmd() tea.Cmd {
	return tea.Tick(time.Second, func(t time.Time) tea.Msg {
		return TickMsg(t)
	})
}
