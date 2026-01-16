package tui

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/hochfrequenz/claude-plan-orchestrator/internal/domain"
	"github.com/hochfrequenz/claude-plan-orchestrator/internal/executor"
	"github.com/hochfrequenz/claude-plan-orchestrator/internal/scheduler"
	isync "github.com/hochfrequenz/claude-plan-orchestrator/internal/sync"
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

	tabActiveStyle = lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("205")).
		Underline(true)

	tabInactiveStyle = lipgloss.NewStyle().
		Foreground(lipgloss.Color("244"))

	highPrioStyle = lipgloss.NewStyle().
		Foreground(lipgloss.Color("196"))

	normalPrioStyle = lipgloss.NewStyle().
		Foreground(lipgloss.Color("255"))

	lowPrioStyle = lipgloss.NewStyle().
		Foreground(lipgloss.Color("244"))

	moduleHeaderStyle = lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("39"))

	completedStyle = lipgloss.NewStyle().
		Foreground(lipgloss.Color("42"))

	inProgressStyle = lipgloss.NewStyle().
		Foreground(lipgloss.Color("214"))

	dimmedStyle = lipgloss.NewStyle().
		Foreground(lipgloss.Color("240"))

	dimmedWarningStyle = lipgloss.NewStyle().
		Foreground(lipgloss.Color("172"))
)

