package tui

import (
	"errors"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/hochfrequenz/claude-plan-orchestrator/internal/domain"
	"github.com/hochfrequenz/claude-plan-orchestrator/internal/executor"
)

func TestNewModel(t *testing.T) {
	tasks := []*domain.Task{
		{ID: domain.TaskID{Module: "test", EpicNum: 0}, Title: "Setup", Status: domain.StatusComplete},
		{ID: domain.TaskID{Module: "test", EpicNum: 1}, Title: "Core", Status: domain.StatusNotStarted},
	}

	cfg := ModelConfig{
		MaxActive:   3,
		AllTasks:    tasks,
		Queued:      tasks[1:],
		ProjectRoot: "/test/project",
	}

	model := NewModel(cfg)

	if model.maxActive != 3 {
		t.Errorf("maxActive = %d, want 3", model.maxActive)
	}

	if len(model.allTasks) != 2 {
		t.Errorf("allTasks count = %d, want 2", len(model.allTasks))
	}

	if len(model.queued) != 1 {
		t.Errorf("queued count = %d, want 1", len(model.queued))
	}

	if model.activeTab != 0 {
		t.Errorf("activeTab = %d, want 0", model.activeTab)
	}
}

func TestModel_TabSwitching(t *testing.T) {
	model := NewModel(ModelConfig{MaxActive: 3})
	model.width = 100
	model.height = 40

	// Start on tab 0 (Dashboard)
	if model.activeTab != 0 {
		t.Fatalf("initial activeTab = %d, want 0", model.activeTab)
	}

	// Press tab to move to Tasks (1)
	newModel, _ := model.Update(tea.KeyMsg{Type: tea.KeyTab})
	model = newModel.(Model)

	if model.activeTab != 1 {
		t.Errorf("after first tab: activeTab = %d, want 1", model.activeTab)
	}

	// Press tab again to move to Agents (2)
	newModel, _ = model.Update(tea.KeyMsg{Type: tea.KeyTab})
	model = newModel.(Model)

	if model.activeTab != 2 {
		t.Errorf("after second tab: activeTab = %d, want 2", model.activeTab)
	}

	// Continue tabbing through all tabs
	for i := 0; i < 3; i++ {
		newModel, _ = model.Update(tea.KeyMsg{Type: tea.KeyTab})
		model = newModel.(Model)
	}

	// Should wrap back to 0
	if model.activeTab != 0 {
		t.Errorf("after wrap: activeTab = %d, want 0", model.activeTab)
	}
}

func TestModel_MaxAgentsAdjustment(t *testing.T) {
	model := NewModel(ModelConfig{MaxActive: 3})
	model.width = 100
	model.height = 40
	model.activeTab = 2 // Agents tab

	// Increase max agents
	newModel, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("+")})
	model = newModel.(Model)

	if model.maxActive != 4 {
		t.Errorf("after +: maxActive = %d, want 4", model.maxActive)
	}

	if !model.configChanged {
		t.Error("configChanged should be true after +")
	}

	// Decrease max agents
	newModel, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("-")})
	model = newModel.(Model)

	if model.maxActive != 3 {
		t.Errorf("after -: maxActive = %d, want 3", model.maxActive)
	}
}

func TestModel_MaxAgentsLimits(t *testing.T) {
	// Test upper limit
	model := NewModel(ModelConfig{MaxActive: 10})
	model.width = 100
	model.height = 40
	model.activeTab = 2

	newModel, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("+")})
	model = newModel.(Model)

	if model.maxActive != 10 {
		t.Errorf("should not exceed 10: maxActive = %d", model.maxActive)
	}

	// Test lower limit
	model = NewModel(ModelConfig{MaxActive: 1})
	model.activeTab = 2

	newModel, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("-")})
	model = newModel.(Model)

	if model.maxActive != 1 {
		t.Errorf("should not go below 1: maxActive = %d", model.maxActive)
	}
}

func TestModel_MaxAgentsOnlyOnAgentsTab(t *testing.T) {
	model := NewModel(ModelConfig{MaxActive: 3})
	model.width = 100
	model.height = 40
	model.activeTab = 0 // Dashboard, not Agents

	newModel, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("+")})
	model = newModel.(Model)

	if model.maxActive != 3 {
		t.Errorf("+/- should only work on Agents tab: maxActive = %d, want 3", model.maxActive)
	}

	if model.configChanged {
		t.Error("configChanged should be false when not on Agents tab")
	}
}

