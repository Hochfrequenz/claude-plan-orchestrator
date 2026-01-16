package api

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/hochfrequenz/claude-plan-orchestrator/internal/domain"
	"github.com/hochfrequenz/claude-plan-orchestrator/internal/executor"
)

// TaskResponse is the API response for a task
type TaskResponse struct {
	ID          string   `json:"id"`
	Module      string   `json:"module"`
	EpicNum     int      `json:"epic_num"`
	Title       string   `json:"title"`
	Description string   `json:"description,omitempty"`
	Status      string   `json:"status"`
	Priority    string   `json:"priority,omitempty"`
	DependsOn   []string `json:"depends_on,omitempty"`
	NeedsReview bool     `json:"needs_review"`
}

// StatusResponse is the API response for overall status
type StatusResponse struct {
	Total      int `json:"total"`
	NotStarted int `json:"not_started"`
	InProgress int `json:"in_progress"`
	Complete   int `json:"complete"`
	Agents     int `json:"agents_running"`
}

// AgentResponse is the API response for an agent
type AgentResponse struct {
	ID           string   `json:"id"`
	TaskID       string   `json:"task_id"`
	TaskTitle    string   `json:"task_title,omitempty"`
	Status       string   `json:"status"`
	StartedAt    *string  `json:"started_at,omitempty"`
	FinishedAt   *string  `json:"finished_at,omitempty"`
	Duration     string   `json:"duration"`
	TokensInput  int      `json:"tokens_input"`
	TokensOutput int      `json:"tokens_output"`
	CostUSD      float64  `json:"cost_usd"`
	LogLines     []string `json:"log_lines,omitempty"`
	WorktreePath string   `json:"worktree_path,omitempty"`
	Error        string   `json:"error,omitempty"`
}

// BatchStatusResponse is the API response for batch status
type BatchStatusResponse struct {
	Running bool `json:"running"`
	Paused  bool `json:"paused"`
	Auto    bool `json:"auto"`
}

func taskToResponse(t *domain.Task) TaskResponse {
	deps := make([]string, len(t.DependsOn))
	for i, d := range t.DependsOn {
		deps[i] = d.String()
	}

	return TaskResponse{
		ID:          t.ID.String(),
		Module:      t.ID.Module,
		EpicNum:     t.ID.EpicNum,
		Title:       t.Title,
		Description: t.Description,
		Status:      string(t.Status),
		Priority:    string(t.Priority),
		DependsOn:   deps,
		NeedsReview: t.NeedsReview,
	}
}

func agentToResponse(a *executor.Agent, includeLines int) AgentResponse {
	resp := AgentResponse{
		ID:           a.ID,
		TaskID:       a.TaskID.String(),
		Status:       string(a.Status),
		TokensInput:  a.TokensInput,
		TokensOutput: a.TokensOutput,
		CostUSD:      a.CostUSD,
		WorktreePath: a.WorktreePath,
	}

	if a.StartedAt != nil {
		t := a.StartedAt.Format(time.RFC3339)
		resp.StartedAt = &t
	}
	if a.FinishedAt != nil {
		t := a.FinishedAt.Format(time.RFC3339)
		resp.FinishedAt = &t
	}
	if a.Error != nil {
		resp.Error = a.Error.Error()
	}

	resp.Duration = a.Duration().Round(time.Second).String()

	if includeLines > 0 {
		output := a.GetOutput()
		if len(output) > includeLines {
			output = output[len(output)-includeLines:]
		}
		resp.LogLines = output
	}

	return resp
}

func (s *Server) statusHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}

		tasks, err := s.store.ListTasks(nil)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}

		var status StatusResponse
		status.Total = len(tasks)

		for _, t := range tasks {
			switch t.Status {
			case domain.StatusNotStarted:
				status.NotStarted++
			case domain.StatusInProgress:
				status.InProgress++
			case domain.StatusComplete:
				status.Complete++
			}
		}

		if s.agents != nil {
			status.Agents = s.agents.RunningCount()
		}

		writeJSON(w, status)
	}
}

func (s *Server) listTasksHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}

		tasks, err := s.store.ListTasks(nil)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}

		responses := make([]TaskResponse, len(tasks))
		for i, t := range tasks {
			responses[i] = taskToResponse(t)
		}

		writeJSON(w, responses)
	}
}

func (s *Server) getTaskHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}

		// Extract task ID from path: /api/tasks/{module}/E{num}
		path := strings.TrimPrefix(r.URL.Path, "/api/tasks/")
		if path == "" {
			writeError(w, http.StatusBadRequest, "task ID required")
			return
		}

		task, err := s.store.GetTask(path)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if task == nil {
			writeError(w, http.StatusNotFound, "task not found")
			return
		}

		writeJSON(w, taskToResponse(task))
	}
}

func (s *Server) listAgentsHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}

		if s.agents == nil {
			writeJSON(w, []AgentResponse{})
			return
		}

		agents := s.agents.GetAll()
		resp := make([]AgentResponse, 0, len(agents))
		for _, a := range agents {
			resp = append(resp, agentToResponse(a, 10))
		}

		writeJSON(w, resp)
	}
}

