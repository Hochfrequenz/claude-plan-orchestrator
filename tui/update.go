package tui

import (
	tea "github.com/charmbracelet/bubbletea"
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
		case "k", "up":
			if m.selectedRow > 0 {
				m.selectedRow--
			}
		case "tab":
			m.activeTab = (m.activeTab + 1) % 3
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
