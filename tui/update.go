package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/hochfrequenz/claude-plan-orchestrator/internal/domain"
	"github.com/hochfrequenz/claude-plan-orchestrator/internal/executor"
	"github.com/hochfrequenz/claude-plan-orchestrator/internal/mcp"
	"github.com/hochfrequenz/claude-plan-orchestrator/internal/observer"
	"github.com/hochfrequenz/claude-plan-orchestrator/internal/parser"
	"github.com/hochfrequenz/claude-plan-orchestrator/internal/scheduler"
	isync "github.com/hochfrequenz/claude-plan-orchestrator/internal/sync"
	"github.com/hochfrequenz/claude-plan-orchestrator/internal/taskstore"
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

// SyncCompleteMsg reports sync completion
type SyncCompleteMsg struct {
	Result *isync.SyncResult
	Err    error
}

// SyncResolveMsg reports conflict resolution completion
type SyncResolveMsg struct {
	Err error
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

		switch msg.String() {
		case "q", "ctrl+c":
			return m, tea.Quit
		case "j", "down":
			m.selectedRow++
			if m.activeTab == 1 {
				m.taskScroll++
			}
			if m.activeTab == 2 { // Agents tab
				if m.showAgentDetail {
					// Scroll agent output down
					m.agentOutputScroll++
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
				if m.showAgentDetail {
					// Scroll agent output up
					if m.agentOutputScroll > 0 {
						m.agentOutputScroll--
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
			// Jump to top of agent output
			if m.activeTab == 2 && m.showAgentDetail {
				m.agentOutputScroll = 0
			}
		case "G":
			// Jump to bottom of agent output (handled in view by setting to max)
			if m.activeTab == 2 && m.showAgentDetail {
				m.agentOutputScroll = -1 // Signal to jump to end
			}
		case "enter":
			// Toggle agent detail view (only on Agents tab)
			if m.activeTab == 2 && len(m.agents) > 0 {
				m.showAgentDetail = !m.showAgentDetail
				m.agentOutputScroll = -1 // Start at bottom (most recent)
			}
		case "esc":
			// Close agent detail view
			if m.activeTab == 2 {
				m.showAgentDetail = false
				m.agentOutputScroll = 0
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
		case "tab":
			m.activeTab = (m.activeTab + 1) % 5
			m.selectedRow = 0
			m.taskScroll = 0
		case "t":
			// Toggle to tasks tab
			m.activeTab = 1
			m.taskScroll = 0
		case "m":
			// Toggle to modules tab
			m.activeTab = 3
			m.taskScroll = 0
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

					// Use scheduler to select tasks that don't conflict with running agents
					sched := scheduler.New(m.queued, m.completedTasks)
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
			// Pause/Resume batch (only on Dashboard tab)
			if m.activeTab == 0 {
				if m.batchRunning && !m.batchPaused {
					m.batchPaused = true
					m.statusMsg = "Batch paused"
				} else if m.batchRunning && m.batchPaused {
					m.batchPaused = false
					m.statusMsg = "Batch resumed"
				}
			}
		case "T":
			// Test worker connection (only on Dashboard tab when build pool is connected)
			if m.activeTab == 0 && m.buildPoolURL != "" && m.buildPoolStatus == "connected" {
				m.statusMsg = "Testing worker..."
				return m, testWorkerCmd(m.buildPoolURL, m.projectRoot)
			} else if m.buildPoolStatus != "connected" {
				m.statusMsg = "Build pool not connected"
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
		// Refresh agent status from manager
		if m.agentManager != nil && m.batchRunning {
			m.updateAgentsFromManager()
		}
		// Fetch workers if build pool is configured
		cmds := []tea.Cmd{tickCmd()}
		if m.buildPoolURL != "" {
			cmds = append(cmds, fetchWorkersCmd(m.buildPoolURL))
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
			m.agents = append(m.agents[:completedIdx], m.agents[completedIdx+1:]...)
			// Adjust selected agent index if needed
			if m.selectedAgent >= len(m.agents) && m.selectedAgent > 0 {
				m.selectedAgent--
			}
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
			m.statusMsg = "Batch complete"
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
		for _, info := range msg.Started {
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
		// Remove started tasks from queued
		var remaining []*domain.Task
		for _, t := range m.queued {
			started := false
			for _, info := range msg.Started {
				if t.ID.String() == info.TaskID {
					started = true
					break
				}
			}
			if !started {
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
			// Success - show flash
			total := msg.Result.MarkdownToDBCount + msg.Result.DBToMarkdownCount
			if total > 0 {
				m.syncFlash = fmt.Sprintf("Synced %d task(s) ✓", total)
			} else {
				m.syncFlash = "Already in sync ✓"
			}
			m.syncFlashExp = time.Now().Add(2 * time.Second)
			m.statusMsg = ""
		}
		return m, nil

	case SyncResolveMsg:
		if msg.Err != nil {
			m.statusMsg = fmt.Sprintf("Resolution failed: %v", msg.Err)
		} else {
			m.syncFlash = "Conflicts resolved ✓"
			m.syncFlashExp = time.Now().Add(2 * time.Second)
			m.statusMsg = ""
			// Recompute module summaries
			m.modules = computeModuleSummaries(m.allTasks)
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

	// Check if batch is complete
	if allDone && m.batchRunning && len(m.agents) > 0 {
		m.batchRunning = false
		m.statusMsg = "Batch complete"
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
				TaskID:       task.ID,
				WorktreePath: wtPath,
				EpicFilePath: task.FilePath, // For sync callback to update epic status
				Status:       executor.AgentQueued,
				Prompt:       prompt,
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
func testWorkerCmd(buildPoolURL, projectRoot string) tea.Cmd {
	return func() tea.Msg {
		client := &http.Client{Timeout: 150 * time.Second} // Must exceed job timeout (120s)

		// Extract hostname from buildPoolURL (e.g., "http://host:8081" -> "host")
		// and construct git daemon URL (default port 9418)
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

			gitURL = fmt.Sprintf("git://%s:9418/", hostPart)
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
