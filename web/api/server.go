package api

import (
	"encoding/json"
	"net/http"

	"github.com/hochfrequenz/claude-plan-orchestrator/internal/domain"
	"github.com/hochfrequenz/claude-plan-orchestrator/internal/executor"
)

// Store interface for database operations
type Store interface {
	ListTasks(opts interface{}) ([]*domain.Task, error)
	GetTask(id string) (*domain.Task, error)
}

// Server is the HTTP API server
type Server struct {
	store  Store
	agents *executor.AgentManager
	addr   string
	mux    *http.ServeMux
	sseHub *SSEHub
}

// NewServer creates a new API server
func NewServer(store Store, agents *executor.AgentManager, addr string) *Server {
	s := &Server{
		store:  store,
		agents: agents,
		addr:   addr,
		mux:    http.NewServeMux(),
		sseHub: NewSSEHub(),
	}
	s.setupRoutes()
	return s
}

func (s *Server) setupRoutes() {
	// API routes
	s.mux.HandleFunc("/api/status", s.statusHandler())
	s.mux.HandleFunc("/api/tasks", s.listTasksHandler())
	s.mux.HandleFunc("/api/tasks/", s.getTaskHandler())
	s.mux.HandleFunc("/api/agents", s.listAgentsHandler())
	s.mux.HandleFunc("/api/events", s.sseHandler())

	// Static files (Svelte build output)
	s.mux.Handle("/", http.FileServer(http.Dir("web/ui/build")))
}

// Start starts the HTTP server
func (s *Server) Start() error {
	go s.sseHub.Run()
	return http.ListenAndServe(s.addr, s.mux)
}

// Broadcast sends an event to all SSE clients
func (s *Server) Broadcast(event SSEEvent) {
	s.sseHub.Broadcast(event)
}

func writeJSON(w http.ResponseWriter, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(data)
}

func writeError(w http.ResponseWriter, code int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{"error": message})
}
