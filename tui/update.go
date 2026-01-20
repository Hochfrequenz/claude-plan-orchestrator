package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/hochfrequenz/claude-plan-orchestrator/internal/buildpool"
	"github.com/hochfrequenz/claude-plan-orchestrator/internal/buildprotocol"
	"github.com/hochfrequenz/claude-plan-orchestrator/internal/domain"
	"github.com/hochfrequenz/claude-plan-orchestrator/internal/executor"
	"github.com/hochfrequenz/claude-plan-orchestrator/internal/maintenance"
	"github.com/hochfrequenz/claude-plan-orchestrator/internal/mcp"
	"github.com/hochfrequenz/claude-plan-orchestrator/internal/observer"
	"github.com/hochfrequenz/claude-plan-orchestrator/internal/parser"
	"github.com/hochfrequenz/claude-plan-orchestrator/internal/scheduler"
	isync "github.com/hochfrequenz/claude-plan-orchestrator/internal/sync"
	"github.com/hochfrequenz/claude-plan-orchestrator/internal/taskstore"
	"github.com/hochfrequenz/claude-plan-orchestrator/internal/updater"
)

// TestCompleteMsg is sent when test execution completes
type TestCompleteMsg struct {
	Output string
	Err    error
}

// PlanSyncMsg is sent when plan files change and need to be re-synced
type PlanSyncMsg struct {
	WorktreePath string
	ChangedFiles []string
}

// AgentStartInfo holds info about an agent that was started
type AgentStartInfo struct {
	TaskID       string
	WorktreePath string
}

// BatchStartMsg is sent to start a batch of tasks
type BatchStartMsg struct {
	Count   int
	Started []AgentStartInfo // Agents that were started
	Errors  []string         // Any errors during startup
}

// BatchPauseMsg is sent to pause batch execution
type BatchPauseMsg struct{}

// BatchResumeMsg is sent to resume batch execution
type BatchResumeMsg struct{}

// StatusUpdateMsg updates the status message
type StatusUpdateMsg string

// AgentUpdateMsg reports an agent status change
type AgentUpdateMsg struct {
	TaskID string
	Status executor.AgentStatus
	Error  error
}

// AgentCompleteMsg is sent when an agent finishes
type AgentCompleteMsg struct {
	TaskID  string
	Success bool
	Output  string
}

// AgentResumeMsg is sent when an agent resume completes
type AgentResumeMsg struct {
	TaskID  string
	Success bool
	Error   string
}

// AgentHistoryMsg contains loaded historical agent runs
type AgentHistoryMsg struct {
	History []*AgentView
	Error   error
}

// HistoryLogsMsg contains loaded logs for a historical agent run
type HistoryLogsMsg struct {
	Index int      // Index in agentHistory slice
	Lines []string // Log lines
	Error error
}

// WorkersUpdateMsg updates the workers list from build pool
type WorkersUpdateMsg struct {
	Workers []*WorkerView
	Status  string // "connected" or "unreachable"
}

// WorkerTestMsg reports the result of a worker test
type WorkerTestMsg struct {
	Success bool
	Output  string
	Error   string
}

// AgentTestMsg reports the result of an agent-based MCP tools test
type AgentTestMsg struct {
	TaskID  string
	Success bool
	Output  string
	Error   string
}

// MaintenanceStartMsg reports the result of starting a maintenance task
type MaintenanceStartMsg struct {
	TaskID       string
	Title        string
	WorktreePath string
	Prompt       string
	Success      bool
	Error        string
}

// AgentTestOutputMsg reports streaming output from the agent test
type AgentTestOutputMsg struct {
	TaskID string
	Lines  []string
}

// SyncCompleteMsg reports sync completion
type SyncCompleteMsg struct {
	Result *isync.SyncResult
	Err    error
}

// SyncResolveMsg reports conflict resolution completion
type SyncResolveMsg struct {
	Err error
}

// GroupPrioritiesMsg contains loaded group priority data
type GroupPrioritiesMsg struct {
	Items []GroupPriorityItem
	Error error
}

// SetGroupPriorityMsg reports result of setting a group priority
type SetGroupPriorityMsg struct {
	Group    string
	Priority int
	Error    error
}

// RemoveGroupPriorityMsg reports result of removing a group priority
type RemoveGroupPriorityMsg struct {
	Group string
	Error error
}

// UpdateCheckMsg reports the result of checking for updates
type UpdateCheckMsg struct {
	LatestVersion string
	NeedsUpdate   bool
	Error         error
}

// UpdateCompleteMsg reports the result of a self-update
type UpdateCompleteMsg struct {
	NewVersion string
	Success    bool
	Error      error
}