func TestModel_ScrollNavigation(t *testing.T) {
	model := NewModel(ModelConfig{MaxActive: 3})
	model.width = 100
	model.height = 40
	model.activeTab = 1 // Tasks tab

	// Scroll down
	newModel, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	model = newModel.(Model)

	if model.taskScroll != 1 {
		t.Errorf("after j: taskScroll = %d, want 1", model.taskScroll)
	}

	// Scroll up
	newModel, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("k")})
	model = newModel.(Model)

	if model.taskScroll != 0 {
		t.Errorf("after k: taskScroll = %d, want 0", model.taskScroll)
	}
}

func TestModel_ModuleSelection(t *testing.T) {
	tasks := []*domain.Task{
		{ID: domain.TaskID{Module: "alpha", EpicNum: 0}, Title: "Setup", Status: domain.StatusComplete},
		{ID: domain.TaskID{Module: "beta", EpicNum: 0}, Title: "Setup", Status: domain.StatusNotStarted},
	}

	model := NewModel(ModelConfig{MaxActive: 3, AllTasks: tasks})
	model.width = 100
	model.height = 40
	model.activeTab = 3 // Modules tab

	if model.selectedModule != 0 {
		t.Errorf("initial selectedModule = %d, want 0", model.selectedModule)
	}

	// Navigate down
	newModel, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	model = newModel.(Model)

	if model.selectedModule != 1 {
		t.Errorf("after j on Modules tab: selectedModule = %d, want 1", model.selectedModule)
	}

	// Navigate up
	newModel, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("k")})
	model = newModel.(Model)

	if model.selectedModule != 0 {
		t.Errorf("after k on Modules tab: selectedModule = %d, want 0", model.selectedModule)
	}
}

func TestModel_ViewModeToggle(t *testing.T) {
	model := NewModel(ModelConfig{MaxActive: 3})
	model.width = 100
	model.height = 40

	if model.viewMode != ViewByPriority {
		t.Fatalf("initial viewMode should be ViewByPriority")
	}

	// Toggle view mode
	newModel, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("v")})
	model = newModel.(Model)

	if model.viewMode != ViewByModule {
		t.Errorf("after v: viewMode should be ViewByModule")
	}

	// Toggle back
	newModel, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("v")})
	model = newModel.(Model)

	if model.viewMode != ViewByPriority {
		t.Errorf("after second v: viewMode should be ViewByPriority")
	}
}

func TestModel_QuitCommands(t *testing.T) {
	model := NewModel(ModelConfig{MaxActive: 3})
	model.width = 100
	model.height = 40

	// Test 'q' quit
	_, cmd := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")})
	if cmd == nil {
		t.Error("'q' should return a quit command")
	}

	// Test ctrl+c quit
	_, cmd = model.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	if cmd == nil {
		t.Error("ctrl+c should return a quit command")
	}
}

func TestModel_ShortcutKeys(t *testing.T) {
	model := NewModel(ModelConfig{MaxActive: 3})
	model.width = 100
	model.height = 40

	// Test 't' shortcut to Tasks tab
	newModel, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("t")})
	model = newModel.(Model)

	if model.activeTab != 1 {
		t.Errorf("'t' should switch to Tasks tab (1), got %d", model.activeTab)
	}

	// Test 'm' shortcut to Modules tab
	newModel, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("m")})
	model = newModel.(Model)

	if model.activeTab != 3 {
		t.Errorf("'m' should switch to Modules tab (3), got %d", model.activeTab)
	}
}

func TestModel_SetAgents(t *testing.T) {
	model := NewModel(ModelConfig{MaxActive: 3})

	agents := []*AgentView{
		{TaskID: "test/E00", Status: "running"},
		{TaskID: "test/E01", Status: "running"},
		{TaskID: "test/E02", Status: "completed"},
	}

	model.SetAgents(agents)

	if len(model.agents) != 3 {
		t.Errorf("agents count = %d, want 3", len(model.agents))
	}

	if model.activeCount != 2 {
		t.Errorf("activeCount = %d, want 2 (running agents)", model.activeCount)
	}
}

