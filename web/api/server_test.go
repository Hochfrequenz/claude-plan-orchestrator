package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/anthropics/erp-orchestrator/internal/domain"
)

func TestListTasksHandler(t *testing.T) {
	store := &mockStore{
		tasks: []*domain.Task{
			{ID: domain.TaskID{Module: "tech", EpicNum: 0}, Title: "Setup", Status: domain.StatusComplete},
			{ID: domain.TaskID{Module: "tech", EpicNum: 1}, Title: "Core", Status: domain.StatusNotStarted},
		},
	}

	server := NewServer(store, nil, ":8080")
	handler := server.listTasksHandler()

	req := httptest.NewRequest("GET", "/api/tasks", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Status = %d, want 200", w.Code)
	}

	var tasks []TaskResponse
	json.NewDecoder(w.Body).Decode(&tasks)

	if len(tasks) != 2 {
		t.Errorf("Task count = %d, want 2", len(tasks))
	}
}

func TestStatusHandler(t *testing.T) {
	store := &mockStore{
		tasks: []*domain.Task{
			{ID: domain.TaskID{Module: "tech", EpicNum: 0}, Status: domain.StatusComplete},
			{ID: domain.TaskID{Module: "tech", EpicNum: 1}, Status: domain.StatusInProgress},
			{ID: domain.TaskID{Module: "tech", EpicNum: 2}, Status: domain.StatusNotStarted},
		},
	}

	server := NewServer(store, nil, ":8080")
	handler := server.statusHandler()

	req := httptest.NewRequest("GET", "/api/status", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	var status StatusResponse
	json.NewDecoder(w.Body).Decode(&status)

	if status.Complete != 1 {
		t.Errorf("Complete = %d, want 1", status.Complete)
	}
	if status.InProgress != 1 {
		t.Errorf("InProgress = %d, want 1", status.InProgress)
	}
}

type mockStore struct {
	tasks []*domain.Task
}

func (m *mockStore) ListTasks(opts interface{}) ([]*domain.Task, error) {
	return m.tasks, nil
}

func (m *mockStore) GetTask(id string) (*domain.Task, error) {
	for _, t := range m.tasks {
		if t.ID.String() == id {
			return t, nil
		}
	}
	return nil, nil
}