// Update handles messages
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		// Handle sync modal keys first (before any other keys)
		if m.syncModal.Visible {
			switch msg.String() {
			case "d":
				// Resolve current conflict as "db"
				if m.syncModal.Selected < len(m.syncModal.Conflicts) {
					taskID := m.syncModal.Conflicts[m.syncModal.Selected].TaskID
					m.syncModal.Resolutions[taskID] = "db"
				}
				return m, nil
			case "m":
				// Resolve current conflict as "markdown"
				if m.syncModal.Selected < len(m.syncModal.Conflicts) {
					taskID := m.syncModal.Conflicts[m.syncModal.Selected].TaskID
					m.syncModal.Resolutions[taskID] = "markdown"
				}
				return m, nil
			case "a":
				// Resolve all as "db"
				for _, c := range m.syncModal.Conflicts {
					m.syncModal.Resolutions[c.TaskID] = "db"
				}
				return m, nil
			case "j", "down":
				if m.syncModal.Selected < len(m.syncModal.Conflicts)-1 {
					m.syncModal.Selected++
				}
				return m, nil
			case "k", "up":
				if m.syncModal.Selected > 0 {
					m.syncModal.Selected--
				}
				return m, nil
			case "enter":
				// Apply resolutions if all conflicts are resolved
				allResolved := true
				for _, c := range m.syncModal.Conflicts {
					if m.syncModal.Resolutions[c.TaskID] == "" {
						allResolved = false
						break
					}
				}
				if allResolved {
					m.syncModal.Visible = false
					m.statusMsg = "Applying resolutions..."
					// Copy resolutions to avoid race conditions
					resCopy := make(map[string]string, len(m.syncModal.Resolutions))
					for k, v := range m.syncModal.Resolutions {
						resCopy[k] = v
					}
					return m, applyResolutionsCmd(m.syncer, m.store, resCopy)
				}
				m.statusMsg = "Please resolve all conflicts before applying"
				return m, nil
			case "esc":
				// Cancel and close modal
				m.syncModal.Visible = false
				m.syncModal.Conflicts = nil
				m.syncModal.Resolutions = make(map[string]string)
				m.syncModal.Selected = 0
				m.statusMsg = "Sync cancelled"
				return m, nil
			case "q", "ctrl+c":
				return m, tea.Quit
			}
			return m, nil // Consume all other keys when modal is open
		}

		// Handle maintenance modal keys
		if m.maintenanceModal.Visible {
			switch msg.String() {
			case "j", "down":
				if m.maintenanceModal.Phase == 0 {
					if m.maintenanceModal.Selected < len(m.maintenanceModal.Templates)-1 {
						m.maintenanceModal.Selected++
					}
				}
				return m, nil
			case "k", "up":
				if m.maintenanceModal.Phase == 0 {
					if m.maintenanceModal.Selected > 0 {
						m.maintenanceModal.Selected--
					}
				}
				return m, nil
			case "enter":
				if m.maintenanceModal.Phase == 0 {
					// Move to scope selection
					m.maintenanceModal.Phase = 1
					// Set target module from current selection
					if m.selectedModule < len(m.modules) {
						m.maintenanceModal.TargetModule = m.modules[m.selectedModule].Name
					}
				}
				return m, nil
			case "1":
				if m.maintenanceModal.Phase == 1 {
					m.maintenanceModal.SelectedScope = "module"
					return m, m.startMaintenanceTask()
				}
				return m, nil
			case "2":
				if m.maintenanceModal.Phase == 1 {
					m.maintenanceModal.SelectedScope = "package"
					return m, m.startMaintenanceTask()
				}
				return m, nil
			case "3":
				if m.maintenanceModal.Phase == 1 {
					m.maintenanceModal.SelectedScope = "all"
					return m, m.startMaintenanceTask()
				}
				return m, nil
			case "esc":
				if m.maintenanceModal.Phase == 1 {
					// Go back to template selection
					m.maintenanceModal.Phase = 0
				} else {
					// Close modal
					m.maintenanceModal.Visible = false
					m.maintenanceModal.Phase = 0
					m.maintenanceModal.Selected = 0
				}
				return m, nil
			case "q", "ctrl+c":
				return m, tea.Quit
			}
			return m, nil // Consume all other keys when modal is open
		}

		// Handle group priorities view keys
		if m.showGroupPriorities {
			switch msg.String() {
			case "j", "down":
				if m.selectedPriorityRow < len(m.groupPriorityItems)-1 {
					m.selectedPriorityRow++
				}
				return m, nil
			case "k", "up":
				if m.selectedPriorityRow > 0 {
					m.selectedPriorityRow--
				}
				return m, nil
			case "+", "=":
				// Increase tier (lower priority)
				if m.selectedPriorityRow < len(m.groupPriorityItems) {
					item := &m.groupPriorityItems[m.selectedPriorityRow]
					newPriority := item.Priority + 1
					if item.Priority < 0 {
						newPriority = 1 // Unassigned goes to tier 1
					}
					return m, setGroupPriorityCmd(m.store, item.Name, newPriority)
				}
				return m, nil
			case "-", "_":
				// Decrease tier (higher priority)
				if m.selectedPriorityRow < len(m.groupPriorityItems) {
					item := &m.groupPriorityItems[m.selectedPriorityRow]
					newPriority := item.Priority - 1
					if newPriority < 0 {
						newPriority = 0
					}
					return m, setGroupPriorityCmd(m.store, item.Name, newPriority)
				}
				return m, nil
			case "u":
				// Unassign (remove from table)
				if m.selectedPriorityRow < len(m.groupPriorityItems) {
					item := m.groupPriorityItems[m.selectedPriorityRow]
					return m, removeGroupPriorityCmd(m.store, item.Name)
				}
				return m, nil
			case "g", "esc":
				// Close priorities view
				m.showGroupPriorities = false
				return m, nil
			case "q", "ctrl+c":
				return m, tea.Quit
			}
			return m, nil // Consume all other keys when priorities view is open
		}

		switch msg.String() {
		case "q", "ctrl+c":
			return m, tea.Quit
		case "j", "down":
			m.selectedRow++
			if m.activeTab == 1 {
				m.taskScroll++
			}
			if m.activeTab == 2 { // Agents tab
				if m.showAgentDetail || m.showHistoryDetail {
					// Scroll agent output down
					m.agentOutputScroll++
				} else if m.showAgentHistory && len(m.agentHistory) > 0 {
					// Navigate history list
					if m.selectedHistory < len(m.agentHistory)-1 {
						m.selectedHistory++
					}
				} else if m.selectedAgent < len(m.agents)-1 {
					m.selectedAgent++
				}
			}
			if m.activeTab == 3 { // Modules tab
				if m.selectedModule < len(m.modules)-1 {
					m.selectedModule++
				}
				// Scroll if needed
				maxVisible := 12
				if m.selectedModule >= m.taskScroll+maxVisible {
					m.taskScroll = m.selectedModule - maxVisible + 1
				}
			}
		case "k", "up":
			if m.selectedRow > 0 {
				m.selectedRow--
			}
			if m.activeTab == 1 && m.taskScroll > 0 {
				m.taskScroll--
			}
			if m.activeTab == 2 { // Agents tab
				if m.showAgentDetail || m.showHistoryDetail {
					// Scroll agent output up
					if m.agentOutputScroll > 0 {
						m.agentOutputScroll--
					}
				} else if m.showAgentHistory && len(m.agentHistory) > 0 {
					// Navigate history list
					if m.selectedHistory > 0 {
						m.selectedHistory--
					}
				} else if m.selectedAgent > 0 {
					m.selectedAgent--
				}
			}
			if m.activeTab == 3 { // Modules tab
				if m.selectedModule > 0 {
					m.selectedModule--
				}
				// Scroll if needed
				if m.selectedModule < m.taskScroll {
					m.taskScroll = m.selectedModule
				}
			}
		case "g":
			// On Agents tab with detail view: jump to top of output
			if m.activeTab == 2 && (m.showAgentDetail || m.showHistoryDetail) {
				m.agentOutputScroll = 0
			} else if m.showGroupPriorities {
				// Close group priorities view
				m.showGroupPriorities = false
			} else if m.activeTab == 0 || m.activeTab == 3 {
				// Toggle group priorities view (only on Dashboard or Modules tab)
				m.showGroupPriorities = true
				m.selectedPriorityRow = 0
				// Load group priority data
				return m, loadGroupPrioritiesCmd(m.store)
			}
		case "G":
			// Jump to bottom of agent output (handled in view by setting to max)
			if m.activeTab == 2 && (m.showAgentDetail || m.showHistoryDetail) {
				m.agentOutputScroll = -1 // Signal to jump to end
			}
		case "enter":
			// Toggle agent/history detail view (only on Agents tab)
			if m.activeTab == 2 {
				if m.showAgentHistory && len(m.agentHistory) > 0 && m.selectedHistory < len(m.agentHistory) {
					// Open history detail view - load logs from file
					m.showHistoryDetail = true
					m.agentOutputScroll = 0
					histAgent := m.agentHistory[m.selectedHistory]
					if histAgent.LogPath != "" {
						// Load logs from file
						return m, loadHistoryLogsCmd(histAgent.LogPath, m.selectedHistory)
					}
				} else if len(m.agents) > 0 && !m.showAgentDetail {
					m.showAgentDetail = true
					// For running agents, show bottom (most recent)
					// For completed/failed agents, show from top (full output)
					agent := m.agents[m.selectedAgent]
					if agent.Status == executor.AgentRunning {
						m.agentOutputScroll = -1 // Start at bottom (most recent)
					} else {
						m.agentOutputScroll = 0 // Start at top (see full output)
					}
				} else if m.showAgentDetail {
					m.showAgentDetail = false
					m.agentOutputScroll = 0
				}
			}
		case "esc":
			// Close agent/history detail view
			if m.activeTab == 2 {
				if m.showHistoryDetail {
					m.showHistoryDetail = false
					m.agentOutputScroll = 0
				} else {
					m.showAgentDetail = false
					m.agentOutputScroll = 0
				}
			}
		case "r":
			// On Agents tab: refresh in overview, resume in detail view
			if m.activeTab == 2 {
				if m.showAgentDetail && len(m.agents) > 0 && m.selectedAgent < len(m.agents) {
					// Detail view: resume the agent
					av := m.agents[m.selectedAgent]
					// Can only resume completed or failed agents
					if av.Status == executor.AgentCompleted || av.Status == executor.AgentFailed {
						m.statusMsg = fmt.Sprintf("Resuming agent %s...", av.TaskID)
						return m, resumeAgentCmd(m.agentManager, av.TaskID)
					} else if av.Status == executor.AgentRunning {
						m.statusMsg = "Agent is already running"
					} else {
						m.statusMsg = "Cannot resume agent in this state"
					}
				} else {
					// Overview: refresh agent list
					m.updateAgentsFromManager()
					m.statusMsg = "Agents refreshed"
				}
			}
		case "h":
			// Toggle agent history on Agents tab
			if m.activeTab == 2 && !m.showAgentDetail {
				m.showAgentHistory = !m.showAgentHistory
				if m.showAgentHistory {
					m.statusMsg = "Loading history..."
					return m, loadAgentHistoryCmd(m.store)
				} else {
					m.agentHistory = nil
					m.statusMsg = "History hidden"
				}
			}
		case "tab":
			m.activeTab = (m.activeTab + 1) % 5
			m.selectedRow = 0
			m.taskScroll = 0
		case "t":
			// Toggle to tasks tab
			m.activeTab = 1
			m.taskScroll = 0
		case "m":
			if m.activeTab == 3 && !m.syncModal.Visible && !m.testRunning {
				// On Modules tab: open maintenance modal
				m.maintenanceModal.Visible = true
				m.maintenanceModal.Phase = 0
				m.maintenanceModal.Selected = 0
				m.maintenanceModal.Templates = maintenance.BuiltinTemplates()
				// Set target module from current selection
				if m.selectedModule < len(m.modules) {
					m.maintenanceModal.TargetModule = m.modules[m.selectedModule].Name
				}
			} else {
				// Not on Modules tab: switch to modules tab
				m.activeTab = 3
				m.taskScroll = 0
			}
		case "v":
			// Toggle view mode (priority/module)
			if m.viewMode == ViewByPriority {
				m.viewMode = ViewByModule
			} else {
				m.viewMode = ViewByPriority
			}
		case "x":
			// Execute tests for selected module (only on modules tab)
			if m.activeTab == 3 && len(m.modules) > 0 && !m.testRunning {
				m.testRunning = true
				m.testOutput = ""
				moduleName := m.modules[m.selectedModule].Name
				return m, runModuleTests(m.projectRoot, moduleName)
			}
		case "+", "=":
			// Increase max agents (only on agents tab)
			if m.activeTab == 2 {
				if m.maxActive < 10 {
					m.maxActive++
					m.configChanged = true
					// Sync with agent manager
					if m.agentManager != nil {
						m.agentManager.SetMaxConcurrent(m.maxActive)
					}
				}
			}
		case "-", "_":
			// Decrease max agents (only on agents tab)
			if m.activeTab == 2 {
				if m.maxActive > 1 {
					m.maxActive--
					m.configChanged = true
					// Sync with agent manager
					if m.agentManager != nil {
						m.agentManager.SetMaxConcurrent(m.maxActive)
					}
				}
			}
		case "s":
			// Start batch (only on Dashboard tab)
			if m.activeTab == 0 && !m.batchRunning {
				slotsAvailable := m.maxActive - m.activeCount
				if slotsAvailable > 0 && len(m.queued) > 0 {
					// Get in-progress task IDs from currently running agents
					inProgress := make(map[string]bool)
					for _, a := range m.agents {
						if a.Status == executor.AgentRunning {
							inProgress[a.TaskID] = true
						}
					}

					// Load group priorities from store if available
					var groupPriorities map[string]int
					if m.store != nil {
						groupPriorities, _ = m.store.GetGroupPriorities()
					}

					// Use scheduler to select tasks that don't conflict with running agents
					var sched *scheduler.Scheduler
					if len(groupPriorities) > 0 {
						sched = scheduler.NewWithPriorities(m.queued, m.completedTasks, groupPriorities)
					} else {
						sched = scheduler.New(m.queued, m.completedTasks)
					}
					readyTasks := sched.GetReadyTasksExcluding(slotsAvailable, inProgress)

					if len(readyTasks) > 0 {
						m.batchRunning = true
						m.batchPaused = false
						m.statusMsg = fmt.Sprintf("Starting batch: %d task(s)...", len(readyTasks))
						return m, startBatchCmd(
							m.projectRoot,
							readyTasks,
							m.worktreeManager,
							m.agentManager,
							m.planWatcher,
						)
					} else {
						m.statusMsg = "No independent tasks ready (dependencies pending)"
					}
				} else if slotsAvailable == 0 {
					m.statusMsg = "No agent slots available"
				} else {
					m.statusMsg = "No tasks queued"
				}
			} else if m.activeTab == 3 && !m.syncModal.Visible && m.syncer != nil && m.store != nil {
				// Sync (only on Modules tab when not already syncing)
				m.statusMsg = "Syncing..."
				return m, startSyncCmd(m.syncer, m.store)
			} else if m.activeTab == 3 && (m.syncer == nil || m.store == nil) {
				m.statusMsg = "Sync not available (no plans directory or database)"
			}
		case "p":
			// Toggle prompt view in agent detail
			if m.activeTab == 2 && m.showAgentDetail {
				m.showAgentPrompt = !m.showAgentPrompt
				m.agentOutputScroll = 0 // Reset scroll when switching views
			} else if m.activeTab == 0 {
				// Pause/Resume batch (only on Dashboard tab)
				if m.batchRunning && !m.batchPaused {
					m.batchPaused = true
					m.statusMsg = "Batch paused"
				} else if m.batchRunning && m.batchPaused {
					m.batchPaused = false
					m.statusMsg = "Batch resumed"
				}
			}
		case "a":
			// Toggle auto mode (only on Dashboard tab)
			if m.activeTab == 0 {
				m.autoMode = !m.autoMode
				if m.autoMode {
					m.statusMsg = "Auto mode ON - will start tasks as slots become available"
					// If not batch running and slots available, start immediately
					if !m.batchRunning {
						return m, m.tryStartAutoTasks()
					}
				} else {
					m.statusMsg = "Auto mode OFF"
				}
			}
		case "T":
			// Test worker connection (only on Dashboard tab when build pool is connected)
			if m.activeTab == 0 && m.buildPoolURL != "" && m.buildPoolStatus == "connected" {
				m.statusMsg = "Testing worker..."
				return m, testWorkerCmd(m.buildPoolURL, m.projectRoot, m.gitDaemonPort)
			} else if m.buildPoolStatus != "connected" {
				m.statusMsg = "Build pool not connected"
			}
		case "E":
			// Test worker error handling (Dashboard tab)
			if m.activeTab == 0 {
				if m.buildPoolURL != "" && m.buildPoolStatus == "connected" {
					// Test via coordinator (may use remote workers or embedded fallback)
					m.statusMsg = "Testing error handling via coordinator..."
					return m, testWorkerErrorCmd(m.buildPoolURL, m.projectRoot)
				} else {
					// Test embedded worker directly (no coordinator running)
					m.statusMsg = "Testing embedded worker directly..."
					return m, testEmbeddedWorkerDirectCmd(m.projectRoot)
				}
			}
		case "A":
			// Run agent test (Dashboard tab) - spawns Claude to test MCP tools
			if m.activeTab == 0 {
				// Generate unique task ID for the test agent
				taskID := fmt.Sprintf("mcp-test-%d", time.Now().UnixNano())

				// Add agent to view immediately
				m.agents = append(m.agents, &AgentView{
					TaskID: taskID,
					Title:  "MCP Tools Test",
					Status: executor.AgentRunning,
				})
				m.activeCount++

				// Get executor type from agent manager
				executorType := ""
				if m.agentManager != nil {
					executorType = string(m.agentManager.GetExecutorType())
				}

				if m.buildPoolURL != "" && m.buildPoolStatus == "connected" {
					// Use external coordinator
					m.statusMsg = "Starting agent test via coordinator..."
					return m, runAgentTestCmd(taskID, m.buildPoolURL, m.projectRoot, executorType)
				} else {
					// Start temporary coordinator with embedded worker
					m.statusMsg = "Starting agent test with embedded worker..."
					return m, runAgentTestWithEmbeddedCmd(taskID, m.projectRoot, executorType)
				}
			}
		case "M":
			// Toggle mouse mode (allows text selection when disabled)
			m.mouseEnabled = !m.mouseEnabled
			if m.mouseEnabled {
				m.statusMsg = "Mouse enabled (Shift+drag to select text)"
				return m, tea.EnableMouseCellMotion
			} else {
				m.statusMsg = "Mouse disabled (text selection enabled)"
				return m, tea.DisableMouse
			}
		case "U":
			// Trigger self-update if update is available
			if m.updateAvailable != "" && !m.updateInProgress {
				m.updateInProgress = true
				m.updateStatus = "Downloading..."
				return m, selfUpdateCmd(m.updateAvailable)
			} else if m.updateInProgress {
				m.statusMsg = "Update already in progress..."
			} else if m.updateAvailable == "" {
				m.statusMsg = "No update available (current: " + m.currentVersion + ")"
			}
		}

	case tea.MouseMsg:
		switch msg.Button {
		case tea.MouseButtonWheelUp:
			// Scroll up
			if m.activeTab == 2 && m.showAgentDetail {
				if m.agentOutputScroll > 0 {
					m.agentOutputScroll -= 3
					if m.agentOutputScroll < 0 {
						m.agentOutputScroll = 0
					}
				}
			} else if m.activeTab == 1 && m.taskScroll > 0 {
				m.taskScroll -= 3
				if m.taskScroll < 0 {
					m.taskScroll = 0
				}
			} else if m.activeTab == 3 && m.taskScroll > 0 {
				m.taskScroll -= 3
				if m.taskScroll < 0 {
					m.taskScroll = 0
				}
			}
		case tea.MouseButtonWheelDown:
			// Scroll down
			if m.activeTab == 2 && m.showAgentDetail {
				m.agentOutputScroll += 3
			} else if m.activeTab == 1 {
				m.taskScroll += 3
			} else if m.activeTab == 3 {
				m.taskScroll += 3
			}
		case tea.MouseButtonLeft:
			// Handle clicks
			if msg.Y == 1 {
				// Click on tab bar (row 1)
				// Tabs are roughly: Dashboard | Tasks | Agents | Modules | PRs
				// Each tab is about 12 chars wide
				tabIndex := msg.X / 12
				if tabIndex >= 0 && tabIndex < 5 {
					m.activeTab = tabIndex
					m.taskScroll = 0
					m.showAgentDetail = false
				}
			} else if m.activeTab == 2 && !m.showAgentDetail && len(m.agents) > 0 {
				// Click on agent list - select agent (rows start around 5)
				clickedRow := msg.Y - 5
				if clickedRow >= 0 && clickedRow < len(m.agents) {
					m.selectedAgent = clickedRow
				}
			}
		case tea.MouseButtonRight:
			// Right click to go back in agent detail
			if m.activeTab == 2 && m.showAgentDetail {
				m.showAgentDetail = false
				m.agentOutputScroll = 0
			}
		}
		return m, nil

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height

	case TickMsg:
		// Refresh agent status from manager (always update if we have running agents)
		if m.agentManager != nil {
			m.updateAgentsFromManager()
		}
		// Update test agent output from shared buffer
		m.updateTestAgentOutput()
		// Fetch workers if build pool is configured
		cmds := []tea.Cmd{tickCmd()}
		if m.buildPoolURL != "" {
			cmds = append(cmds, fetchWorkersCmd(m.buildPoolURL))
		}
		// In auto mode, periodically try to start new tasks
		if m.autoMode && !m.batchPaused && !m.batchRunning {
			if cmd := m.tryStartAutoTasks(); cmd != nil {
				cmds = append(cmds, cmd)
			}
		}
		return m, tea.Batch(cmds...)

	case WorkersUpdateMsg:
		m.workers = msg.Workers
		m.buildPoolStatus = msg.Status
		return m, nil

	case AgentUpdateMsg:
		// Update agent view with new status
		for i, a := range m.agents {
			if a.TaskID == msg.TaskID {
				m.agents[i].Status = msg.Status
				break
			}
		}
		// Update active count
		m.activeCount = 0
		for _, a := range m.agents {
			if a.Status == executor.AgentRunning {
				m.activeCount++
			}
		}
		return m, nil

	case AgentCompleteMsg:
		// Handle agent completion
		var completedIdx int = -1
		for i, a := range m.agents {
			if a.TaskID == msg.TaskID {
				if msg.Success {
					// Mark task as completed for dependency tracking
					if m.completedTasks == nil {
						m.completedTasks = make(map[string]bool)
					}
					m.completedTasks[msg.TaskID] = true
					// Update task status in allTasks for module statistics
					for j, task := range m.allTasks {
						if task.ID.String() == msg.TaskID {
							m.allTasks[j].Status = domain.StatusComplete
							break
						}
					}
					// Mark for removal from agents list
					completedIdx = i
				} else {
					m.agents[i].Status = executor.AgentFailed
				}
				break
			}
		}
		// Remove successfully completed agent from the list
		if completedIdx >= 0 {
			// Clean up worktree on successful completion
			agent := m.agents[completedIdx]
			if m.worktreeManager != nil && agent.WorktreePath != "" {
				if err := m.worktreeManager.Remove(agent.WorktreePath); err != nil {
					m.statusMsg = fmt.Sprintf("Warning: worktree cleanup failed: %v", err)
				}
			}
			m.agents = append(m.agents[:completedIdx], m.agents[completedIdx+1:]...)
			// Adjust selected agent index if needed
			if m.selectedAgent >= len(m.agents) && m.selectedAgent > 0 {
				m.selectedAgent--
			}
			// Recompute module summaries
			m.modules = computeModuleSummaries(m.allTasks)
		}
		// Update active count
		m.activeCount = 0
		for _, a := range m.agents {
			if a.Status == executor.AgentRunning {
				m.activeCount++
			}
		}
		// Check if all agents are done
		allDone := true
		for _, a := range m.agents {
			if a.Status == executor.AgentRunning || a.Status == executor.AgentQueued {
				allDone = false
				break
			}
		}
		if allDone && m.batchRunning {
			m.batchRunning = false
			if m.autoMode {
				// In auto mode, check if more tasks are now ready
				return m, m.tryStartAutoTasks()
			}
			m.statusMsg = "Batch complete"
		} else if m.autoMode && !m.batchPaused {
			// In auto mode, try to start more tasks as agents complete
			return m, m.tryStartAutoTasks()
		}
		return m, nil

	case TestCompleteMsg:
		m.testRunning = false
		if msg.Err != nil {
			m.testOutput = "Error: " + msg.Err.Error()
		} else {
			m.testOutput = msg.Output
		}
		return m, nil

	case WorkerTestMsg:
		if msg.Success {
			m.statusMsg = fmt.Sprintf("Worker test OK: %s", strings.TrimSpace(msg.Output))
		} else {
			m.statusMsg = fmt.Sprintf("Worker test failed: %s", msg.Error)
		}
		return m, nil

	case AgentTestMsg:
		// Update the test agent in the agents list
		for i, a := range m.agents {
			if a.TaskID == msg.TaskID {
				if msg.Success {
					m.agents[i].Status = executor.AgentCompleted
					m.statusMsg = "Agent test PASSED"
				} else {
					m.agents[i].Status = executor.AgentFailed
					m.agents[i].Error = msg.Error
					m.statusMsg = fmt.Sprintf("Agent test: %s", msg.Error)
				}
				// Store full output in the agent view
				if msg.Output != "" {
					lines := strings.Split(msg.Output, "\n")
					m.agents[i].Output = lines
				}
				m.activeCount--
				if m.activeCount < 0 {
					m.activeCount = 0
				}
				break
			}
		}
		return m, nil

	case AgentResumeMsg:
		if msg.Success {
			// Update agent view status
			for i, a := range m.agents {
				if a.TaskID == msg.TaskID {
					m.agents[i].Status = executor.AgentRunning
					m.activeCount++
					break
				}
			}
			m.statusMsg = fmt.Sprintf("Resumed agent %s", msg.TaskID)
			m.batchRunning = true // Re-enable batch tracking
		} else {
			m.statusMsg = fmt.Sprintf("Failed to resume %s: %s", msg.TaskID, msg.Error)
		}
		return m, nil

	case StatusUpdateMsg:
		m.statusMsg = string(msg)
		return m, nil

	case PlanSyncMsg:
		// Re-parse the changed plan files and update task data
		updatedCount := 0
		for _, filePath := range msg.ChangedFiles {
			task, err := parser.ParseEpicFile(filePath)
			if err != nil {
				continue // Skip files that can't be parsed
			}

			// Find and update the matching task in our list
			for i, t := range m.allTasks {
				if t.ID.String() == task.ID.String() {
					// Update task with new data from file
					m.allTasks[i].Status = task.Status
					m.allTasks[i].TestSummary = task.TestSummary
					m.allTasks[i].NeedsReview = task.NeedsReview
					updatedCount++
					break
				}
			}
		}

		if updatedCount > 0 {
			m.statusMsg = fmt.Sprintf("Updated %d task(s) from %s", updatedCount, msg.WorktreePath)
			// Recompute module summaries from updated tasks
			m.modules = computeModuleSummaries(m.allTasks)
		}

		// Continue listening for more changes
		if m.planChangeChan != nil {
			return m, waitForPlanChange(m.planChangeChan)
		}
		return m, nil

	case BatchStartMsg:
		// Batch has been initiated - add agents to view
		startedTaskIDs := make(map[string]bool)
		for _, info := range msg.Started {
			startedTaskIDs[info.TaskID] = true
			// Find the task to get its title
			var title string
			for _, t := range m.queued {
				if t.ID.String() == info.TaskID {
					title = t.Title
					break
				}
			}
			m.agents = append(m.agents, &AgentView{
				TaskID:       info.TaskID,
				Title:        title,
				Status:       executor.AgentRunning,
				Duration:     0,
				WorktreePath: info.WorktreePath,
			})
			m.activeCount++
		}
		// Update task status in allTasks to in_progress for started tasks
		for _, t := range m.allTasks {
			if startedTaskIDs[t.ID.String()] {
				t.Status = domain.StatusInProgress
			}
		}
		// Recompute module summaries to reflect in-progress tasks
		m.modules = computeModuleSummaries(m.allTasks)

		// Remove started tasks from queued
		var remaining []*domain.Task
		for _, t := range m.queued {
			if !startedTaskIDs[t.ID.String()] {
				remaining = append(remaining, t)
			}
		}
		m.queued = remaining

		if len(msg.Errors) > 0 {
			// Show all errors (truncated if too long)
			errorDetails := strings.Join(msg.Errors, "; ")
			if len(errorDetails) > 200 {
				errorDetails = errorDetails[:200] + "..."
			}
			if msg.Count > 0 {
				m.statusMsg = fmt.Sprintf("Batch: %d started, %d failed: %s", msg.Count, len(msg.Errors), errorDetails)
			} else {
				m.statusMsg = fmt.Sprintf("Batch failed: %s", errorDetails)
			}
		} else {
			m.statusMsg = fmt.Sprintf("Batch started: %d task(s)", msg.Count)
		}
		return m, nil

	case SyncCompleteMsg:
		if msg.Err != nil {
			m.statusMsg = fmt.Sprintf("Sync failed: %v", msg.Err)
		} else if len(msg.Result.Conflicts) > 0 {
			// Show conflict modal
			m.syncModal.Visible = true
			m.syncModal.Conflicts = msg.Result.Conflicts
			m.syncModal.Resolutions = make(map[string]string)
			m.syncModal.Selected = 0
			m.statusMsg = fmt.Sprintf("%d conflict(s) found", len(msg.Result.Conflicts))
		} else {
			// Success - reload tasks from database to update UI
			if err := m.reloadTasksFromStore(); err != nil {
				m.statusMsg = fmt.Sprintf("Sync succeeded but failed to reload: %v", err)
			} else {
				// Show flash
				total := msg.Result.MarkdownToDBCount + msg.Result.DBToMarkdownCount
				if total > 0 {
					m.syncFlash = fmt.Sprintf("Synced %d task(s) ✓", total)
				} else {
					m.syncFlash = "Already in sync ✓"
				}
				m.syncFlashExp = time.Now().Add(2 * time.Second)
				m.statusMsg = ""
			}
		}
		return m, nil

	case SyncResolveMsg:
		if msg.Err != nil {
			m.statusMsg = fmt.Sprintf("Resolution failed: %v", msg.Err)
		} else {
			// Reload tasks from database to update UI with resolved state
			if err := m.reloadTasksFromStore(); err != nil {
				m.statusMsg = fmt.Sprintf("Resolved but failed to reload: %v", err)
			} else {
				m.syncFlash = "Conflicts resolved ✓"
				m.syncFlashExp = time.Now().Add(2 * time.Second)
				m.statusMsg = ""
			}
		}
		return m, nil

	case AgentHistoryMsg:
		if msg.Error != nil {
			m.statusMsg = fmt.Sprintf("Failed to load history: %v", msg.Error)
			m.showAgentHistory = false
		} else {
			m.agentHistory = msg.History
			m.selectedHistory = 0 // Reset selection
			m.statusMsg = fmt.Sprintf("Showing %d historical runs (h to hide)", len(msg.History))
		}
		return m, nil

	case HistoryLogsMsg:
		if msg.Error != nil {
			m.statusMsg = fmt.Sprintf("Failed to load logs: %v", msg.Error)
		} else if msg.Index < len(m.agentHistory) {
			m.agentHistory[msg.Index].Output = msg.Lines
			m.statusMsg = fmt.Sprintf("Loaded %d log lines", len(msg.Lines))
		}
		return m, nil

	case MaintenanceStartMsg:
		if msg.Success {
			// Add maintenance agent to the view
			m.agents = append(m.agents, &AgentView{
				TaskID:       msg.TaskID,
				Title:        msg.Title,
				WorktreePath: msg.WorktreePath,
				Prompt:       msg.Prompt,
				Status:       executor.AgentRunning,
			})
			m.activeCount++
			m.batchRunning = true
			m.statusMsg = fmt.Sprintf("Started maintenance task: %s", msg.Title)
		} else {
			m.statusMsg = fmt.Sprintf("Maintenance failed: %s", msg.Error)
		}
		return m, nil

	case GroupPrioritiesMsg:
		if msg.Error != nil {
			m.statusMsg = fmt.Sprintf("Failed to load priorities: %v", msg.Error)
			m.showGroupPriorities = false
		} else {
			m.groupPriorityItems = msg.Items
			// Sort items to match display order: by tier ascending, unassigned (-1) at end
			sort.Slice(m.groupPriorityItems, func(i, j int) bool {
				pi, pj := m.groupPriorityItems[i].Priority, m.groupPriorityItems[j].Priority
				// Unassigned (-1) goes to the end
				if pi < 0 && pj >= 0 {
					return false
				}
				if pj < 0 && pi >= 0 {
					return true
				}
				// Both unassigned or both assigned: sort by priority
				return pi < pj
			})
		}
		return m, nil

	case SetGroupPriorityMsg:
		if msg.Error != nil {
			m.statusMsg = fmt.Sprintf("Failed to set priority: %v", msg.Error)
		} else {
			// Update local state and re-sort to match display order
			var selectedName string
			if m.selectedPriorityRow < len(m.groupPriorityItems) {
				selectedName = m.groupPriorityItems[m.selectedPriorityRow].Name
			}
			for i, item := range m.groupPriorityItems {
				if item.Name == msg.Group {
					m.groupPriorityItems[i].Priority = msg.Priority
					break
				}
			}
			// Re-sort items to match display order
			sort.Slice(m.groupPriorityItems, func(i, j int) bool {
				pi, pj := m.groupPriorityItems[i].Priority, m.groupPriorityItems[j].Priority
				if pi < 0 && pj >= 0 {
					return false
				}
				if pj < 0 && pi >= 0 {
					return true
				}
				return pi < pj
			})
			// Update selectedPriorityRow to follow the item
			for i, item := range m.groupPriorityItems {
				if item.Name == selectedName {
					m.selectedPriorityRow = i
					break
				}
			}
			m.statusMsg = fmt.Sprintf("Set %s to tier %d", msg.Group, msg.Priority)
		}
		return m, nil

	case RemoveGroupPriorityMsg:
		if msg.Error != nil {
			m.statusMsg = fmt.Sprintf("Failed to unassign: %v", msg.Error)
		} else {
			// Update local state and re-sort to match display order
			var selectedName string
			if m.selectedPriorityRow < len(m.groupPriorityItems) {
				selectedName = m.groupPriorityItems[m.selectedPriorityRow].Name
			}
			for i, item := range m.groupPriorityItems {
				if item.Name == msg.Group {
					m.groupPriorityItems[i].Priority = -1
					break
				}
			}
			// Re-sort items to match display order
			sort.Slice(m.groupPriorityItems, func(i, j int) bool {
				pi, pj := m.groupPriorityItems[i].Priority, m.groupPriorityItems[j].Priority
				if pi < 0 && pj >= 0 {
					return false
				}
				if pj < 0 && pi >= 0 {
					return true
				}
				return pi < pj
			})
			// Update selectedPriorityRow to follow the item
			for i, item := range m.groupPriorityItems {
				if item.Name == selectedName {
					m.selectedPriorityRow = i
					break
				}
			}
			m.statusMsg = fmt.Sprintf("Unassigned %s (defaults to tier 0)", msg.Group)
		}
		return m, nil

	case UpdateCheckMsg:
		if msg.Error != nil {
			// Silently ignore update check errors - don't disrupt user
			m.updateStatus = ""
		} else if msg.NeedsUpdate {
			m.updateAvailable = msg.LatestVersion
			m.updateStatus = ""
		} else {
			m.updateAvailable = ""
			m.updateStatus = ""
		}
		return m, nil

	case UpdateCompleteMsg:
		m.updateInProgress = false
		if msg.Error != nil {
			m.updateStatus = fmt.Sprintf("Update failed: %v", msg.Error)
			m.statusMsg = m.updateStatus
		} else {
			m.updateStatus = fmt.Sprintf("Updated to %s! Restart to apply.", msg.NewVersion)
			m.statusMsg = m.updateStatus
		}
		return m, nil
	}

	return m, nil
}