func TestModel_GetMaxActive(t *testing.T) {
	model := NewModel(ModelConfig{MaxActive: 5})

	if model.GetMaxActive() != 5 {
		t.Errorf("GetMaxActive() = %d, want 5", model.GetMaxActive())
	}
}

func TestModel_ConfigChanged(t *testing.T) {
	model := NewModel(ModelConfig{MaxActive: 3})

	if model.ConfigChanged() {
		t.Error("ConfigChanged() should be false initially")
	}

	model.configChanged = true

	if !model.ConfigChanged() {
		t.Error("ConfigChanged() should be true after setting")
	}
}

func TestComputeModuleSummaries(t *testing.T) {
	tasks := []*domain.Task{
		{ID: domain.TaskID{Module: "alpha", EpicNum: 0}, Status: domain.StatusComplete},
		{ID: domain.TaskID{Module: "alpha", EpicNum: 1}, Status: domain.StatusInProgress},
		{ID: domain.TaskID{Module: "alpha", EpicNum: 2}, Status: domain.StatusNotStarted},
		{ID: domain.TaskID{Module: "beta", EpicNum: 0}, Status: domain.StatusComplete,
			TestSummary: &domain.TestSummary{Tests: 10, Passed: 8, Failed: 2}},
	}

	summaries := computeModuleSummaries(tasks)

	if len(summaries) != 2 {
		t.Fatalf("summaries count = %d, want 2", len(summaries))
	}

	// Find alpha module
	var alpha, beta *ModuleSummary
	for _, s := range summaries {
		if s.Name == "alpha" {
			alpha = s
		} else if s.Name == "beta" {
			beta = s
		}
	}

	if alpha == nil {
		t.Fatal("alpha module not found")
	}
	if alpha.TotalEpics != 3 {
		t.Errorf("alpha.TotalEpics = %d, want 3", alpha.TotalEpics)
	}
	if alpha.CompletedEpics != 1 {
		t.Errorf("alpha.CompletedEpics = %d, want 1", alpha.CompletedEpics)
	}
	if alpha.InProgressEpics != 1 {
		t.Errorf("alpha.InProgressEpics = %d, want 1", alpha.InProgressEpics)
	}

	if beta == nil {
		t.Fatal("beta module not found")
	}
	if beta.TotalTests != 10 {
		t.Errorf("beta.TotalTests = %d, want 10", beta.TotalTests)
	}
	if beta.PassedTests != 8 {
		t.Errorf("beta.PassedTests = %d, want 8", beta.PassedTests)
	}
	if beta.FailedTests != 2 {
		t.Errorf("beta.FailedTests = %d, want 2", beta.FailedTests)
	}
}

func TestModel_WindowResize(t *testing.T) {
	model := NewModel(ModelConfig{MaxActive: 3})

	newModel, _ := model.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	model = newModel.(Model)

	if model.width != 120 {
		t.Errorf("width = %d, want 120", model.width)
	}
	if model.height != 40 {
		t.Errorf("height = %d, want 40", model.height)
	}
}

func TestModel_TickMsg(t *testing.T) {
	model := NewModel(ModelConfig{MaxActive: 3})
	model.width = 100
	model.height = 40

	// TickMsg should return another tick command
	_, cmd := model.Update(TickMsg(time.Now()))

	if cmd == nil {
		t.Error("TickMsg should return a command for the next tick")
	}
}

func TestModel_TestCompleteMsg(t *testing.T) {
	model := NewModel(ModelConfig{MaxActive: 3})
	model.testRunning = true

	// Test success case
	newModel, _ := model.Update(TestCompleteMsg{Output: "All tests passed!", Err: nil})
	model = newModel.(Model)

	if model.testRunning {
		t.Error("testRunning should be false after TestCompleteMsg")
	}
	if model.testOutput != "All tests passed!" {
		t.Errorf("testOutput = %q, want 'All tests passed!'", model.testOutput)
	}

	// Test error case
	model.testRunning = true
	newModel, _ = model.Update(TestCompleteMsg{Output: "", Err: errors.New("test failed")})
	model = newModel.(Model)

	if model.testRunning {
		t.Error("testRunning should be false after error")
	}
	if model.testOutput != "Error: test failed" {
		t.Errorf("testOutput = %q, want 'Error: test failed'", model.testOutput)
	}
}