// View renders the TUI
func (m Model) View() string {
	if m.width == 0 {
		return "Loading..."
	}

	var b strings.Builder

	// Header
	header := fmt.Sprintf(" Claude Plan Orchestrator │ Active: %d/%d │ Tasks: %d │ Completed today: %d │ Flagged: %d ",
		m.activeCount, m.maxActive, len(m.allTasks), m.completedToday, len(m.flagged))
	b.WriteString(headerStyle.Width(m.width).Render(header))
	b.WriteString("\n")

	// Tab bar
	b.WriteString(m.renderTabs())
	b.WriteString("\n")

	// If group priorities view is active, render it instead of normal content
	if m.showGroupPriorities {
		prioritiesSection := m.renderGroupPriorities()
		b.WriteString(sectionStyle.Width(m.width - 2).Render(prioritiesSection))
		b.WriteString("\n")
	} else {
		// Content based on active tab
		switch m.activeTab {
		case 0: // Dashboard
			runningSection := m.renderRunning()
			b.WriteString(sectionStyle.Width(m.width - 2).Render(runningSection))
			b.WriteString("\n")

			workersSection := m.renderWorkers()
			b.WriteString(sectionStyle.Width(m.width - 2).Render(workersSection))
			b.WriteString("\n")

			queuedSection := m.renderQueued()
			b.WriteString(sectionStyle.Width(m.width - 2).Render(queuedSection))
			b.WriteString("\n")

			if len(m.flagged) > 0 {
				attentionSection := m.renderAttention()
				b.WriteString(sectionStyle.Width(m.width - 2).Render(attentionSection))
				b.WriteString("\n")
			}

		case 1: // Tasks
			tasksSection := m.renderTasks()
			b.WriteString(sectionStyle.Width(m.width - 2).Render(tasksSection))
			b.WriteString("\n")

		case 2: // Agents
			agentsSection := m.renderAgentsDetail()
			b.WriteString(sectionStyle.Width(m.width - 2).Render(agentsSection))
			b.WriteString("\n")

		case 3: // Modules
			modulesSection := m.renderModules()
			b.WriteString(sectionStyle.Width(m.width - 2).Render(modulesSection))
			b.WriteString("\n")

		case 4: // PRs
			prsSection := m.renderPRs()
			b.WriteString(sectionStyle.Width(m.width - 2).Render(prsSection))
			b.WriteString("\n")
		}
	}

	// Status message (if any)
	if m.statusMsg != "" {
		statusLine := fmt.Sprintf(" %s ", m.statusMsg)
		if m.autoMode {
			// Auto mode indicator
			statusLine = fmt.Sprintf(" 🔄 AUTO | %s ", m.statusMsg)
		}
		if m.batchRunning {
			if m.batchPaused {
				b.WriteString(warningStyle.Width(m.width).Render("⏸ " + statusLine))
			} else {
				b.WriteString(runningStyle.Width(m.width).Render("▶ " + statusLine))
			}
		} else if m.autoMode {
			b.WriteString(runningStyle.Width(m.width).Render(statusLine))
		} else {
			b.WriteString(queuedStyle.Width(m.width).Render(statusLine))
		}
		b.WriteString("\n")
	} else if m.autoMode {
		// Show auto mode even without status message
		b.WriteString(runningStyle.Width(m.width).Render(" 🔄 AUTO MODE - waiting for tasks... "))
		b.WriteString("\n")
	}

	// Flash message (sync success/error)
	if m.syncFlash != "" && time.Now().Before(m.syncFlashExp) {
		flashStyle := completedStyle
		if strings.HasPrefix(m.syncFlash, "Error") || strings.HasPrefix(m.syncFlash, "Sync failed") {
			flashStyle = warningStyle
		}
		b.WriteString(flashStyle.Width(m.width).Render(fmt.Sprintf(" %s ", m.syncFlash)))
		b.WriteString("\n")
	}

	// Status bar
	var statusBar string

	// Mouse mode indicator
	mouseHint := "[M]ouse"
	if !m.mouseEnabled {
		mouseHint = "[M]ouse:off"
	}

	switch m.activeTab {
	case 1: // Tasks
		viewModeStr := "priority"
		if m.viewMode == ViewByModule {
			viewModeStr = "module"
		}
		statusBar = fmt.Sprintf(" [tab]switch [v]iew mode (%s) [j/k]scroll %s [q]uit ", viewModeStr, mouseHint)
	case 2: // Agents
		if m.showAgentDetail {
			statusBar = fmt.Sprintf(" [j/k]scroll [g]top [G]bottom [esc/enter]back [r]esume %s [q]uit ", mouseHint)
		} else if len(m.agents) > 0 {
			statusBar = fmt.Sprintf(" [tab]switch [j/k]navigate [enter]details [+/-]max agents %s [q]uit ", mouseHint)
		} else {
			statusBar = fmt.Sprintf(" [tab]switch [+/-]max agents %s [q]uit ", mouseHint)
		}
	case 3: // Modules
		statusBar = fmt.Sprintf(" [tab]switch [j/k]scroll [g]roups [m]aint [s]sync [x]run tests %s [q]uit ", mouseHint)
	default:
		testHint := ""
		if m.buildPoolStatus == "connected" {
			testHint = "[T]est worker "
		}
		autoHint := "[a]uto"
		if m.autoMode {
			autoHint = "[a]uto:ON"
		}
		if m.batchRunning && !m.batchPaused {
			statusBar = fmt.Sprintf(" [tab]switch [t]asks [m]odules [g]roups %s[p]ause %s %s [q]uit ", testHint, autoHint, mouseHint)
		} else if m.batchRunning && m.batchPaused {
			statusBar = fmt.Sprintf(" [tab]switch [t]asks [m]odules [g]roups %s[p]resume %s %s [q]uit ", testHint, autoHint, mouseHint)
		} else {
			statusBar = fmt.Sprintf(" [tab]switch [t]asks [m]odules [g]roups %s[s]tart [a]uto %s [q]uit ", testHint, mouseHint)
		}
	}
	b.WriteString(statusBarStyle.Width(m.width).Render(statusBar))

	// Render sync modal overlay if visible
	if m.syncModal.Visible {
		modal := m.renderSyncModal()
		if modal != "" {
			// Center the modal horizontally
			modalLines := strings.Split(modal, "\n")
			var centeredModal strings.Builder
			for _, line := range modalLines {
				// Calculate padding to center
				padding := (m.width - lipgloss.Width(line)) / 2
				if padding < 0 {
					padding = 0
				}
				centeredModal.WriteString(strings.Repeat(" ", padding))
				centeredModal.WriteString(line)
				centeredModal.WriteString("\n")
			}
			// Add vertical padding and overlay
			content := b.String()
			contentLines := strings.Split(content, "\n")
			modalHeight := len(modalLines)
			contentHeight := len(contentLines)

			// Ensure we have enough lines to fit the modal
			minRequiredHeight := modalHeight + 4 // 2 lines top padding + modal + 2 lines bottom
			if contentHeight < minRequiredHeight {
				// Pad content with empty lines
				for i := contentHeight; i < minRequiredHeight; i++ {
					contentLines = append(contentLines, strings.Repeat(" ", m.width))
				}
				contentHeight = len(contentLines)
			}

			// Calculate vertical position (roughly centered)
			topPadding := (contentHeight - modalHeight) / 2
			if topPadding < 2 {
				topPadding = 2
			}

			// Pre-split centered modal for efficiency
			centeredModalLines := strings.Split(centeredModal.String(), "\n")

			// Rebuild content with modal overlay
			var result strings.Builder
			for i, line := range contentLines {
				if i >= topPadding && i < topPadding+modalHeight {
					modalLineIdx := i - topPadding
					if modalLineIdx < len(centeredModalLines) {
						// Use modal line (already centered)
						result.WriteString(strings.TrimRight(centeredModalLines[modalLineIdx], " "))
					} else {
						result.WriteString(line)
					}
				} else {
					result.WriteString(line)
				}
				if i < len(contentLines)-1 {
					result.WriteString("\n")
				}
			}
			return result.String()
		}
	}

	// Render maintenance modal overlay if visible
	if m.maintenanceModal.Visible {
		modal := m.renderMaintenanceModal()
		if modal != "" {
			// Center the modal horizontally
			modalLines := strings.Split(modal, "\n")
			var centeredModal strings.Builder
			for _, line := range modalLines {
				// Calculate padding to center
				padding := (m.width - lipgloss.Width(line)) / 2
				if padding < 0 {
					padding = 0
				}
				centeredModal.WriteString(strings.Repeat(" ", padding))
				centeredModal.WriteString(line)
				centeredModal.WriteString("\n")
			}
			// Add vertical padding and overlay
			content := b.String()
			contentLines := strings.Split(content, "\n")
			modalHeight := len(modalLines)
			contentHeight := len(contentLines)

			// Ensure we have enough lines to fit the modal
			minRequiredHeight := modalHeight + 4
			if contentHeight < minRequiredHeight {
				for i := contentHeight; i < minRequiredHeight; i++ {
					contentLines = append(contentLines, strings.Repeat(" ", m.width))
				}
				contentHeight = len(contentLines)
			}

			// Calculate vertical position (roughly centered)
			topPadding := (contentHeight - modalHeight) / 2
			if topPadding < 2 {
				topPadding = 2
			}

			// Pre-split centered modal for efficiency
			centeredModalLines := strings.Split(centeredModal.String(), "\n")

			// Rebuild content with modal overlay
			var result strings.Builder
			for i, line := range contentLines {
				if i >= topPadding && i < topPadding+modalHeight {
					modalLineIdx := i - topPadding
					if modalLineIdx < len(centeredModalLines) {
						result.WriteString(strings.TrimRight(centeredModalLines[modalLineIdx], " "))
					} else {
						result.WriteString(line)
					}
				} else {
					result.WriteString(line)
				}
				if i < len(contentLines)-1 {
					result.WriteString("\n")
				}
			}
			return result.String()
		}
	}

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

	hasRunning := false
	for _, agent := range m.agents {
		if agent.Status == executor.AgentRunning {
			hasRunning = true
			line := fmt.Sprintf("  ● %-15s %-20s %5s  %s",
				agent.TaskID, truncate(agent.Title, 20),
				formatDuration(agent.Duration), agent.Progress)
			b.WriteString(runningStyle.Render(line))
			b.WriteString("\n")
		}
	}

	// Also show failed agents with errors on dashboard
	for _, agent := range m.agents {
		if agent.Status == executor.AgentFailed {
			errMsg := agent.Error
			if errMsg == "" {
				errMsg = "unknown error"
			}
			line := fmt.Sprintf("  ✗ %-15s %-20s %s",
				agent.TaskID, truncate(agent.Title, 20), truncate(errMsg, 30))
			b.WriteString(warningStyle.Render(line))
			b.WriteString("\n")
		}
	}

	if !hasRunning && len(m.agents) > 0 {
		// Check if all are done
		allDone := true
		for _, a := range m.agents {
			if a.Status == executor.AgentQueued || a.Status == executor.AgentRunning {
				allDone = false
				break
			}
		}
		if allDone {
			b.WriteString(queuedStyle.Render("  All agents completed"))
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

	// Use scheduler to get tasks in priority order, respecting dependencies
	var sched *scheduler.Scheduler
	if len(groupPriorities) > 0 {
		sched = scheduler.NewWithPriorities(m.queued, m.completedTasks, groupPriorities)
	} else {
		sched = scheduler.New(m.queued, m.completedTasks)
	}
	readyTasks := sched.GetReadyTasksExcluding(len(m.queued), inProgress)

	// Build a set of ready task IDs for quick lookup
	readySet := make(map[string]bool)
	for _, t := range readyTasks {
		readySet[t.ID.String()] = true
	}

	// Show ready tasks first (in scheduler priority order), then blocked tasks
	shown := 0
	limit := 5

	// First show ready tasks
	for _, task := range readyTasks {
		if shown >= limit {
			break
		}
		line := fmt.Sprintf("  ● %-15s %-20s %s",
			task.ID.String(), truncate(task.Title, 20), runningStyle.Render("(ready)"))
		b.WriteString(queuedStyle.Render(line))
		b.WriteString("\n")
		shown++
	}

	// Then show blocked tasks if we have room (only from active tier)
	if shown < limit {
		// Determine active tier for filtering blocked tasks
		activeTier := 0
		if len(groupPriorities) > 0 {
			// Find the lowest tier that has incomplete tasks
			tierHasIncomplete := make(map[int]bool)
			for _, task := range m.queued {
				if !m.completedTasks[task.ID.String()] {
					tier := groupPriorities[task.ID.Module] // Defaults to 0
					tierHasIncomplete[tier] = true
				}
			}
			// Find max tier to bound the search
			maxTier := 0
			for tier := range tierHasIncomplete {
				if tier > maxTier {
					maxTier = tier
				}
			}
			for tier := 0; tier <= maxTier; tier++ {
				if tierHasIncomplete[tier] {
					activeTier = tier
					break
				}
			}
		}

		for _, task := range m.queued {
			if shown >= limit {
				break
			}
			if readySet[task.ID.String()] {
				continue // Already shown above
			}
			// Skip tasks not in active tier
			if len(groupPriorities) > 0 {
				taskTier := groupPriorities[task.ID.Module]
				if taskTier > activeTier {
					continue
				}
			}
			// Find what's blocking this task
			blocking := ""
			for _, dep := range task.DependsOn {
				if !m.completedTasks[dep.String()] {
					blocking = dep.String()
					break
				}
			}
			if blocking == "" && inProgress[task.ID.String()] {
				blocking = "in progress"
			}
			if blocking == "" {
				// Blocked by implicit module dependency or in-progress task
				blocking = "dependency"
			}
			line := fmt.Sprintf("  ○ %-15s %-20s %s",
				task.ID.String(), truncate(task.Title, 20), warningStyle.Render(fmt.Sprintf("(waiting: %s)", blocking)))
			b.WriteString(queuedStyle.Render(line))
			b.WriteString("\n")
			shown++
		}
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

func formatTokens(n int) string {
	if n >= 1000000 {
		return fmt.Sprintf("%.1fM", float64(n)/1000000)
	}
	if n >= 1000 {
		return fmt.Sprintf("%.1fK", float64(n)/1000)
	}
	return fmt.Sprintf("%d", n)
}

func (m Model) renderTabs() string {
	tabs := []string{"Dashboard", "Tasks", "Agents", "Modules", "PRs"}
	var parts []string

	for i, tab := range tabs {
		if i == m.activeTab {
			parts = append(parts, tabActiveStyle.Render(fmt.Sprintf(" %s ", tab)))
		} else {
			parts = append(parts, tabInactiveStyle.Render(fmt.Sprintf(" %s ", tab)))
		}
	}

	return strings.Join(parts, "│")
}

func (m Model) renderTasks() string {
	var b strings.Builder

	if m.viewMode == ViewByPriority {
		b.WriteString(titleStyle.Render("TASKS (by priority)"))
	} else {
		b.WriteString(titleStyle.Render("TASKS (by module)"))
	}
	b.WriteString("\n")

	if len(m.allTasks) == 0 {
		b.WriteString(queuedStyle.Render("  No tasks found. Run 'claude-orch sync' to load tasks."))
		return b.String()
	}

	if m.viewMode == ViewByPriority {
		b.WriteString(m.renderTasksByPriority())
	} else {
		b.WriteString(m.renderTasksByModule())
	}

	return strings.TrimSuffix(b.String(), "\n")
}

func (m Model) renderTasksByPriority() string {
	var b strings.Builder

	// Sort tasks by priority (high -> normal -> low), then by module/epic
	tasks := make([]*domain.Task, len(m.allTasks))
	copy(tasks, m.allTasks)
	sort.Slice(tasks, func(i, j int) bool {
		if tasks[i].Priority != tasks[j].Priority {
			return tasks[i].Priority < tasks[j].Priority // Lower enum = higher priority
		}
		if tasks[i].ID.Module != tasks[j].ID.Module {
			return tasks[i].ID.Module < tasks[j].ID.Module
		}
		return tasks[i].ID.EpicNum < tasks[j].ID.EpicNum
	})

	// Calculate visible range
	maxVisible := 15
	start := m.taskScroll
	if start >= len(tasks) {
		start = 0
	}
	end := start + maxVisible
	if end > len(tasks) {
		end = len(tasks)
	}

	for i := start; i < end; i++ {
		task := tasks[i]
		line := m.formatTaskLine(task)
		b.WriteString(line)
		b.WriteString("\n")
	}

	if len(tasks) > maxVisible {
		b.WriteString(queuedStyle.Render(fmt.Sprintf("  ... showing %d-%d of %d (j/k to scroll)", start+1, end, len(tasks))))
		b.WriteString("\n")
	}

	return b.String()
}

func (m Model) renderTasksByModule() string {
	var b strings.Builder

	// Group tasks by module
	modules := make(map[string][]*domain.Task)
	var moduleOrder []string

	for _, task := range m.allTasks {
		mod := task.ID.Module
		if _, exists := modules[mod]; !exists {
			moduleOrder = append(moduleOrder, mod)
		}
		modules[mod] = append(modules[mod], task)
	}

	// Sort module names
	sort.Strings(moduleOrder)

	// Sort tasks within each module by epic number
	for _, tasks := range modules {
		sort.Slice(tasks, func(i, j int) bool {
			return tasks[i].ID.EpicNum < tasks[j].ID.EpicNum
		})
	}

	lineCount := 0
	maxVisible := 15
	start := m.taskScroll

	for _, mod := range moduleOrder {
		tasks := modules[mod]

		// Module header
		if lineCount >= start && lineCount < start+maxVisible {
			b.WriteString(moduleHeaderStyle.Render(fmt.Sprintf("  ┌─ %s (%d tasks)", mod, len(tasks))))
			b.WriteString("\n")
		}
		lineCount++

		for _, task := range tasks {
			if lineCount >= start && lineCount < start+maxVisible {
				line := m.formatTaskLine(task)
				b.WriteString("  │" + line[2:])
				b.WriteString("\n")
			}
			lineCount++
		}
	}

	if lineCount > maxVisible {
		b.WriteString(queuedStyle.Render(fmt.Sprintf("  ... showing %d lines (j/k to scroll)", maxVisible)))
		b.WriteString("\n")
	}

	return b.String()
}

func (m Model) formatTaskLine(task *domain.Task) string {
	// Status icon
	var statusIcon string
	var style lipgloss.Style
	switch task.Status {
	case domain.StatusComplete:
		statusIcon = "✓"
		style = completedStyle
	case domain.StatusInProgress:
		statusIcon = "●"
		style = inProgressStyle
	default:
		statusIcon = "○"
		switch task.Priority {
		case domain.PriorityHigh:
			style = highPrioStyle
		case domain.PriorityLow:
			style = lowPrioStyle
		default:
			style = normalPrioStyle
		}
	}

	// Priority indicator
	var prioStr string
	switch task.Priority {
	case domain.PriorityHigh:
		prioStr = "!"
	case domain.PriorityLow:
		prioStr = "-"
	default:
		prioStr = " "
	}

	// Issue indicator
	var issueStr string
	if task.GitHubIssue != nil {
		issueStr = fmt.Sprintf(" #%d", *task.GitHubIssue)
	}

	line := fmt.Sprintf("  %s %s %-15s%-4s %-30s",
		statusIcon, prioStr, task.ID.String(), issueStr, truncate(task.Title, 30))

	return style.Render(line)
}

func (m Model) renderAgentsDetail() string {
	var b strings.Builder

	// If showing history detail, render that instead
	if m.showHistoryDetail && len(m.agentHistory) > 0 && m.selectedHistory < len(m.agentHistory) {
		return m.renderSelectedHistoryDetail()
	}

	// If showing agent detail, render that instead
	if m.showAgentDetail && len(m.agents) > 0 && m.selectedAgent < len(m.agents) {
		return m.renderSelectedAgentDetail()
	}

	b.WriteString(titleStyle.Render("AGENTS"))
	b.WriteString("\n\n")

	// Configuration section
	configLine := fmt.Sprintf("  Max Parallel Agents: %d", m.maxActive)
	if m.configChanged {
		configLine += " (modified)"
	}
	b.WriteString(moduleHeaderStyle.Render(configLine))
	b.WriteString("\n")
	b.WriteString(queuedStyle.Render("  Press [+] to increase, [-] to decrease (1-10)"))
	b.WriteString("\n\n")

	// Active agents section
	b.WriteString(titleStyle.Render(fmt.Sprintf("AGENTS (%d/%d running)", m.activeCount, m.maxActive)))
	b.WriteString("\n")

	if len(m.agents) == 0 {
		b.WriteString(queuedStyle.Render("  No agents. Press [s] on Dashboard to start a batch."))
		b.WriteString("\n")
	} else {
		for i, agent := range m.agents {
			var statusIcon string
			var style lipgloss.Style
			switch agent.Status {
			case executor.AgentRunning:
				statusIcon = "●"
				style = runningStyle
			case executor.AgentCompleted:
				statusIcon = "✓"
				style = completedStyle
			case executor.AgentFailed:
				statusIcon = "✗"
				style = warningStyle
			case executor.AgentStuck:
				statusIcon = "⚠"
				style = warningStyle
			default:
				statusIcon = "○"
				style = queuedStyle
			}

			// Show error preview for failed agents
			extra := agent.Progress
			if agent.Status == executor.AgentFailed && agent.Error != "" {
				extra = truncate(agent.Error, 25)
			}

			line := fmt.Sprintf("  %s %-15s %-20s %8s  %s",
				statusIcon, agent.TaskID, truncate(agent.Title, 20),
				formatDuration(agent.Duration), extra)

			// Highlight selected agent
			if i == m.selectedAgent {
				line = fmt.Sprintf("> %s", line[2:])
				b.WriteString(tabActiveStyle.Render(line))
			} else {
				b.WriteString(style.Render(line))
			}
			b.WriteString("\n")
		}
	}

	// Slots available
	slotsAvailable := m.maxActive - m.activeCount
	if slotsAvailable > 0 && len(m.agents) > 0 {
		b.WriteString("\n")
		b.WriteString(queuedStyle.Render(fmt.Sprintf("  %d slot(s) available for new agents", slotsAvailable)))
	}

	if len(m.agents) > 0 {
		b.WriteString("\n")
		b.WriteString(queuedStyle.Render("  Press [enter] to view agent details, [j/k] to navigate"))
	}

	// History section (toggle with 'h')
	b.WriteString("\n\n")
	if m.showAgentHistory {
		b.WriteString(titleStyle.Render(fmt.Sprintf("HISTORY (%d recent runs)", len(m.agentHistory))))
		b.WriteString("\n")
		if len(m.agentHistory) == 0 {
			b.WriteString(queuedStyle.Render("  No completed runs found."))
			b.WriteString("\n")
		} else {
			for i, agent := range m.agentHistory {
				var statusIcon string
				var style lipgloss.Style
				switch agent.Status {
				case executor.AgentCompleted:
					statusIcon = "✓"
					style = dimmedStyle
				case executor.AgentFailed:
					statusIcon = "✗"
					style = dimmedWarningStyle
				default:
					statusIcon = "○"
					style = dimmedStyle
				}

				// Show error preview for failed agents or duration
				extra := formatDuration(agent.Duration)
				if agent.Status == executor.AgentFailed && agent.Error != "" {
					extra = truncate(agent.Error, 25)
				}

				// Show cost if available
				costStr := ""
				if agent.CostUSD > 0 {
					costStr = fmt.Sprintf(" $%.2f", agent.CostUSD)
				}

				line := fmt.Sprintf("  %s %-15s %8s%s  %s",
					statusIcon, agent.TaskID,
					formatDuration(agent.Duration), costStr, extra)

				// Highlight selected history item
				if i == m.selectedHistory {
					line = fmt.Sprintf("> %s", line[2:])
					b.WriteString(tabActiveStyle.Render(line))
				} else {
					b.WriteString(style.Render(line))
				}
				b.WriteString("\n")
			}
		}
		b.WriteString(queuedStyle.Render("  [j/k]navigate [enter]view logs [h]hide history"))
	} else {
		b.WriteString(queuedStyle.Render("  Press [h] to show completed/failed run history"))
	}

	return strings.TrimSuffix(b.String(), "\n")
}

func (m Model) renderSelectedAgentDetail() string {
	var b strings.Builder
	agent := m.agents[m.selectedAgent]

	b.WriteString(titleStyle.Render(fmt.Sprintf("AGENT DETAIL: %s", agent.TaskID)))
	b.WriteString("\n\n")

	// Status
	var statusStr string
	var style lipgloss.Style
	switch agent.Status {
	case executor.AgentRunning:
		statusStr = "Running"
		style = runningStyle
	case executor.AgentCompleted:
		statusStr = "Completed"
		style = completedStyle
	case executor.AgentFailed:
		statusStr = "Failed"
		style = warningStyle
	case executor.AgentStuck:
		statusStr = "Stuck"
		style = warningStyle
	case executor.AgentQueued:
		statusStr = "Queued"
		style = queuedStyle
	default:
		statusStr = string(agent.Status)
		style = queuedStyle
	}

	b.WriteString(fmt.Sprintf("  Status:   %s\n", style.Render(statusStr)))
	b.WriteString(fmt.Sprintf("  Task:     %s\n", agent.Title))
	b.WriteString(fmt.Sprintf("  Duration: %s\n", formatDuration(agent.Duration)))
	if agent.WorktreePath != "" {
		b.WriteString(fmt.Sprintf("  Worktree: %s\n", agent.WorktreePath))
	}

	// Show token usage if available
	if agent.TokensInput > 0 || agent.TokensOutput > 0 {
		b.WriteString(fmt.Sprintf("  Tokens:   %s in / %s out",
			formatTokens(agent.TokensInput), formatTokens(agent.TokensOutput)))
		if agent.CostUSD > 0 {
			b.WriteString(fmt.Sprintf(" ($%.4f)", agent.CostUSD))
		}
		b.WriteString("\n")
	}

	// Error section
	if agent.Error != "" {
		b.WriteString("\n")
		b.WriteString(warningStyle.Render("  ERROR:"))
		b.WriteString("\n")
		b.WriteString(warningStyle.Render(fmt.Sprintf("  %s", agent.Error)))
		b.WriteString("\n")
	}

	// Content section with scrolling (either prompt or output)
	b.WriteString("\n")

	// Calculate visible window
	maxLines := 20
	if m.height > 20 {
		maxLines = m.height - 15 // Leave room for header and footer
	}

	maxWidth := 80
	if m.width > 10 {
		maxWidth = m.width - 10
	}

	if m.showAgentPrompt {
		// Show prompt
		var promptLines []string
		if agent.Prompt != "" {
			// Word-wrap the prompt
			for _, line := range strings.Split(agent.Prompt, "\n") {
				if len(line) <= maxWidth {
					promptLines = append(promptLines, line)
				} else {
					// Wrap long lines
					for len(line) > maxWidth {
						promptLines = append(promptLines, line[:maxWidth])
						line = line[maxWidth:]
					}
					if line != "" {
						promptLines = append(promptLines, line)
					}
				}
			}
		} else {
			promptLines = []string{"(No prompt captured)"}
		}

		totalLines := len(promptLines)
		scroll := m.agentOutputScroll

		// Handle -1 as "jump to end"
		if scroll < 0 || scroll > totalLines-maxLines {
			scroll = totalLines - maxLines
			if scroll < 0 {
				scroll = 0
			}
		}

		end := scroll + maxLines
		if end > totalLines {
			end = totalLines
		}

		// Header with scroll position
		scrollInfo := ""
		if totalLines > maxLines {
			scrollInfo = fmt.Sprintf(" [%d-%d of %d]", scroll+1, end, totalLines)
		}
		b.WriteString(titleStyle.Render(fmt.Sprintf("  PROMPT%s:", scrollInfo)))
		b.WriteString("\n")

		// Show scroll indicator at top
		if scroll > 0 {
			b.WriteString(queuedStyle.Render("  ↑ (more above)"))
			b.WriteString("\n")
		}

		// Show visible lines
		for i := scroll; i < end; i++ {
			b.WriteString(queuedStyle.Render(fmt.Sprintf("  %s", promptLines[i])))
			b.WriteString("\n")
		}

		// Show scroll indicator at bottom
		if end < totalLines {
			b.WriteString(queuedStyle.Render(fmt.Sprintf("  ↓ (%d more below)", totalLines-end)))
			b.WriteString("\n")
		}
	} else if len(agent.Output) > 0 {
		// Show output
		// Format the JSON output into readable lines
		formattedLines := formatClaudeOutput(agent.Output, maxWidth)

		totalLines := len(formattedLines)
		scroll := m.agentOutputScroll

		// Handle -1 as "jump to end"
		if scroll < 0 || scroll > totalLines-maxLines {
			scroll = totalLines - maxLines
			if scroll < 0 {
				scroll = 0
			}
		}

		end := scroll + maxLines
		if end > totalLines {
			end = totalLines
		}

		// Header with scroll position
		scrollInfo := ""
		if totalLines > maxLines {
			scrollInfo = fmt.Sprintf(" [%d-%d of %d]", scroll+1, end, totalLines)
		}
		b.WriteString(titleStyle.Render(fmt.Sprintf("  OUTPUT%s:", scrollInfo)))
		b.WriteString("\n")

		// Show scroll indicator at top
		if scroll > 0 {
			b.WriteString(queuedStyle.Render("  ↑ (more above)"))
			b.WriteString("\n")
		}

		// Show visible lines
		for i := scroll; i < end; i++ {
			b.WriteString(queuedStyle.Render(fmt.Sprintf("  %s", formattedLines[i])))
			b.WriteString("\n")
		}

		// Show scroll indicator at bottom
		if end < totalLines {
			b.WriteString(queuedStyle.Render(fmt.Sprintf("  ↓ (%d more below)", totalLines-end)))
			b.WriteString("\n")
		}
	} else if agent.Status == executor.AgentFailed {
		b.WriteString(queuedStyle.Render("  No output captured"))
		b.WriteString("\n")
	}

	b.WriteString("\n")
	promptHint := "[p]rompt"
	if m.showAgentPrompt {
		promptHint = "[p]output"
	}
	b.WriteString(queuedStyle.Render(fmt.Sprintf("  [j/k]scroll [g]top [G]bottom %s [esc]back", promptHint)))

	return strings.TrimSuffix(b.String(), "\n")
}

func (m Model) renderSelectedHistoryDetail() string {
	var b strings.Builder
	agent := m.agentHistory[m.selectedHistory]

	b.WriteString(titleStyle.Render(fmt.Sprintf("HISTORY DETAIL: %s", agent.TaskID)))
	b.WriteString("\n\n")

	// Status
	var statusStr string
	var style lipgloss.Style
	switch agent.Status {
	case executor.AgentCompleted:
		statusStr = "Completed"
		style = completedStyle
	case executor.AgentFailed:
		statusStr = "Failed"
		style = warningStyle
	default:
		statusStr = string(agent.Status)
		style = queuedStyle
	}

	b.WriteString(fmt.Sprintf("  Status:   %s\n", style.Render(statusStr)))
	b.WriteString(fmt.Sprintf("  Task:     %s\n", agent.Title))
	b.WriteString(fmt.Sprintf("  Duration: %s\n", formatDuration(agent.Duration)))
	if agent.WorktreePath != "" {
		b.WriteString(fmt.Sprintf("  Worktree: %s\n", agent.WorktreePath))
	}
	if agent.LogPath != "" {
		b.WriteString(fmt.Sprintf("  Log:      %s\n", agent.LogPath))
	}

	// Show token usage if available
	if agent.TokensInput > 0 || agent.TokensOutput > 0 {
		b.WriteString(fmt.Sprintf("  Tokens:   %s in / %s out",
			formatTokens(agent.TokensInput), formatTokens(agent.TokensOutput)))
		if agent.CostUSD > 0 {
			b.WriteString(fmt.Sprintf(" ($%.4f)", agent.CostUSD))
		}
		b.WriteString("\n")
	}

	// Error section
	if agent.Error != "" {
		b.WriteString("\n")
		b.WriteString(warningStyle.Render("  ERROR:"))
		b.WriteString("\n")
		b.WriteString(warningStyle.Render(fmt.Sprintf("  %s", agent.Error)))
		b.WriteString("\n")
	}

	// Log output section with scrolling
	b.WriteString("\n")

	// Calculate visible window
	maxLines := 20
	if m.height > 20 {
		maxLines = m.height - 15 // Leave room for header and footer
	}

	maxWidth := 80
	if m.width > 10 {
		maxWidth = m.width - 10
	}

	if len(agent.Output) > 0 {
		// Format the JSON output into readable lines
		formattedLines := formatClaudeOutput(agent.Output, maxWidth)

		totalLines := len(formattedLines)
		scroll := m.agentOutputScroll

		// Handle -1 as "jump to end"
		if scroll < 0 || scroll > totalLines-maxLines {
			scroll = totalLines - maxLines
			if scroll < 0 {
				scroll = 0
			}
		}

		end := scroll + maxLines
		if end > totalLines {
			end = totalLines
		}

		// Header with scroll position
		scrollInfo := ""
		if totalLines > maxLines {
			scrollInfo = fmt.Sprintf(" [%d-%d of %d]", scroll+1, end, totalLines)
		}
		b.WriteString(titleStyle.Render(fmt.Sprintf("  LOG OUTPUT%s:", scrollInfo)))
		b.WriteString("\n")

		// Show scroll indicator at top
		if scroll > 0 {
			b.WriteString(queuedStyle.Render("  ↑ (more above)"))
			b.WriteString("\n")
		}

		// Show visible lines
		for i := scroll; i < end; i++ {
			b.WriteString(queuedStyle.Render(fmt.Sprintf("  %s", formattedLines[i])))
			b.WriteString("\n")
		}

		// Show scroll indicator at bottom
		if end < totalLines {
			b.WriteString(queuedStyle.Render(fmt.Sprintf("  ↓ (%d more below)", totalLines-end)))
			b.WriteString("\n")
		}
	} else {
		b.WriteString(queuedStyle.Render("  No log output available"))
		b.WriteString("\n")
		if agent.LogPath != "" {
			b.WriteString(queuedStyle.Render("  Loading logs..."))
			b.WriteString("\n")
		}
	}

	b.WriteString("\n")
	b.WriteString(queuedStyle.Render("  [j/k]scroll [g]top [G]bottom [esc]back"))

	return strings.TrimSuffix(b.String(), "\n")
}

func (m Model) renderPRs() string {
	var b strings.Builder
	b.WriteString(titleStyle.Render("PULL REQUESTS"))
	b.WriteString("\n")

	if len(m.flagged) == 0 {
		b.WriteString(queuedStyle.Render("  No PRs needing attention"))
		return b.String()
	}

	for _, pr := range m.flagged {
		line := fmt.Sprintf("  ⚠ %-15s PR #%-5d %s",
			pr.TaskID, pr.PRNumber, pr.Reason)
		b.WriteString(warningStyle.Render(line))
		b.WriteString("\n")
	}

	return strings.TrimSuffix(b.String(), "\n")
}

// claudeStreamMessage represents a message from Claude's stream-json output
type claudeStreamMessage struct {
	Type    string `json:"type"`
	Subtype string `json:"subtype,omitempty"`
	Message struct {
		Content []struct {
			Type      string `json:"type"`
			Text      string `json:"text,omitempty"`
			Name      string `json:"name,omitempty"`  // tool name
			ToolInput any    `json:"input,omitempty"` // tool input
			ToolUseID string `json:"tool_use_id,omitempty"`
			Content   any    `json:"content,omitempty"` // tool result content (can be string or array)
		} `json:"content,omitempty"`
	} `json:"message,omitempty"`
}

// Track last tool used for better result display
var lastToolUsed string
var lastToolDetail string

// extractToolResultInfo tries to extract useful info from tool result content
func extractToolResultInfo(content any, maxLen int) string {
	if maxLen < 10 {
		maxLen = 10
	}

	// Handle array of content items (MCP tools return this format)
	if arr, ok := content.([]any); ok {
		for _, item := range arr {
			if obj, ok := item.(map[string]any); ok {
				if text, ok := obj["text"].(string); ok {
					return parseToolResultText(text, maxLen)
				}
			}
		}
	}

	// Handle string content
	if str, ok := content.(string); ok {
		return parseToolResultText(str, maxLen)
	}

	return ""
}

// parseToolResultText extracts useful info from tool result text (often JSON)
func parseToolResultText(text string, maxLen int) string {
	// Try to parse as JSON and extract useful fields
	var data map[string]any
	if err := json.Unmarshal([]byte(text), &data); err == nil {
		// Look for common useful fields
		if msg, ok := data["message"].(string); ok {
			return truncate(msg, maxLen)
		}
		if status, ok := data["status"].(string); ok {
			return truncate(status, maxLen)
		}
		if summary, ok := data["summary"].(string); ok {
			return truncate(summary, maxLen)
		}
		// For test results, show pass/fail counts
		if passed, ok := data["passed"].(float64); ok {
			if failed, ok := data["failed"].(float64); ok {
				return fmt.Sprintf("passed: %.0f, failed: %.0f", passed, failed)
			}
		}
		if total, ok := data["total"].(float64); ok {
			return fmt.Sprintf("total: %.0f", total)
		}
	}

	// Not JSON or no useful fields found, return truncated text
	// Skip if it looks like raw JSON object
	if strings.HasPrefix(strings.TrimSpace(text), "{") {
		return ""
	}
	return truncate(strings.TrimSpace(text), maxLen)
}

// formatClaudeOutput parses JSON stream lines and formats them for display
func formatClaudeOutput(lines []string, maxWidth int) []string {
	var result []string

	for _, line := range lines {
		if line == "" {
			continue
		}

		var msg claudeStreamMessage
		if err := json.Unmarshal([]byte(line), &msg); err != nil {
			// Not valid JSON, show truncated raw line
			if len(line) > maxWidth-4 {
				line = line[:maxWidth-7] + "..."
			}
			result = append(result, line)
			continue
		}

		switch msg.Type {
		case "system":
			// Skip most system messages, just show init
			if msg.Subtype == "init" {
				result = append(result, "▶ Session started")
			}

		case "assistant":
			// Extract text content and tool uses
			for _, content := range msg.Message.Content {
				switch content.Type {
				case "text":
					// Split text into lines and add each
					textLines := strings.Split(content.Text, "\n")
					for _, tl := range textLines {
						tl = strings.TrimSpace(tl)
						if tl == "" {
							continue
						}
						if len(tl) > maxWidth-4 {
							tl = tl[:maxWidth-7] + "..."
						}
						result = append(result, tl)
					}
				case "tool_use":
					// Show tool being used and remember it
					lastToolUsed = content.Name
					lastToolDetail = ""
					toolName := content.Name

					// Format MCP tool names nicely: mcp__server__tool -> server: tool
					if strings.HasPrefix(toolName, "mcp__") {
						parts := strings.SplitN(toolName[5:], "__", 2)
						if len(parts) == 2 {
							toolName = fmt.Sprintf("%s: %s", parts[0], parts[1])
						}
					}

					toolLine := fmt.Sprintf("🔧 %s", toolName)

					// Extract file path from tool input for file operations
					if input, ok := content.ToolInput.(map[string]any); ok {
						if filePath, ok := input["file_path"].(string); ok {
							// Show just the filename for brevity
							parts := strings.Split(filePath, "/")
							fileName := parts[len(parts)-1]
							lastToolDetail = fileName
							toolLine = fmt.Sprintf("🔧 %s: %s", toolName, fileName)
						} else if pattern, ok := input["pattern"].(string); ok {
							// For Glob/Grep show the pattern
							lastToolDetail = truncate(pattern, 20)
							toolLine = fmt.Sprintf("🔧 %s: %s", toolName, truncate(pattern, 30))
						} else if cmd, ok := input["command"].(string); ok {
							// For Bash show truncated command
							lastToolDetail = truncate(cmd, 20)
							toolLine = fmt.Sprintf("🔧 %s: %s", toolName, truncate(cmd, 30))
						}
					}

					// Store formatted name for result display
					lastToolUsed = toolName
					result = append(result, toolLine)
				}
			}

		case "user":
			// Tool results - show which tool completed
			for _, content := range msg.Message.Content {
				if content.Type == "tool_result" {
					resultLine := ""
					if lastToolUsed != "" {
						if lastToolDetail != "" {
							resultLine = fmt.Sprintf("   ✓ %s: %s", lastToolUsed, lastToolDetail)
						} else {
							resultLine = fmt.Sprintf("   ✓ %s", lastToolUsed)
						}
					}

					// Try to extract useful info from MCP tool results
					if resultLine != "" && content.Content != nil {
						extraInfo := extractToolResultInfo(content.Content, maxWidth-len(resultLine)-3)
						if extraInfo != "" {
							resultLine += " → " + extraInfo
						}
					}

					if resultLine != "" {
						result = append(result, resultLine)
					}
				}
			}
		}
	}

	return result
}

func (m Model) renderModules() string {
	var b strings.Builder
	b.WriteString(titleStyle.Render("MODULES"))
	b.WriteString("\n\n")

	if len(m.modules) == 0 {
		b.WriteString(queuedStyle.Render("  No modules found. Run 'claude-orch sync' to load tasks."))
		return b.String()
	}

	// Show test output if running or has output
	if m.testRunning {
		b.WriteString(inProgressStyle.Render("  ⏳ Running tests..."))
		b.WriteString("\n\n")
	} else if m.testOutput != "" {
		b.WriteString(queuedStyle.Render("  Last test output:"))
		b.WriteString("\n")
		// Show last few lines of test output
		lines := strings.Split(m.testOutput, "\n")
		start := 0
		if len(lines) > 5 {
			start = len(lines) - 5
		}
		for _, line := range lines[start:] {
			if line != "" {
				b.WriteString(queuedStyle.Render("  " + line))
				b.WriteString("\n")
			}
		}
		b.WriteString("\n")
	}

	// Header row
	header := fmt.Sprintf("  %-20s %8s %8s %8s %8s %8s %8s",
		"Module", "Epics", "Done", "InProg", "Tests", "Passed", "Failed")
	b.WriteString(headerStyle.Render(header))
	b.WriteString("\n")

	// Calculate visible range for scrolling
	maxVisible := 12
	start := m.taskScroll
	if start >= len(m.modules) {
		start = 0
	}
	end := start + maxVisible
	if end > len(m.modules) {
		end = len(m.modules)
	}

	for i := start; i < end; i++ {
		mod := m.modules[i]
		selected := i == m.selectedModule

		// Status indicator
		var statusIcon string
		var style lipgloss.Style
		if mod.CompletedEpics == mod.TotalEpics && mod.TotalEpics > 0 {
			statusIcon = "✓"
			style = completedStyle
		} else if mod.InProgressEpics > 0 {
			statusIcon = "●"
			style = inProgressStyle
		} else {
			statusIcon = "○"
			style = normalPrioStyle
		}

		// Format line
		line := fmt.Sprintf("  %s %-18s %8d %8d %8d %8d %8d %8d",
			statusIcon,
			truncate(mod.Name, 18),
			mod.TotalEpics,
			mod.CompletedEpics,
			mod.InProgressEpics,
			mod.TotalTests,
			mod.PassedTests,
			mod.FailedTests,
		)

		// Highlight selected row
		if selected {
			line = fmt.Sprintf("> %s", line[2:])
			b.WriteString(tabActiveStyle.Render(line))
		} else {
			b.WriteString(style.Render(line))
		}
		b.WriteString("\n")
	}

	if len(m.modules) > maxVisible {
		b.WriteString(queuedStyle.Render(fmt.Sprintf("  ... showing %d-%d of %d modules (j/k to scroll)", start+1, end, len(m.modules))))
		b.WriteString("\n")
	}

	// Show coverage summary if available
	b.WriteString("\n")
	var totalTests, totalPassed, totalFailed int
	for _, mod := range m.modules {
		totalTests += mod.TotalTests
		totalPassed += mod.PassedTests
		totalFailed += mod.FailedTests
	}
	if totalTests > 0 {
		passRate := float64(totalPassed) / float64(totalTests) * 100
		summary := fmt.Sprintf("  Total: %d tests, %d passed, %d failed (%.0f%% pass rate)",
			totalTests, totalPassed, totalFailed, passRate)
		if totalFailed > 0 {
			b.WriteString(warningStyle.Render(summary))
		} else {
			b.WriteString(completedStyle.Render(summary))
		}
	}

	return strings.TrimSuffix(b.String(), "\n")
}

func (m Model) renderWorkers() string {
	var b strings.Builder
	b.WriteString(titleStyle.Render("BUILD POOL"))
	b.WriteString("\n")

	switch m.buildPoolStatus {
	case "disabled":
		b.WriteString(queuedStyle.Render("  Not enabled (set build_pool.enabled = true)"))
	case "unreachable":
		b.WriteString(queuedStyle.Render("  Coordinator not running"))
		b.WriteString("\n")
		b.WriteString(queuedStyle.Render("  Run: claude-orch build-pool start"))
	case "connected":
		if len(m.workers) == 0 {
			b.WriteString(queuedStyle.Render("  Coordinator running, no workers connected"))
			b.WriteString("\n")
			b.WriteString(queuedStyle.Render("  Using local fallback for builds"))
		} else {
			b.WriteString(queuedStyle.Render(fmt.Sprintf("  Coordinator running, %d worker(s):", len(m.workers))))
			b.WriteString("\n")
			for _, w := range m.workers {
				b.WriteString(queuedStyle.Render(fmt.Sprintf("    %s: %d/%d jobs",
					w.ID, w.ActiveJobs, w.MaxJobs)))
				b.WriteString("\n")
			}
			return strings.TrimSuffix(b.String(), "\n")
		}
	}
	return b.String()
}

// Modal styles
var (
	modalStyle = lipgloss.NewStyle().
		Border(lipgloss.DoubleBorder()).
		BorderForeground(lipgloss.Color("205")).
		Padding(1, 2).
		Background(lipgloss.Color("235"))

	modalTitleStyle = lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("205"))

	modalSelectedStyle = lipgloss.NewStyle().
		Background(lipgloss.Color("240")).
		Foreground(lipgloss.Color("255"))

	resolvedDBStyle = lipgloss.NewStyle().
		Foreground(lipgloss.Color("42")).
		Bold(true)

	resolvedMarkdownStyle = lipgloss.NewStyle().
		Foreground(lipgloss.Color("214")).
		Bold(true)
)

// renderSyncModal renders the sync conflict resolution modal
func (m Model) renderSyncModal() string {
	if !m.syncModal.Visible || len(m.syncModal.Conflicts) == 0 {
		return ""
	}

	var b strings.Builder

	// Title
	b.WriteString(modalTitleStyle.Render("SYNC CONFLICTS"))
	b.WriteString("\n\n")

	// Description
	b.WriteString(queuedStyle.Render("Database and markdown files have different status values."))
	b.WriteString("\n")
	b.WriteString(queuedStyle.Render("Choose which source should be authoritative:"))
	b.WriteString("\n\n")

	// Instructions
	b.WriteString(runningStyle.Render("[d]"))
	b.WriteString(queuedStyle.Render(" use DB status  "))
	b.WriteString(warningStyle.Render("[m]"))
	b.WriteString(queuedStyle.Render(" use Markdown status  "))
	b.WriteString(runningStyle.Render("[a]"))
	b.WriteString(queuedStyle.Render(" all use DB"))
	b.WriteString("\n")
	b.WriteString(queuedStyle.Render("[j/k] navigate  [enter] apply  [esc] cancel"))
	b.WriteString("\n\n")

	// Count resolved
	resolvedCount := 0
	for _, c := range m.syncModal.Conflicts {
		if m.syncModal.Resolutions[c.TaskID] != "" {
			resolvedCount++
		}
	}
	b.WriteString(queuedStyle.Render(fmt.Sprintf("Progress: %d/%d resolved", resolvedCount, len(m.syncModal.Conflicts))))
	b.WriteString("\n\n")

	// Header
	b.WriteString(headerStyle.Render(fmt.Sprintf("%-14s  %-20s  %-20s  %s", "Task", "Database", "Markdown", "Action")))
	b.WriteString("\n")

	// Conflicts list
	for i, conflict := range m.syncModal.Conflicts {
		selected := i == m.syncModal.Selected
		resolution := m.syncModal.Resolutions[conflict.TaskID]

		// Format the conflict line with details
		line := renderConflictLine(conflict, resolution, selected)
		b.WriteString(line)
		b.WriteString("\n")

		// Show file path for selected item
		if selected && conflict.EpicFilePath != "" {
			filePath := conflict.EpicFilePath
			// Truncate path if too long, showing end
			if len(filePath) > 50 {
				filePath = "..." + filePath[len(filePath)-47:]
			}
			b.WriteString(queuedStyle.Render(fmt.Sprintf("  └─ %s", filePath)))
			b.WriteString("\n")
		}
	}

	// Wrap in modal style
	modalContent := b.String()

	// Calculate modal dimensions - wider to accommodate detail
	modalWidth := 75
	if m.width > 85 {
		modalWidth = 75
	} else if m.width > 60 {
		modalWidth = m.width - 10
	} else {
		modalWidth = m.width - 4
	}

	return modalStyle.Width(modalWidth).Render(modalContent)
}

// renderConflictLine renders a single conflict with its resolution status
func renderConflictLine(conflict isync.SyncConflict, resolution string, selected bool) string {
	// Get emoji representations for statuses
	dbEmoji := isync.StatusEmoji(domain.TaskStatus(conflict.DBStatus))
	mdEmoji := isync.StatusEmoji(domain.TaskStatus(conflict.MarkdownStatus))

	// Format status with emoji: "🔴 not_started"
	dbDisplay := fmt.Sprintf("%s %-11s", dbEmoji, truncate(conflict.DBStatus, 11))
	mdDisplay := fmt.Sprintf("%s %-11s", mdEmoji, truncate(conflict.MarkdownStatus, 11))

	// Build the main line
	var line string
	line = fmt.Sprintf("%-14s  %-20s  %-20s",
		conflict.TaskID,
		dbDisplay,
		mdDisplay)

	// Add resolution indicator with visual distinction
	switch resolution {
	case "db":
		line += "  " + resolvedDBStyle.Render("→ use DB ✓")
	case "markdown":
		line += "  " + resolvedMarkdownStyle.Render("→ use MD ✓")
	default:
		line += "  " + warningStyle.Render("← choose")
	}

	// Highlight if selected
	if selected {
		return modalSelectedStyle.Render("▸ " + line)
	}
	return queuedStyle.Render("  " + line)
}

func (m Model) renderGroupPriorities() string {
	var b strings.Builder
	b.WriteString(titleStyle.Render("GROUP PRIORITIES"))
	b.WriteString("\n\n")

	if len(m.groupPriorityItems) == 0 {
		b.WriteString(queuedStyle.Render("  No groups found. Run 'claude-orch sync' to load tasks."))
		return b.String()
	}

	// Group items by tier
	tiers := make(map[int][]GroupPriorityItem)
	var maxTier int
	for _, item := range m.groupPriorityItems {
		tier := item.Priority
		if tier < 0 {
			tier = 0 // Unassigned defaults to tier 0 for display
		}
		tiers[tier] = append(tiers[tier], item)
		if tier > maxTier {
			maxTier = tier
		}
	}

	// Determine active tier (lowest with incomplete tasks)
	activeTier := 0
	for tier := 0; tier <= maxTier; tier++ {
		for _, item := range tiers[tier] {
			if item.Completed < item.Total {
				activeTier = tier
				goto foundActive
			}
		}
	}
foundActive:

	// Also track unassigned items separately for display
	var unassignedItems []GroupPriorityItem
	for _, item := range m.groupPriorityItems {
		if item.Priority == -1 {
			unassignedItems = append(unassignedItems, item)
		}
	}

	// Render each tier
	itemIndex := 0
	for tier := 0; tier <= maxTier; tier++ {
		items := tiers[tier]
		// Filter out unassigned items from tier 0 display (they go in separate section)
		if tier == 0 {
			var assigned []GroupPriorityItem
			for _, item := range items {
				if item.Priority >= 0 {
					assigned = append(assigned, item)
				}
			}
			items = assigned
		}
		if len(items) == 0 {
			continue
		}

		// Tier header
		tierStatus := "waiting"
		if tier == activeTier {
			tierStatus = "active"
		} else if tier < activeTier {
			tierStatus = "complete"
		}
		tierHeader := fmt.Sprintf("  Tier %d (%s)", tier, tierStatus)
		if tier == activeTier {
			b.WriteString(runningStyle.Render(tierHeader))
		} else {
			b.WriteString(queuedStyle.Render(tierHeader))
		}
		b.WriteString("\n")

		// Render items in this tier
		for _, item := range items {
			selected := itemIndex == m.selectedPriorityRow
			var statusIcon string
			var style lipgloss.Style

			if item.Completed == item.Total && item.Total > 0 {
				statusIcon = "✓"
				style = completedStyle
			} else if tier == activeTier {
				statusIcon = "●"
				style = runningStyle
			} else {
				statusIcon = "○"
				style = queuedStyle
			}

			line := fmt.Sprintf("    %s %-18s [%d/%d complete]",
				statusIcon, truncate(item.Name, 18), item.Completed, item.Total)

			if selected {
				line = fmt.Sprintf("  > %s", line[4:])
				b.WriteString(tabActiveStyle.Render(line))
			} else {
				b.WriteString(style.Render(line))
			}
			b.WriteString("\n")
			itemIndex++
		}
		b.WriteString("\n")
	}

	// Show unassigned groups section
	if len(unassignedItems) > 0 {
		b.WriteString(queuedStyle.Render("  (unassigned - runs with tier 0)"))
		b.WriteString("\n")
		for _, item := range unassignedItems {
			selected := itemIndex == m.selectedPriorityRow
			statusIcon := "○"
			style := queuedStyle

			if item.Completed == item.Total && item.Total > 0 {
				statusIcon = "✓"
				style = completedStyle
			}

			line := fmt.Sprintf("    %s %-18s [%d/%d complete]",
				statusIcon, truncate(item.Name, 18), item.Completed, item.Total)

			if selected {
				line = fmt.Sprintf("  > %s", line[4:])
				b.WriteString(tabActiveStyle.Render(line))
			} else {
				b.WriteString(style.Render(line))
			}
			b.WriteString("\n")
			itemIndex++
		}
	}

	// Help text
	b.WriteString("\n")
	b.WriteString(queuedStyle.Render("  [↑/↓] select  [+/-] change tier  [u] unassign  [g] back"))

	return b.String()
}

// renderMaintenanceModal renders the maintenance task selection modal
func (m Model) renderMaintenanceModal() string {
	var b strings.Builder

	// Title
	b.WriteString(modalTitleStyle.Render("MAINTENANCE TASKS"))
	b.WriteString("\n\n")

	if m.maintenanceModal.Phase == 0 {
		// Phase 0: Template selection
		b.WriteString(queuedStyle.Render("Select a maintenance task:"))
		b.WriteString("\n\n")

		for i, tmpl := range m.maintenanceModal.Templates {
			selected := i == m.maintenanceModal.Selected
			line := fmt.Sprintf("%-20s  %s", tmpl.Name, tmpl.Description)
			if selected {
				b.WriteString(modalSelectedStyle.Render("▸ " + line))
			} else {
				b.WriteString(queuedStyle.Render("  " + line))
			}
			b.WriteString("\n")
		}

		b.WriteString("\n")
		b.WriteString(queuedStyle.Render("[j/k] navigate  [enter] select  [esc] cancel"))
	} else {
		// Phase 1: Scope selection
		tmpl := m.maintenanceModal.Templates[m.maintenanceModal.Selected]
		b.WriteString(queuedStyle.Render(fmt.Sprintf("Apply \"%s\" to:", tmpl.Name)))
		b.WriteString("\n\n")

		// Module scope option
		moduleName := m.maintenanceModal.TargetModule
		if moduleName == "" {
			moduleName = "(no module selected)"
		}
		b.WriteString(runningStyle.Render("[1]"))
		b.WriteString(queuedStyle.Render(fmt.Sprintf(" Module: %s", moduleName)))
		b.WriteString("\n")

		// Package scope option
		b.WriteString(runningStyle.Render("[2]"))
		b.WriteString(queuedStyle.Render(fmt.Sprintf(" Package: internal/%s", moduleName)))
		b.WriteString("\n")

		// All scope option
		b.WriteString(runningStyle.Render("[3]"))
		b.WriteString(queuedStyle.Render(" Entire codebase"))
		b.WriteString("\n\n")

		b.WriteString(queuedStyle.Render("[1-3] select scope  [esc] back"))
	}

	// Wrap in modal style
	modalContent := b.String()
	modalWidth := 55
	if m.width > 65 {
		modalWidth = 55
	} else if m.width > 45 {
		modalWidth = m.width - 10
	} else {
		modalWidth = m.width - 4
	}

	return modalStyle.Width(modalWidth).Render(modalContent)
}