// SetAgents updates the agents list
func (m *Model) SetAgents(agents []*AgentView) {
	m.agents = agents
	m.activeCount = 0
	for _, a := range agents {
		if a.Status == executor.AgentRunning {
			m.activeCount++
		}
	}
}

// SetTasks updates the all tasks list
func (m *Model) SetTasks(tasks []*domain.Task) {
	m.allTasks = tasks
}

// SetQueued updates the queued tasks list
func (m *Model) SetQueued(tasks []*domain.Task) {
	m.queued = tasks
}

// reloadTasksFromStore reloads all tasks from the database and updates derived state
func (m *Model) reloadTasksFromStore() error {
	if m.store == nil {
		return fmt.Errorf("store is nil")
	}

	tasks, err := m.store.ListTasks(taskstore.ListOptions{})
	if err != nil {
		return err
	}

	// Update allTasks
	m.allTasks = tasks

	// Rebuild completedTasks map
	m.completedTasks = make(map[string]bool)
	for _, t := range tasks {
		if t.Status == domain.StatusComplete {
			m.completedTasks[t.ID.String()] = true
		}
	}

	// Rebuild queued (non-completed tasks)
	var queued []*domain.Task
	for _, t := range tasks {
		if t.Status != domain.StatusComplete {
			queued = append(queued, t)
		}
	}
	m.queued = queued

	// Recompute module summaries
	m.modules = computeModuleSummaries(tasks)

	return nil
}