func TestModel_BatchStartKey(t *testing.T) {
	tasks := []*domain.Task{
		{ID: domain.TaskID{Module: "test", EpicNum: 0}, Status: domain.StatusNotStarted},
		{ID: domain.TaskID{Module: "test", EpicNum: 1}, Status: domain.StatusNotStarted},
	}

	model := NewModel(ModelConfig{MaxActive: 3, Queued: tasks})
	model.width = 100
	model.height = 40
	model.activeTab = 0 // Dashboard

	// Press 's' to start batch
	newModel, cmd := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("s")})
	model = newModel.(Model)

	if !model.batchRunning {
		t.Error("batchRunning should be true after 's'")
	}

	if model.batchPaused {
		t.Error("batchPaused should be false after start")
	}

	if cmd == nil {
		t.Error("'s' should return a command to start the batch")
	}
}

func TestModel_BatchStartNoSlots(t *testing.T) {
	tasks := []*domain.Task{
		{ID: domain.TaskID{Module: "test", EpicNum: 0}, Status: domain.StatusNotStarted},
	}

	model := NewModel(ModelConfig{MaxActive: 2, Queued: tasks})
	model.width = 100
	model.height = 40
	model.activeTab = 0
	model.activeCount = 2 // All slots used

	// Press 's' when no slots available
	newModel, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("s")})
	model = newModel.(Model)

	if model.batchRunning {
		t.Error("batchRunning should be false when no slots available")
	}

	if model.statusMsg != "No agent slots available" {
		t.Errorf("statusMsg = %q, want 'No agent slots available'", model.statusMsg)
	}
}

func TestModel_BatchStartNoTasks(t *testing.T) {
	model := NewModel(ModelConfig{MaxActive: 3, Queued: nil})
	model.width = 100
	model.height = 40
	model.activeTab = 0

	// Press 's' when no tasks queued
	newModel, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("s")})
	model = newModel.(Model)

	if model.batchRunning {
		t.Error("batchRunning should be false when no tasks queued")
	}

	if model.statusMsg != "No tasks queued" {
		t.Errorf("statusMsg = %q, want 'No tasks queued'", model.statusMsg)
	}
}

func TestModel_BatchPauseResume(t *testing.T) {
	tasks := []*domain.Task{
		{ID: domain.TaskID{Module: "test", EpicNum: 0}, Status: domain.StatusNotStarted},
	}

	model := NewModel(ModelConfig{MaxActive: 3, Queued: tasks})
	model.width = 100
	model.height = 40
	model.activeTab = 0

	// Start batch first
	newModel, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("s")})
	model = newModel.(Model)

	// Press 'p' to pause
	newModel, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("p")})
	model = newModel.(Model)

	if !model.batchPaused {
		t.Error("batchPaused should be true after 'p'")
	}

	if model.statusMsg != "Batch paused" {
		t.Errorf("statusMsg = %q, want 'Batch paused'", model.statusMsg)
	}

	// Press 'p' again to resume
	newModel, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("p")})
	model = newModel.(Model)

	if model.batchPaused {
		t.Error("batchPaused should be false after second 'p'")
	}

	if model.statusMsg != "Batch resumed" {
		t.Errorf("statusMsg = %q, want 'Batch resumed'", model.statusMsg)
	}
}

func TestModel_BatchStartOnlyOnDashboard(t *testing.T) {
	tasks := []*domain.Task{
		{ID: domain.TaskID{Module: "test", EpicNum: 0}, Status: domain.StatusNotStarted},
	}

	model := NewModel(ModelConfig{MaxActive: 3, Queued: tasks})
	model.width = 100
	model.height = 40
	model.activeTab = 1 // Tasks tab, not Dashboard

	// Press 's' on non-Dashboard tab
	newModel, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("s")})
	model = newModel.(Model)

	if model.batchRunning {
		t.Error("'s' should only start batch on Dashboard tab")
	}
}

func TestModel_BatchStartMsg(t *testing.T) {
	model := NewModel(ModelConfig{MaxActive: 3})
	model.width = 100
	model.height = 40

	newModel, _ := model.Update(BatchStartMsg{Count: 2})
	model = newModel.(Model)

	if model.statusMsg != "Batch started: 2 task(s)" {
		t.Errorf("statusMsg = %q, want 'Batch started: 2 task(s)'", model.statusMsg)
	}
}

