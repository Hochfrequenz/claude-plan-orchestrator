package tui

import (
	"fmt"
	"strings"
	"time"

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
