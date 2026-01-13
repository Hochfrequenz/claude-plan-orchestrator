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

	// Status message (if any)
	if m.statusMsg != "" {
		statusLine := fmt.Sprintf(" %s ", m.statusMsg)
		if m.batchRunning {
			if m.batchPaused {
				b.WriteString(warningStyle.Width(m.width).Render("⏸ " + statusLine))
			} else {
				b.WriteString(runningStyle.Width(m.width).Render("▶ " + statusLine))
			}
		} else {
			b.WriteString(queuedStyle.Width(m.width).Render(statusLine))
		}
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
		statusBar = fmt.Sprintf(" [tab]switch [j/k]scroll [x]run tests %s [q]uit ", mouseHint)
	default:
		testHint := ""
		if m.buildPoolStatus == "connected" {
			testHint = "[T]est worker "
		}
		if m.batchRunning && !m.batchPaused {
			statusBar = fmt.Sprintf(" [tab]switch [t]asks [m]odules %s[p]ause %s [q]uit ", testHint, mouseHint)
		} else if m.batchRunning && m.batchPaused {
			statusBar = fmt.Sprintf(" [tab]switch [t]asks [m]odules %s[p]resume %s [q]uit ", testHint, mouseHint)
		} else {
			statusBar = fmt.Sprintf(" [tab]switch [t]asks [m]odules %s[s]tart batch %s [q]uit ", testHint, mouseHint)
		}
	}
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

	// Use scheduler to get tasks in priority order, respecting dependencies
	sched := scheduler.New(m.queued, m.completedTasks)
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

	// Then show blocked tasks if we have room
	if shown < limit {
		for _, task := range m.queued {
			if shown >= limit {
				break
			}
			if readySet[task.ID.String()] {
				continue // Already shown above
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

	line := fmt.Sprintf("  %s %s %-15s %-30s",
		statusIcon, prioStr, task.ID.String(), truncate(task.Title, 30))

	return style.Render(line)
}

func (m Model) renderAgentsDetail() string {
	var b strings.Builder

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

	// Output section with scrolling
	if len(agent.Output) > 0 {
		b.WriteString("\n")

		// Format the JSON output into readable lines
		maxWidth := 80
		if m.width > 10 {
			maxWidth = m.width - 10
		}
		formattedLines := formatClaudeOutput(agent.Output, maxWidth)

		// Calculate visible window
		maxLines := 20
		if m.height > 20 {
			maxLines = m.height - 15 // Leave room for header and footer
		}

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
		b.WriteString("\n")
		b.WriteString(queuedStyle.Render("  No output captured"))
		b.WriteString("\n")
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