// updateTestAgentOutput copies streaming output from the shared buffer to the test agent
func (m *Model) updateTestAgentOutput() {
	testAgentMutex.Lock()
	taskID := testAgentTaskID
	output := make([]string, len(testAgentOutput))
	copy(output, testAgentOutput)
	testAgentMutex.Unlock()

	if taskID == "" || len(output) == 0 {
		return
	}

	// Find the test agent and update its output (update while running)
	for i, a := range m.agents {
		if a.TaskID == taskID {
			m.agents[i].Output = output
			break
		}
	}
}

// updateAgentsFromManager syncs the agents view with the agent manager
func (m *Model) updateAgentsFromManager() {
	if m.agentManager == nil {
		return
	}

	// Update duration and status for each agent in the view
	m.activeCount = 0
	allDone := true

	// Track agents to remove (completed successfully)
	var toRemove []int

	for i, av := range m.agents {
		agent := m.agentManager.Get(av.TaskID)
		if agent == nil {
			continue
		}

		// Track status change to detect completions
		prevStatus := av.Status
		av.Status = agent.Status
		av.Duration = agent.Duration()
		av.WorktreePath = agent.WorktreePath

		// Capture error if any
		if agent.Error != nil {
			av.Error = agent.Error.Error()
		}

		// Capture last N lines of output (keep more for better context)
		output := agent.GetOutput()
		maxLines := 100
		if len(output) > maxLines {
			av.Output = output[len(output)-maxLines:]
		} else {
			av.Output = output
		}

		// Capture prompt
		av.Prompt = agent.Prompt

		// Capture token usage
		tokensIn, tokensOut, cost := agent.GetUsage()
		av.TokensInput = tokensIn
		av.TokensOutput = tokensOut
		av.CostUSD = cost

		// Mark task as completed for dependency tracking when status changes to completed
		if agent.Status == executor.AgentCompleted && prevStatus != executor.AgentCompleted {
			if m.completedTasks == nil {
				m.completedTasks = make(map[string]bool)
			}
			m.completedTasks[av.TaskID] = true
			// Update task status in allTasks for module statistics
			for j, task := range m.allTasks {
				if task.ID.String() == av.TaskID {
					m.allTasks[j].Status = domain.StatusComplete
					break
				}
			}
			// Mark for removal from agents list
			toRemove = append(toRemove, i)
		}

		if agent.Status == executor.AgentRunning {
			m.activeCount++
			allDone = false
		} else if agent.Status == executor.AgentQueued {
			allDone = false
		}
	}

	// Clean up worktrees for completed agents before removing them
	for _, idx := range toRemove {
		agent := m.agents[idx]
		if m.worktreeManager != nil && agent.WorktreePath != "" {
			if err := m.worktreeManager.Remove(agent.WorktreePath); err != nil {
				m.statusMsg = fmt.Sprintf("Warning: worktree cleanup failed: %v", err)
			}
		}
	}

	// Remove completed agents (iterate in reverse to maintain correct indices)
	for i := len(toRemove) - 1; i >= 0; i-- {
		idx := toRemove[i]
		m.agents = append(m.agents[:idx], m.agents[idx+1:]...)
	}
	// Adjust selected agent index if needed
	if len(toRemove) > 0 && m.selectedAgent >= len(m.agents) && m.selectedAgent > 0 {
		m.selectedAgent = len(m.agents) - 1
		if m.selectedAgent < 0 {
			m.selectedAgent = 0
		}
	}
	// Recompute module summaries if any agents completed
	if len(toRemove) > 0 {
		m.modules = computeModuleSummaries(m.allTasks)
	}

	// Check if batch is complete
	// Note: We check (len(m.agents) > 0 || len(toRemove) > 0) because completed agents
	// were already removed from m.agents, so we need to account for them via toRemove
	if allDone && m.batchRunning && (len(m.agents) > 0 || len(toRemove) > 0) {
		m.batchRunning = false
		if !m.autoMode {
			m.statusMsg = "Batch complete"
		}
	}
}