func (s *Server) getAgentHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}

		if s.agents == nil {
			writeError(w, http.StatusServiceUnavailable, "agent manager not available")
			return
		}

		// Extract task ID from path: /api/agents/{module/Enn}
		taskID := strings.TrimPrefix(r.URL.Path, "/api/agents/")
		if taskID == "" || strings.HasSuffix(taskID, "/") {
			writeError(w, http.StatusBadRequest, "task ID required")
			return
		}

		// Remove trailing action paths like /stop, /resume, /logs
		if idx := strings.LastIndex(taskID, "/"); idx > 0 {
			taskID = taskID[:idx]
		}

		agent := s.agents.Get(taskID)
		if agent == nil {
			writeError(w, http.StatusNotFound, "agent not found")
			return
		}

		writeJSON(w, agentToResponse(agent, 50))
	}
}

func (s *Server) stopAgentHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}

		if s.agents == nil {
			writeError(w, http.StatusServiceUnavailable, "agent manager not available")
			return
		}

		// Extract task ID: /api/agents/{taskID}/stop
		path := strings.TrimPrefix(r.URL.Path, "/api/agents/")
		path = strings.TrimSuffix(path, "/stop")
		taskID := path

		agent := s.agents.Get(taskID)
		if agent == nil {
			writeError(w, http.StatusNotFound, "agent not found")
			return
		}

		agent.Stop()

		s.Broadcast(SSEEvent{Type: "agent_update", Data: agentToResponse(agent, 0)})

		writeJSON(w, map[string]string{"status": "stopped"})
	}
}

func (s *Server) resumeAgentHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}

		if s.agents == nil {
			writeError(w, http.StatusServiceUnavailable, "agent manager not available")
			return
		}

		// Extract task ID: /api/agents/{taskID}/resume
		path := strings.TrimPrefix(r.URL.Path, "/api/agents/")
		path = strings.TrimSuffix(path, "/resume")
		taskID := path

		agent := s.agents.Get(taskID)
		if agent == nil {
			writeError(w, http.StatusNotFound, "agent not found")
			return
		}

		go func() {
			ctx := context.Background()
			if err := agent.Resume(ctx); err != nil {
				s.Broadcast(SSEEvent{Type: "agent_update", Data: agentToResponse(agent, 0)})
			}
		}()

		writeJSON(w, map[string]string{"status": "resuming"})
	}
}

func (s *Server) agentLogsHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}

		if s.agents == nil {
			writeError(w, http.StatusServiceUnavailable, "agent manager not available")
			return
		}

		// Extract task ID: /api/agents/{taskID}/logs
		path := strings.TrimPrefix(r.URL.Path, "/api/agents/")
		path = strings.TrimSuffix(path, "/logs")
		taskID := path

		agent := s.agents.Get(taskID)
		if agent == nil {
			writeError(w, http.StatusNotFound, "agent not found")
			return
		}

		// Load full output
		_ = agent.LoadOutput(1000)
		output := agent.GetOutput()

		writeJSON(w, map[string]interface{}{
			"task_id": taskID,
			"lines":   output,
		})
	}
}

func (s *Server) batchStatusHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}

		s.batchMu.RLock()
		resp := BatchStatusResponse{
			Running: s.batchRunning,
			Paused:  s.batchPaused,
			Auto:    s.autoMode,
		}
		s.batchMu.RUnlock()

		writeJSON(w, resp)
	}
}

func (s *Server) batchStartHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}

		s.batchMu.Lock()
		s.batchRunning = true
		s.batchPaused = false
		resp := BatchStatusResponse{
			Running: true,
			Paused:  false,
			Auto:    s.autoMode,
		}
		s.batchMu.Unlock()

		s.Broadcast(SSEEvent{Type: "batch_update", Data: resp})

		writeJSON(w, map[string]string{"status": "started"})
	}
}

func (s *Server) batchStopHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}

		s.batchMu.Lock()
		s.batchRunning = false
		s.batchPaused = false
		resp := BatchStatusResponse{
			Running: false,
			Paused:  false,
			Auto:    s.autoMode,
		}
		s.batchMu.Unlock()

		s.Broadcast(SSEEvent{Type: "batch_update", Data: resp})

		writeJSON(w, map[string]string{"status": "stopped"})
	}
}

func (s *Server) batchPauseHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}

		s.batchMu.Lock()
		s.batchPaused = true
		resp := BatchStatusResponse{
			Running: s.batchRunning,
			Paused:  true,
			Auto:    s.autoMode,
		}
		s.batchMu.Unlock()

		s.Broadcast(SSEEvent{Type: "batch_update", Data: resp})

		writeJSON(w, map[string]string{"status": "paused"})
	}
}

func (s *Server) batchResumeHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}

		s.batchMu.Lock()
		s.batchPaused = false
		resp := BatchStatusResponse{
			Running: s.batchRunning,
			Paused:  false,
			Auto:    s.autoMode,
		}
		s.batchMu.Unlock()

		s.Broadcast(SSEEvent{Type: "batch_update", Data: resp})

		writeJSON(w, map[string]string{"status": "resumed"})
	}
}

func (s *Server) batchAutoHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}

		s.batchMu.Lock()
		s.autoMode = !s.autoMode
		resp := BatchStatusResponse{
			Running: s.batchRunning,
			Paused:  s.batchPaused,
			Auto:    s.autoMode,
		}
		s.batchMu.Unlock()

		s.Broadcast(SSEEvent{Type: "batch_update", Data: resp})

		writeJSON(w, map[string]bool{"auto": resp.Auto})
	}
}
