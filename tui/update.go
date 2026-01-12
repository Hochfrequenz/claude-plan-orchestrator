package tui

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/hochfrequenz/claude-plan-orchestrator/internal/domain"
)

// Update handles messages
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			return m, tea.Quit
		case "r":
			return m, tickCmd()
		case "j", "down":
			m.selectedRow++
			if m.activeTab == 1 {
				m.taskScroll++
			}
		case "k", "up":
			if m.selectedRow > 0 {
				m.selectedRow--
			}
			if m.activeTab == 1 && m.taskScroll > 0 {
				m.taskScroll--
			}
		case "tab":
			m.activeTab = (m.activeTab + 1) % 4
			m.selectedRow = 0
			m.taskScroll = 0
		case "t":
			// Toggle to tasks tab
			m.activeTab = 1
		case "v":
			// Toggle view mode (priority/module)
			if m.viewMode == ViewByPriority {
				m.viewMode = ViewByModule
			} else {
				m.viewMode = ViewByPriority
			}
		}

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height

	case TickMsg:
		// Refresh data would happen here
		return m, tickCmd()
	}

	return m, nil
}

// SetAgents updates the agents list
func (m *Model) SetAgents(agents []*AgentView) {
	m.agents = agents
	m.activeCount = 0
	for _, a := range agents {
		if a.Status == "running" {
			m.activeCount++
		}
	}
}

// SetTasks updates the all tasks list
func (m *Model) SetTasks(tasks []*domain.Task) {
	m.allTasks = tasks
}