// GetMaxActive returns the current max active agents setting
func (m Model) GetMaxActive() int {
	return m.maxActive
}

// ConfigChanged returns true if the configuration was modified
func (m Model) ConfigChanged() bool {
	return m.configChanged
}

// runModuleTests executes tests for a specific module via MCP test runner
func runModuleTests(projectRoot, moduleName string) tea.Cmd {
	return func() tea.Msg {
		// Try to connect to MCP test runner
		runner, err := mcp.NewTestRunner(projectRoot)
		if err != nil {
			return TestCompleteMsg{
				Output: "",
				Err:    fmt.Errorf("failed to connect to test runner: %w", err),
			}
		}
		defer runner.Close()

		// Sync and run tests with the module filter
		output := fmt.Sprintf("Starting tests for module: %s\n", moduleName)

		// Start the test run
		runResult, err := runner.RunTests(moduleName, true, true)
		if err != nil {
			return TestCompleteMsg{
				Output: output,
				Err:    fmt.Errorf("failed to start tests: %w", err),
			}
		}

		output += fmt.Sprintf("Test run started: %s\n", runResult.RunID)

		// Poll for results (blocking)
		var results *mcp.TestResults
		for i := 0; i < 120; i++ { // Timeout after 2 minutes
			results, err = runner.GetTestResults(runResult.RunID, false, 20)
			if err != nil {
				return TestCompleteMsg{
					Output: output,
					Err:    fmt.Errorf("failed to get results: %w", err),
				}
			}

			if results.Status != "running" {
				break
			}

			time.Sleep(1 * time.Second)
		}

		// Format results
		if results.Summary != nil {
			output += fmt.Sprintf("\nResults:\n")
			output += fmt.Sprintf("  Total:   %d\n", results.Summary.Total)
			output += fmt.Sprintf("  Passed:  %d\n", results.Summary.Passed)
			output += fmt.Sprintf("  Failed:  %d\n", results.Summary.Failed)
			output += fmt.Sprintf("  Skipped: %d\n", results.Summary.Skipped)
		}

		if results.Output != "" {
			output += fmt.Sprintf("\nOutput:\n%s", results.Output)
		}

		if results.Status == "failed" || (results.Summary != nil && !results.Summary.Success) {
			return TestCompleteMsg{
				Output: output,
				Err:    fmt.Errorf("tests failed"),
			}
		}

		return TestCompleteMsg{
			Output: output,
			Err:    nil,
		}
	}
}

