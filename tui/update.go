package tui

import (
	"fmt"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/hochfrequenz/claude-plan-orchestrator/internal/domain"
	"github.com/hochfrequenz/claude-plan-orchestrator/internal/mcp"
)

// TestCompleteMsg is sent when test execution completes
type TestCompleteMsg struct {
	Output string
	Err    error
}

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
			if m.activeTab == 3 { // Modules tab
				if m.selectedModule > 0 {
					m.selectedModule--
				}
				// Scroll if needed
				if m.selectedModule < m.taskScroll {
					m.taskScroll = m.selectedModule
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
		}

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height

	case TickMsg:
		// Refresh data would happen here
		return m, tickCmd()

	case TestCompleteMsg:
		m.testRunning = false
		if msg.Err != nil {
			m.testOutput = "Error: " + msg.Err.Error()
		} else {
			m.testOutput = msg.Output
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
		if a.Status == "running" {
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
