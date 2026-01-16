package api

import (
	"encoding/json"
	"io/fs"
	"net/http"
	"sync"

	"github.com/hochfrequenz/claude-plan-orchestrator/internal/domain"
	"github.com/hochfrequenz/claude-plan-orchestrator/internal/executor"
	"github.com/hochfrequenz/claude-plan-orchestrator/internal/prbot"
	"github.com/hochfrequenz/claude-plan-orchestrator/internal/scheduler"
	"github.com/hochfrequenz/claude-plan-orchestrator/web/ui"
)

// Store interface for database operations
type Store interface {
	ListTasks(opts interface{}) ([]*domain.Task, error)
	GetTask(id string) (*domain.Task, error)
}

// Server is the HTTP API server
type Server struct {
	store     Store
	agents    *executor.AgentManager
	scheduler *scheduler.Scheduler
	prbot     *prbot.PRBot
	addr      string
	mux       *http.ServeMux
	sseHub    *SSEHub

	// Batch state
	batchMu      sync.RWMutex
	batchRunning bool
	batchPaused  bool
	autoMode     bool
}

// NewServer creates a new API server
func NewServer(store Store, agents *executor.AgentManager, sched *scheduler.Scheduler, pr *prbot.PRBot, addr string) *Server {
	s := &Server{
		store:     store,
		agents:    agents,
		scheduler: sched,
		prbot:     pr,
		addr:      addr,
		mux:       http.NewServeMux(),
		sseHub:    NewSSEHub(),
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

	// Static files (embedded Svelte build output)
	buildFS, _ := fs.Sub(ui.BuildFS, "build")
	s.mux.Handle("/", http.FileServer(http.FS(buildFS)))
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