// startBatchCmd initiates batch execution of queued tasks
func startBatchCmd(
	projectRoot string,
	tasks []*domain.Task,
	wtMgr *executor.WorktreeManager,
	agentMgr *executor.AgentManager,
	planWatcher *observer.PlanWatcher,
) tea.Cmd {
	return func() tea.Msg {
		var started []AgentStartInfo
		var errors []string

		for _, task := range tasks {
			// Create worktree for this task
			var wtPath string
			var err error
			if wtMgr != nil {
				wtPath, err = wtMgr.Create(task.ID)
				if err != nil {
					errors = append(errors, fmt.Sprintf("%s: worktree: %v", task.ID.String(), err))
					continue
				}
				// Add worktree to plan watcher
				if planWatcher != nil {
					planWatcher.AddWorktree(wtPath)
				}
			} else {
				// No worktree manager - use project root directly (for testing)
				wtPath = projectRoot
			}

			// Build prompt for the agent
			prompt := executor.BuildPrompt(task, task.Description, "", nil)

			// Create the agent with status callback for persistence
			agent := &executor.Agent{
				TaskID:        task.ID,
				WorktreePath:  wtPath,
				EpicFilePath:  task.FilePath, // For sync callback to update epic status
				Status:        executor.AgentQueued,
				Prompt:        prompt,
				BuildPoolURL:  agentMgr.GetBuildPoolURL(),
				ExecutorType:  agentMgr.GetExecutorType(),
				OpenCodeModel: agentMgr.GetOpenCodeModel(),
			}

			// Set up status change callback if manager has persistence
			if agentMgr != nil {
				agent.OnStatusChange = agentMgr.CreateStatusCallback()
			}

			if agentMgr != nil {
				// Start the agent if we can
				if agentMgr.CanStart() {
					if err := agent.Start(context.Background()); err != nil {
						errors = append(errors, fmt.Sprintf("%s: start: %v", task.ID.String(), err))
						// Clean up worktree on failure
						if wtMgr != nil {
							wtMgr.Remove(wtPath)
						}
						continue
					}
				}

				// Add to manager after starting (so PID and LogPath are set)
				agentMgr.Add(agent)
			}

			started = append(started, AgentStartInfo{
				TaskID:       task.ID.String(),
				WorktreePath: wtPath,
			})
		}

		return BatchStartMsg{
			Count:   len(started),
			Started: started,
			Errors:  errors,
		}
	}
}

// min returns the smaller of two integers
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// resumeAgentCmd creates a command to resume an agent
func resumeAgentCmd(agentMgr *executor.AgentManager, taskID string) tea.Cmd {
	return func() tea.Msg {
		if agentMgr == nil {
			return AgentResumeMsg{
				TaskID:  taskID,
				Success: false,
				Error:   "no agent manager",
			}
		}

		agent := agentMgr.Get(taskID)
		if agent == nil {
			return AgentResumeMsg{
				TaskID:  taskID,
				Success: false,
				Error:   "agent not found",
			}
		}

		// Resume the agent
		if err := agent.Resume(context.Background()); err != nil {
			return AgentResumeMsg{
				TaskID:  taskID,
				Success: false,
				Error:   err.Error(),
			}
		}

		return AgentResumeMsg{
			TaskID:  taskID,
			Success: true,
		}
	}
}

// fetchWorkersCmd fetches worker status from the build pool coordinator
func fetchWorkersCmd(buildPoolURL string) tea.Cmd {
	return func() tea.Msg {
		client := &http.Client{Timeout: 2 * time.Second}
		resp, err := client.Get(buildPoolURL + "/status")
		if err != nil {
			// Coordinator not reachable
			return WorkersUpdateMsg{Workers: nil, Status: "unreachable"}
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			return WorkersUpdateMsg{Workers: nil, Status: "unreachable"}
		}

		var status struct {
			Workers []struct {
				ID             string `json:"id"`
				MaxJobs        int    `json:"max_jobs"`
				ActiveJobs     int    `json:"active_jobs"`
				ConnectedSince string `json:"connected_since"`
			} `json:"workers"`
		}

		if err := json.NewDecoder(resp.Body).Decode(&status); err != nil {
			return WorkersUpdateMsg{Workers: nil, Status: "unreachable"}
		}

		workers := make([]*WorkerView, 0, len(status.Workers))
		for _, w := range status.Workers {
			connectedAt, _ := time.Parse(time.RFC3339, w.ConnectedSince)
			workers = append(workers, &WorkerView{
				ID:          w.ID,
				MaxJobs:     w.MaxJobs,
				ActiveJobs:  w.ActiveJobs,
				ConnectedAt: connectedAt,
			})
		}

		return WorkersUpdateMsg{Workers: workers, Status: "connected"}
	}
}

// testWorkerCmd sends a test job to verify worker connectivity
func testWorkerCmd(buildPoolURL, projectRoot string, gitDaemonPort int) tea.Cmd {
	return func() tea.Msg {
		client := &http.Client{Timeout: 150 * time.Second} // Must exceed job timeout (120s)

		// Use configured port, default to 9418 if not set
		port := gitDaemonPort
		if port == 0 {
			port = 9418
		}

		// Extract hostname from buildPoolURL (e.g., "http://host:8081" -> "host")
		// and construct git daemon URL with configured port
		gitURL := ""
		if strings.HasPrefix(buildPoolURL, "http://") || strings.HasPrefix(buildPoolURL, "https://") {
			// Parse the URL to get the hostname
			hostPart := strings.TrimPrefix(buildPoolURL, "http://")
			hostPart = strings.TrimPrefix(hostPart, "https://")
			// Remove port if present
			if idx := strings.Index(hostPart, ":"); idx != -1 {
				hostPart = hostPart[:idx]
			}
			// Remove path if present
			if idx := strings.Index(hostPart, "/"); idx != -1 {
				hostPart = hostPart[:idx]
			}

			// If localhost, try to get a network-accessible address for remote workers
			if hostPart == "localhost" || hostPart == "127.0.0.1" {
				hostPart = getExternalHost()
			}

			gitURL = fmt.Sprintf("git://%s:%d/", hostPart, port)
		}

		// Get current commit hash from local repo
		commit := "HEAD"
		if cmd := exec.Command("git", "-C", projectRoot, "rev-parse", "HEAD"); cmd != nil {
			if out, err := cmd.Output(); err == nil {
				commit = strings.TrimSpace(string(out))
			}
		}

		// Submit a simple test command using the coordinator's git daemon
		jobReq := struct {
			Command string `json:"command"`
			Repo    string `json:"repo"`
			Commit  string `json:"commit"`
			Timeout int    `json:"timeout"`
		}{
			Command: "echo 'hello from worker' && git rev-parse --short HEAD",
			Repo:    gitURL,
			Commit:  commit,
			Timeout: 120, // Allow time for nix develop on first run
		}

		reqBody, _ := json.Marshal(jobReq)
		resp, err := client.Post(buildPoolURL+"/job", "application/json", strings.NewReader(string(reqBody)))
		if err != nil {
			return WorkerTestMsg{Success: false, Error: err.Error()}
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			return WorkerTestMsg{Success: false, Error: fmt.Sprintf("HTTP %d", resp.StatusCode)}
		}

		var jobResp struct {
			JobID    string `json:"job_id"`
			ExitCode int    `json:"exit_code"`
			Output   string `json:"output"`
			Error    string `json:"error"`
		}

		if err := json.NewDecoder(resp.Body).Decode(&jobResp); err != nil {
			return WorkerTestMsg{Success: false, Error: "failed to decode response"}
		}

		if jobResp.ExitCode != 0 {
			return WorkerTestMsg{Success: false, Error: fmt.Sprintf("exit code %d: %s", jobResp.ExitCode, jobResp.Output)}
		}

		return WorkerTestMsg{Success: true, Output: jobResp.Output}
	}
}

