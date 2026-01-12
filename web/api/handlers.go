package api

import (
	"net/http"
	"strings"

	"github.com/hochfrequenz/claude-plan-orchestrator/internal/domain"
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
	TaskID   string `json:"task_id"`
	Status   string `json:"status"`
	Duration string `json:"duration"`
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

		// Would return agent status from AgentManager
		writeJSON(w, []AgentResponse{})
	}
}