func TestModel_StatusUpdateMsg(t *testing.T) {
	model := NewModel(ModelConfig{MaxActive: 3})

	newModel, _ := model.Update(StatusUpdateMsg("Custom status message"))
	model = newModel.(Model)

	if model.statusMsg != "Custom status message" {
		t.Errorf("statusMsg = %q, want 'Custom status message'", model.statusMsg)
	}
}

func TestModel_AgentUpdateMsg(t *testing.T) {
	agents := []*AgentView{
		{TaskID: "test/E00", Status: executor.AgentRunning},
		{TaskID: "test/E01", Status: executor.AgentRunning},
	}
	model := NewModel(ModelConfig{MaxActive: 3, Agents: agents})

	// Update one agent to completed
	newModel, _ := model.Update(AgentUpdateMsg{TaskID: "test/E00", Status: executor.AgentCompleted})
	model = newModel.(Model)

	// Verify agent status was updated
	if model.agents[0].Status != executor.AgentCompleted {
		t.Errorf("agent[0].Status = %v, want AgentCompleted", model.agents[0].Status)
	}

	// Verify active count was updated
	if model.activeCount != 1 {
		t.Errorf("activeCount = %d, want 1", model.activeCount)
	}
}

func TestModel_AgentCompleteMsg(t *testing.T) {
	agents := []*AgentView{
		{TaskID: "test/E00", Status: executor.AgentRunning},
	}
	model := NewModel(ModelConfig{MaxActive: 3, Agents: agents})
	model.batchRunning = true

	// Complete the agent
	newModel, _ := model.Update(AgentCompleteMsg{TaskID: "test/E00", Success: true})
	model = newModel.(Model)

	// Verify agent status
	if model.agents[0].Status != executor.AgentCompleted {
		t.Errorf("agent[0].Status = %v, want AgentCompleted", model.agents[0].Status)
	}

	// Verify batch completed since no more running agents
	if model.batchRunning {
		t.Error("batchRunning should be false when all agents complete")
	}

	if model.statusMsg != "Batch complete" {
		t.Errorf("statusMsg = %q, want 'Batch complete'", model.statusMsg)
	}
}

func TestModel_AgentCompleteMsg_Failed(t *testing.T) {
	agents := []*AgentView{
		{TaskID: "test/E00", Status: executor.AgentRunning},
	}
	model := NewModel(ModelConfig{MaxActive: 3, Agents: agents})
	model.batchRunning = true

	// Fail the agent
	newModel, _ := model.Update(AgentCompleteMsg{TaskID: "test/E00", Success: false})
	model = newModel.(Model)

	// Verify agent status
	if model.agents[0].Status != executor.AgentFailed {
		t.Errorf("agent[0].Status = %v, want AgentFailed", model.agents[0].Status)
	}
}

func TestModel_BatchStartMsgWithTasks(t *testing.T) {
	tasks := []*domain.Task{
		{ID: domain.TaskID{Module: "test", EpicNum: 0}, Title: "Task 0", Status: domain.StatusNotStarted},
		{ID: domain.TaskID{Module: "test", EpicNum: 1}, Title: "Task 1", Status: domain.StatusNotStarted},
	}
	model := NewModel(ModelConfig{MaxActive: 3, Queued: tasks})
	model.width = 100
	model.height = 40

	// Simulate batch start with one task started
	newModel, _ := model.Update(BatchStartMsg{
		Count: 1,
		Started: []AgentStartInfo{
			{TaskID: "test/E00", WorktreePath: "/tmp/worktree"},
		},
	})
	model = newModel.(Model)

	// Verify agent was added
	if len(model.agents) != 1 {
		t.Errorf("len(agents) = %d, want 1", len(model.agents))
	}

	if model.agents[0].TaskID != "test/E00" {
		t.Errorf("agents[0].TaskID = %q, want 'test/E00'", model.agents[0].TaskID)
	}

	if model.agents[0].Title != "Task 0" {
		t.Errorf("agents[0].Title = %q, want 'Task 0'", model.agents[0].Title)
	}

	if model.agents[0].WorktreePath != "/tmp/worktree" {
		t.Errorf("agents[0].WorktreePath = %q, want '/tmp/worktree'", model.agents[0].WorktreePath)
	}

	// Verify task was removed from queued
	if len(model.queued) != 1 {
		t.Errorf("len(queued) = %d, want 1", len(model.queued))
	}

	if model.queued[0].ID.String() != "test/E01" {
		t.Errorf("queued[0].ID = %q, want 'test/E01'", model.queued[0].ID.String())
	}
}