// getExternalHost tries to get a network-accessible address for this machine
// Prefers Tailscale IP, falls back to hostname
func getExternalHost() string {
	// Try Tailscale IP first (most reliable for remote access)
	if out, err := exec.Command("tailscale", "ip", "-4").Output(); err == nil {
		if ip := strings.TrimSpace(string(out)); ip != "" {
			return ip
		}
	}

	// Try hostname
	if hostname, err := os.Hostname(); err == nil && hostname != "" {
		return hostname
	}

	// Last resort - return localhost (will likely fail for remote workers)
	return "localhost"
}

// testWorkerErrorCmd sends test commands to verify error handling
// It sends three test commands:
// 1. A command that exits with code 42 (should show "exit code 42")
// 2. A command that writes to stderr (should show stderr output)
// 3. A nonexistent command (should trigger executor error with exit code -1)
func testWorkerErrorCmd(buildPoolURL, projectRoot string) tea.Cmd {
	return func() tea.Msg {
		client := &http.Client{Timeout: 60 * time.Second}

		// Test 1: Command that exits with specific code and stderr
		tests := []struct {
			name    string
			command string
		}{
			{"exit_42", "echo 'stdout line' && echo 'error: test failure' >&2 && exit 42"},
			{"stderr_only", "echo 'error message on stderr' >&2 && exit 1"},
			{"bad_command", "/nonexistent_command_that_does_not_exist_12345"},
		}

		var results []string
		for _, test := range tests {
			jobReq := struct {
				Command string `json:"command"`
				Repo    string `json:"repo"`
				Commit  string `json:"commit"`
				Timeout int    `json:"timeout"`
			}{
				Command: test.command,
				Repo:    "", // Empty repo triggers local execution without git
				Commit:  "",
				Timeout: 30,
			}

			reqBody, _ := json.Marshal(jobReq)
			resp, err := client.Post(buildPoolURL+"/job", "application/json", strings.NewReader(string(reqBody)))
			if err != nil {
				results = append(results, fmt.Sprintf("%s: HTTP error: %s", test.name, err.Error()))
				continue
			}

			var jobResp struct {
				JobID    string `json:"job_id"`
				ExitCode int    `json:"exit_code"`
				Output   string `json:"output"`
				Error    string `json:"error"`
			}

			if err := json.NewDecoder(resp.Body).Decode(&jobResp); err != nil {
				resp.Body.Close()
				results = append(results, fmt.Sprintf("%s: decode error: %s", test.name, err.Error()))
				continue
			}
			resp.Body.Close()

			// Truncate output for display
			output := strings.TrimSpace(jobResp.Output)
			if len(output) > 100 {
				output = output[:100] + "..."
			}
			results = append(results, fmt.Sprintf("%s: exit=%d, output=%q", test.name, jobResp.ExitCode, output))
		}

		return WorkerTestMsg{Success: false, Error: strings.Join(results, " | ")}
	}
}

// testEmbeddedWorkerDirectCmd tests the embedded worker directly without coordinator
// This bypasses HTTP/coordinator to isolate embedded worker behavior
func testEmbeddedWorkerDirectCmd(projectRoot string) tea.Cmd {
	return func() tea.Msg {
		// Create a temporary worktree directory for the embedded worker
		tempDir, err := os.MkdirTemp("", "embedded-test-")
		if err != nil {
			return WorkerTestMsg{Success: false, Error: fmt.Sprintf("failed to create temp dir: %v", err)}
		}
		defer os.RemoveAll(tempDir)

		// Create embedded worker
		worker := buildpool.NewEmbeddedWorker(buildpool.EmbeddedConfig{
			RepoDir:     filepath.Join(tempDir, "repos"),
			WorktreeDir: filepath.Join(tempDir, "worktrees"),
			MaxJobs:     1,
			UseNixShell: false, // Direct shell for faster test
		})

		tests := []struct {
			name    string
			command string
		}{
			{"exit_42", "echo 'stdout line' && echo 'error: test failure' >&2 && exit 42"},
			{"stderr_only", "echo 'error message on stderr' >&2 && exit 1"},
			{"bad_command", "/nonexistent_command_that_does_not_exist_12345"},
		}

		var results []string
		for i, test := range tests {
			job := &buildprotocol.JobMessage{
				JobID:   fmt.Sprintf("test-%d", i),
				Repo:    "", // Empty repo - no git checkout needed
				Commit:  "",
				Command: test.command,
				Timeout: 30,
			}

			result := worker.Run(job)

			// Format result - show all fields to diagnose issue
			output := result.Output
			if len(output) > 80 {
				output = output[:80] + "..."
			}
			stderr := result.Stderr
			if len(stderr) > 80 {
				stderr = stderr[:80] + "..."
			}

			results = append(results, fmt.Sprintf("%s: exit=%d, output=%q, stderr=%q",
				test.name, result.ExitCode, output, stderr))
		}

		return WorkerTestMsg{Success: false, Error: strings.Join(results, " | ")}
	}
}

// startSyncCmd initiates a two-way sync
func startSyncCmd(syncer *isync.Syncer, store *taskstore.Store) tea.Cmd {
	return func() tea.Msg {
		result, err := syncer.TwoWaySync(store)
		return SyncCompleteMsg{Result: result, Err: err}
	}
}

// applyResolutionsCmd applies conflict resolutions
func applyResolutionsCmd(syncer *isync.Syncer, store *taskstore.Store, resolutions map[string]string) tea.Cmd {
	return func() tea.Msg {
		if syncer == nil || store == nil {
			return SyncResolveMsg{Err: fmt.Errorf("syncer or store is nil")}
		}
		err := syncer.ResolveConflicts(store, resolutions)
		return SyncResolveMsg{Err: err}
	}
}

// runAgentTestCmd spawns a Claude agent to test the build pool MCP tools via external coordinator
func runAgentTestCmd(taskID, buildPoolURL, projectRoot, executorType string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()

		// Initialize shared buffer for streaming output
		testAgentMutex.Lock()
		testAgentOutput = nil
		testAgentTaskID = taskID
		testAgentMutex.Unlock()

		// Callback to capture streaming output
		onOutput := func(line string) {
			testAgentMutex.Lock()
			testAgentOutput = append(testAgentOutput, line)
			testAgentMutex.Unlock()
		}

		config := buildpool.TestAgentConfig{
			BuildPoolURL: buildPoolURL,
			ProjectRoot:  projectRoot,
			Verbose:      true,
			ExecutorType: executorType,
		}

		result, err := buildpool.RunTestAgent(ctx, config, onOutput)

		// Clear the task ID when done
		testAgentMutex.Lock()
		testAgentTaskID = ""
		testAgentMutex.Unlock()

		if err != nil {
			return AgentTestMsg{
				TaskID:  taskID,
				Success: false,
				Error:   err.Error(),
			}
		}

		return AgentTestMsg{
			TaskID:  taskID,
			Success: result.Success,
			Output:  result.Output,
			Error:   result.Error,
		}
	}
}

// runAgentTestWithEmbeddedCmd spawns a Claude agent with a temporary embedded coordinator
func runAgentTestWithEmbeddedCmd(taskID, projectRoot, executorType string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()

		// Initialize shared buffer for streaming output
		testAgentMutex.Lock()
		testAgentOutput = nil
		testAgentTaskID = taskID
		testAgentMutex.Unlock()

		// Callback to capture streaming output
		onOutput := func(line string) {
			testAgentMutex.Lock()
			testAgentOutput = append(testAgentOutput, line)
			testAgentMutex.Unlock()
		}

		result, err := buildpool.RunTestAgentWithEmbeddedCoordinator(ctx, projectRoot, true, executorType, onOutput)

		// Clear the task ID when done
		testAgentMutex.Lock()
		testAgentTaskID = ""
		testAgentMutex.Unlock()

		if err != nil {
			return AgentTestMsg{
				TaskID:  taskID,
				Success: false,
				Error:   err.Error(),
			}
		}

		return AgentTestMsg{
			TaskID:  taskID,
			Success: result.Success,
			Output:  result.Output,
			Error:   result.Error,
		}
	}
}

