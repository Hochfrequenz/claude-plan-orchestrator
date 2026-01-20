package tui

import (
	"fmt"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/hochfrequenz/claude-plan-orchestrator/internal/domain"
	"github.com/hochfrequenz/claude-plan-orchestrator/internal/executor"
	"github.com/hochfrequenz/claude-plan-orchestrator/internal/maintenance"
	"github.com/hochfrequenz/claude-plan-orchestrator/internal/observer"
	isync "github.com/hochfrequenz/claude-plan-orchestrator/internal/sync"
	"github.com/hochfrequenz/claude-plan-orchestrator/internal/taskstore"
)

// testAgentOutput stores streaming output from the test agent
// Protected by testAgentMutex for concurrent access
var (
	testAgentMutex  sync.Mutex
	testAgentOutput []string
	testAgentTaskID string
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

// SyncConflictModal holds state for the sync conflict resolution modal
type SyncConflictModal struct {
	Visible     bool
	Conflicts   []isync.SyncConflict
	Resolutions map[string]string // taskID -> "db" | "markdown" | ""
	Selected    int               // Currently highlighted conflict
}

// MaintenanceModal holds state for the maintenance task selection modal
type MaintenanceModal struct {
	Visible      bool
	Templates    []maintenance.Template
	Selected     int    // Currently highlighted template
	Phase        int    // 0=select template, 1=select scope
	SelectedScope string // "module", "package", "all"
	CustomPrompt string // For custom template input
	TargetModule string // Selected module name for scope
}

// GroupPriorityItem represents a group in the priorities view
type GroupPriorityItem struct {
	Name      string
	Priority  int // -1 if unassigned
	Total     int
	Completed int
}

// Model is the TUI application model
type Model struct {
	// Data
	agents         []*AgentView
	queued         []*domain.Task
	allTasks       []*domain.Task
	flagged        []*FlaggedPR
	workers        []*WorkerView
	modules        []*ModuleSummary
	completedTasks map[string]bool // Track completed task IDs for dependency checking

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
	selectedAgent      int
	showAgentDetail    bool
	showAgentPrompt    bool // Toggle to show prompt instead of output
	agentOutputScroll  int  // Scroll position for agent output
	showAgentHistory   bool         // Toggle to show completed/failed agent history
	agentHistory       []*AgentView // Historical agent runs from database
	selectedHistory    int          // Selected index in history list
	showHistoryDetail  bool         // Show detail view for selected history item
	testRunning        bool
	testOutput         string

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
	autoMode     bool // Auto mode: continuously start new tasks when slots available
	statusMsg    string

	// Build pool
	buildPoolURL    string
	buildPoolStatus string // "disabled", "unreachable", "connected"

	// Refresh
	lastRefresh time.Time

	// Mouse mode toggle
	mouseEnabled bool

	// Sync modal state
	syncModal    SyncConflictModal
	syncFlash    string
	syncFlashExp time.Time
	store        *taskstore.Store
	syncer       *isync.Syncer

	// Maintenance modal state
	maintenanceModal MaintenanceModal

	// Group priorities view state
	showGroupPriorities bool                // Toggle with 'g' key
	groupPriorityItems  []GroupPriorityItem // Groups with their priorities
	selectedPriorityRow int                 // Currently selected row

	// Update state
	currentVersion   string // Current running version (e.g., "v0.3.19")
	updateAvailable  string // Latest version if update available, empty otherwise
	updateStatus     string // Status message during update ("Downloading...", "Updated!", error)
	updateInProgress bool   // True while downloading/installing
}

// AgentView represents an agent in the TUI
type AgentView struct {
	TaskID       string
	Title        string
	Duration     time.Duration
	Status       executor.AgentStatus
	Progress     string
	WorktreePath string
	LogPath      string   // Path to the log file (for historical runs)
	Error        string
	Output       []string // Last N lines of output
	Prompt       string   // The prompt sent to the LLM
	TokensInput  int
	TokensOutput int
	CostUSD      float64
}

// FlaggedPR represents a PR needing attention
type FlaggedPR struct {
	TaskID   string
	PRNumber int
	Reason   string
}

// WorkerView represents a connected worker for display
type WorkerView struct {
	ID          string
	MaxJobs     int
	ActiveJobs  int
	ConnectedAt time.Time
}

// ModelConfig holds initial data for the TUI model
type ModelConfig struct {
	MaxActive       int
	AllTasks        []*domain.Task
	Queued          []*domain.Task
	Agents          []*AgentView
	Flagged         []*FlaggedPR
	Workers         []*WorkerView
	ProjectRoot     string
	WorktreeDir     string
	PlansDir        string // Directory containing plans (for sync)
	BuildPoolURL    string // URL for build pool status (e.g., "http://localhost:8081")
	AgentManager    *executor.AgentManager
	WorktreeManager *executor.WorktreeManager
	RecoveredAgents []*AgentView // Agents recovered from previous session
	PlanWatcher     *observer.PlanWatcher
	PlanChangeChan  chan PlanSyncMsg
	Store           *taskstore.Store  // Database store for sync operations
	Syncer          *isync.Syncer     // Syncer for two-way sync operations
	CurrentVersion  string            // Current version for update checking
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

	// Build completed tasks map from task status
	completedTasks := make(map[string]bool)
	for _, t := range cfg.AllTasks {
		if t.Status == domain.StatusComplete {
			completedTasks[t.ID.String()] = true
		}
	}

	// Use provided managers or create defaults
	agentMgr := cfg.AgentManager
	if agentMgr == nil {
		agentMgr = executor.NewAgentManager(cfg.MaxActive)
	}

	// Set up syncer for agent manager and model if plans directory is configured
	syncer := cfg.Syncer
	if syncer == nil && cfg.PlansDir != "" {
		syncer = isync.New(cfg.PlansDir)
	}
	if syncer != nil {
		agentMgr.SetSyncer(syncer)
	}

	// Set build pool URL if configured
	if cfg.BuildPoolURL != "" {
		agentMgr.SetBuildPoolURL(cfg.BuildPoolURL)
	}

	worktreeMgr := cfg.WorktreeManager
	if worktreeMgr == nil && cfg.ProjectRoot != "" && cfg.WorktreeDir != "" {
		worktreeMgr = executor.NewWorktreeManager(cfg.ProjectRoot, cfg.WorktreeDir)
	}

	// Set status message if we recovered agents
	statusMsg := ""
	if len(cfg.RecoveredAgents) > 0 {
		stillRunning := 0
		for _, a := range cfg.RecoveredAgents {
			if a.Status == executor.AgentRunning {
				stillRunning++
			}
		}
		if stillRunning > 0 {
			statusMsg = fmt.Sprintf("Recovered %d agent(s): %d running", len(cfg.RecoveredAgents), stillRunning)
		} else {
			statusMsg = fmt.Sprintf("Recovered %d completed agent(s)", len(cfg.RecoveredAgents))
		}
	}

	// Determine initial build pool status
	buildPoolStatus := "disabled"
	if cfg.BuildPoolURL != "" {
		buildPoolStatus = "unreachable" // Will be updated on first fetch
	}

	return Model{
		maxActive:       cfg.MaxActive,
		allTasks:        cfg.AllTasks,
		queued:          cfg.Queued,
		agents:          agents,
		flagged:         cfg.Flagged,
		workers:         cfg.Workers,
		modules:         modules,
		completedTasks:  completedTasks,
		activeCount:     activeCount,
		activeTab:       0,
		projectRoot:     cfg.ProjectRoot,
		worktreeDir:     cfg.WorktreeDir,
		buildPoolURL:    cfg.BuildPoolURL,
		buildPoolStatus: buildPoolStatus,
		agentManager:    agentMgr,
		worktreeManager: worktreeMgr,
		planWatcher:     cfg.PlanWatcher,
		planChangeChan:  cfg.PlanChangeChan,
		statusMsg:       statusMsg,
		mouseEnabled:    true,
		store:           cfg.Store,
		syncer:          syncer,
		syncModal:       SyncConflictModal{Resolutions: make(map[string]string)},
		currentVersion:  cfg.CurrentVersion,
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

	// Check for updates on startup (async, non-blocking)
	if m.currentVersion != "" {
		cmds = append(cmds, checkUpdateCmd(m.currentVersion))
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