func TestModel_UpdateAgentsFromManager(t *testing.T) {
	agents := []*AgentView{
		{TaskID: "test/E00", Status: executor.AgentQueued},
	}
	agentMgr := executor.NewAgentManager(3)

	model := NewModel(ModelConfig{
		MaxActive:    3,
		Agents:       agents,
		AgentManager: agentMgr,
	})
	model.batchRunning = true

	// Add an agent to the manager
	agent := &executor.Agent{
		TaskID: domain.TaskID{Module: "test", EpicNum: 0},
		Status: executor.AgentRunning,
	}
	agentMgr.Add(agent)

	// Trigger update via TickMsg
	newModel, _ := model.Update(TickMsg(time.Now()))
	model = newModel.(Model)

	// Verify agent status was synced
	if model.agents[0].Status != executor.AgentRunning {
		t.Errorf("agents[0].Status = %v, want AgentRunning", model.agents[0].Status)
	}

	if model.activeCount != 1 {
		t.Errorf("activeCount = %d, want 1", model.activeCount)
	}
}

func TestModel_AgentSelection(t *testing.T) {
	agents := []*AgentView{
		{TaskID: "test/E00", Status: executor.AgentRunning},
		{TaskID: "test/E01", Status: executor.AgentCompleted},
		{TaskID: "test/E02", Status: executor.AgentFailed, Error: "test error"},
	}
	model := NewModel(ModelConfig{MaxActive: 3, Agents: agents})
	model.width = 100
	model.height = 40
	model.activeTab = 2 // Agents tab

	// Navigate down
	newModel, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	model = newModel.(Model)

	if model.selectedAgent != 1 {
		t.Errorf("selectedAgent = %d, want 1", model.selectedAgent)
	}

	// Navigate up
	newModel, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("k")})
	model = newModel.(Model)

	if model.selectedAgent != 0 {
		t.Errorf("selectedAgent = %d, want 0", model.selectedAgent)
	}
}

func TestModel_AgentDetailToggle(t *testing.T) {
	agents := []*AgentView{
		{TaskID: "test/E00", Status: executor.AgentFailed, Error: "test error"},
	}
	model := NewModel(ModelConfig{MaxActive: 3, Agents: agents})
	model.width = 100
	model.height = 40
	model.activeTab = 2 // Agents tab

	// Toggle detail view with enter
	newModel, _ := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = newModel.(Model)

	if !model.showAgentDetail {
		t.Error("showAgentDetail should be true after enter")
	}

	// Toggle off with enter
	newModel, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = newModel.(Model)

	if model.showAgentDetail {
		t.Error("showAgentDetail should be false after second enter")
	}

	// Toggle on and close with esc
	newModel, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = newModel.(Model)
	newModel, _ = model.Update(tea.KeyMsg{Type: tea.KeyEsc})
	model = newModel.(Model)

	if model.showAgentDetail {
		t.Error("showAgentDetail should be false after esc")
	}
}

func TestModel_AgentViewHasErrorAndOutput(t *testing.T) {
	agents := []*AgentView{
		{
			TaskID:       "test/E00",
			Status:       executor.AgentFailed,
			Error:        "command failed: exit code 1",
			Output:       []string{"line 1", "line 2", "error output"},
			WorktreePath: "/tmp/worktree/test",
		},
	}
	model := NewModel(ModelConfig{MaxActive: 3, Agents: agents})

	// Verify fields are preserved
	if model.agents[0].Error != "command failed: exit code 1" {
		t.Errorf("Error = %q, want 'command failed: exit code 1'", model.agents[0].Error)
	}

	if len(model.agents[0].Output) != 3 {
		t.Errorf("len(Output) = %d, want 3", len(model.agents[0].Output))
	}

	if model.agents[0].WorktreePath != "/tmp/worktree/test" {
		t.Errorf("WorktreePath = %q, want '/tmp/worktree/test'", model.agents[0].WorktreePath)
	}
}