// tryStartAutoTasks checks if we can start more tasks in auto mode
func (m *Model) tryStartAutoTasks() tea.Cmd {
	if !m.autoMode || m.batchPaused {
		return nil
	}

	// Calculate available slots
	slotsAvailable := m.maxActive - m.activeCount
	if slotsAvailable <= 0 || len(m.queued) == 0 {
		return nil
	}

	// Get in-progress task IDs from currently running agents
	inProgress := make(map[string]bool)
	for _, a := range m.agents {
		if a.Status == executor.AgentRunning {
			inProgress[a.TaskID] = true
		}
	}

	// Load group priorities from store if available
	var groupPriorities map[string]int
	if m.store != nil {
		groupPriorities, _ = m.store.GetGroupPriorities()
	}

	// Use scheduler to select tasks that don't conflict with running agents
	var sched *scheduler.Scheduler
	if len(groupPriorities) > 0 {
		sched = scheduler.NewWithPriorities(m.queued, m.completedTasks, groupPriorities)
	} else {
		sched = scheduler.New(m.queued, m.completedTasks)
	}
	readyTasks := sched.GetReadyTasksExcluding(slotsAvailable, inProgress)

	if len(readyTasks) == 0 {
		// No tasks ready - check if we're done
		if len(m.queued) == 0 {
			m.autoMode = false
			m.statusMsg = "Auto mode: all tasks complete!"
		}
		return nil
	}

	// Start the batch
	m.batchRunning = true
	m.batchPaused = false
	m.statusMsg = fmt.Sprintf("Auto: starting %d task(s)...", len(readyTasks))

	return startBatchCmd(
		m.projectRoot,
		readyTasks,
		m.worktreeManager,
		m.agentManager,
		m.planWatcher,
	)
}

// loadAgentHistoryCmd loads completed/failed agent runs from the database
func loadAgentHistoryCmd(store *taskstore.Store) tea.Cmd {
	return func() tea.Msg {
		if store == nil {
			return AgentHistoryMsg{Error: fmt.Errorf("no database configured")}
		}

		runs, err := store.ListRecentAgentRuns(50) // Load last 50 runs
		if err != nil {
			return AgentHistoryMsg{Error: err}
		}

		// Convert to AgentViews
		var history []*AgentView
		for _, run := range runs {
			var duration time.Duration
			if run.FinishedAt != nil {
				duration = run.FinishedAt.Sub(run.StartedAt)
			}
			status := executor.AgentCompleted
			if run.Status == "failed" {
				status = executor.AgentFailed
			}
			history = append(history, &AgentView{
				TaskID:       run.TaskID,
				Title:        run.TaskID, // Use task ID as title since we don't have the task title stored
				Duration:     duration,
				Status:       status,
				WorktreePath: run.WorktreePath,
				LogPath:      run.LogPath,
				Error:        run.ErrorMessage,
				TokensInput:  run.TokensInput,
				TokensOutput: run.TokensOutput,
				CostUSD:      run.CostUSD,
			})
		}

		return AgentHistoryMsg{History: history}
	}
}

// loadHistoryLogsCmd loads log output from a historical agent run's log file
func loadHistoryLogsCmd(logPath string, index int) tea.Cmd {
	return func() tea.Msg {
		if logPath == "" {
			return HistoryLogsMsg{Index: index, Error: fmt.Errorf("no log file path")}
		}

		data, err := os.ReadFile(logPath)
		if err != nil {
			return HistoryLogsMsg{Index: index, Error: fmt.Errorf("failed to read log file: %w", err)}
		}

		lines := strings.Split(string(data), "\n")
		// Limit to last 500 lines to avoid memory issues
		if len(lines) > 500 {
			lines = lines[len(lines)-500:]
		}

		return HistoryLogsMsg{Index: index, Lines: lines}
	}
}

// startMaintenanceTask starts a maintenance task agent
func (m *Model) startMaintenanceTask() tea.Cmd {
	// Capture state needed for the command
	template := m.maintenanceModal.Templates[m.maintenanceModal.Selected]
	scope := m.maintenanceModal.SelectedScope
	targetModule := m.maintenanceModal.TargetModule
	projectRoot := m.projectRoot
	wtMgr := m.worktreeManager
	agentMgr := m.agentManager
	planWatcher := m.planWatcher

	// Close the modal immediately
	m.maintenanceModal.Visible = false
	m.maintenanceModal.Phase = 0
	m.maintenanceModal.Selected = 0
	m.statusMsg = fmt.Sprintf("Starting %s task...", template.Name)

	return func() tea.Msg {
		// Generate a unique task ID for this maintenance run
		// Use the same TaskID throughout to ensure consistency
		taskID := domain.TaskID{Module: "maint", EpicNum: int(time.Now().Unix() % 10000)}
		taskIDStr := taskID.String()
		title := fmt.Sprintf("%s (%s)", template.Name, scope)

		// Create worktree if manager is available
		var wtPath string
		if wtMgr != nil {
			var err error
			wtPath, err = wtMgr.Create(taskID)
			if err != nil {
				return MaintenanceStartMsg{
					TaskID:  taskIDStr,
					Title:   title,
					Success: false,
					Error:   fmt.Sprintf("failed to create worktree: %v", err),
				}
			}
			// Add worktree to plan watcher
			if planWatcher != nil {
				planWatcher.AddWorktree(wtPath)
			}
		} else {
			wtPath = projectRoot
		}

		// Build the prompt
		prompt := executor.BuildMaintenancePrompt(template.Prompt, scope, targetModule)

		// Create and start the agent
		agent := &executor.Agent{
			TaskID:       taskID,
			WorktreePath: wtPath,
			Status:       executor.AgentQueued,
			Prompt:       prompt,
		}

		if agentMgr != nil {
			agent.BuildPoolURL = agentMgr.GetBuildPoolURL()
			agent.ExecutorType = agentMgr.GetExecutorType()
			agent.OpenCodeModel = agentMgr.GetOpenCodeModel()
			agent.OnStatusChange = agentMgr.CreateStatusCallback()

			if agentMgr.CanStart() {
				if err := agent.Start(context.Background()); err != nil {
					// Clean up worktree on failure
					if wtMgr != nil {
						wtMgr.Remove(wtPath)
					}
					return MaintenanceStartMsg{
						TaskID:  taskIDStr,
						Title:   title,
						Success: false,
						Error:   fmt.Sprintf("failed to start agent: %v", err),
					}
				}
			}
			agentMgr.Add(agent)
		}

		return MaintenanceStartMsg{
			TaskID:       taskIDStr,
			Title:        title,
			WorktreePath: wtPath,
			Prompt:       prompt,
			Success:      true,
		}
	}
}

// loadGroupPrioritiesCmd loads group priority data from the store
func loadGroupPrioritiesCmd(store *taskstore.Store) tea.Cmd {
	return func() tea.Msg {
		if store == nil {
			return GroupPrioritiesMsg{Error: fmt.Errorf("no database configured")}
		}

		stats, err := store.GetGroupsWithTaskCounts()
		if err != nil {
			return GroupPrioritiesMsg{Error: err}
		}

		items := make([]GroupPriorityItem, len(stats))
		for i, s := range stats {
			items[i] = GroupPriorityItem{
				Name:      s.Name,
				Priority:  s.Priority,
				Total:     s.Total,
				Completed: s.Completed,
			}
		}

		return GroupPrioritiesMsg{Items: items}
	}
}

// setGroupPriorityCmd sets the priority tier for a group
func setGroupPriorityCmd(store *taskstore.Store, group string, priority int) tea.Cmd {
	return func() tea.Msg {
		if store == nil {
			return SetGroupPriorityMsg{Group: group, Error: fmt.Errorf("no database")}
		}
		err := store.SetGroupPriority(group, priority)
		return SetGroupPriorityMsg{Group: group, Priority: priority, Error: err}
	}
}

// removeGroupPriorityCmd removes a group from the priorities table
func removeGroupPriorityCmd(store *taskstore.Store, group string) tea.Cmd {
	return func() tea.Msg {
		if store == nil {
			return RemoveGroupPriorityMsg{Group: group, Error: fmt.Errorf("no database")}
		}
		err := store.RemoveGroupPriority(group)
		return RemoveGroupPriorityMsg{Group: group, Error: err}
	}
}

// checkUpdateCmd checks for available updates asynchronously
func checkUpdateCmd(currentVersion string) tea.Cmd {
	return func() tea.Msg {
		latest, err := updater.CheckLatestVersion()
		if err != nil {
			return UpdateCheckMsg{Error: err}
		}
		needsUpdate := updater.NeedsUpdate(currentVersion, latest)
		return UpdateCheckMsg{
			LatestVersion: latest,
			NeedsUpdate:   needsUpdate,
		}
	}
}

// selfUpdateCmd downloads and installs the specified version
func selfUpdateCmd(targetVersion string) tea.Cmd {
	return func() tea.Msg {
		err := updater.SelfUpdate(targetVersion)
		if err != nil {
			return UpdateCompleteMsg{Error: err}
		}
		return UpdateCompleteMsg{
			NewVersion: targetVersion,
			Success:    true,
		}
	}
}
