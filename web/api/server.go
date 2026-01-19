package api

import (
	"encoding/json"
	"io/fs"
	"net/http"
	"strings"
	"sync"
	"time"

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
	ListFlaggedPRs() ([]*PRRecord, error)
	GetPR(taskID string) (*PRRecord, error)
	UpdatePRStatus(taskID string, status string) error
	GetGroupPriorities() (map[string]int, error)
	SetGroupPriority(group string, priority int) error
	RemoveGroupPriority(group string) error
	GetGroupsWithTaskCounts() ([]GroupStats, error)
}

// PRRecord represents a PR flagged for review
type PRRecord struct {
	TaskID     string
	PRNumber   int
	Title      string
	FlagReason string
	CreatedAt  time.Time
	Status     string
}

// GroupStats represents a group with task counts
type GroupStats struct {
	Name      string
	Priority  int
	Total     int
	Completed int
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
	s.mux.HandleFunc("/api/events", s.sseHandler())

	// Agent routes
	s.mux.HandleFunc("/api/agents", s.listAgentsHandler())
	s.mux.Handle("/api/agents/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		switch {
		case strings.HasSuffix(path, "/stop"):
			s.stopAgentHandler().ServeHTTP(w, r)
		case strings.HasSuffix(path, "/resume"):
			s.resumeAgentHandler().ServeHTTP(w, r)
		case strings.HasSuffix(path, "/logs"):
			s.agentLogsHandler().ServeHTTP(w, r)
		default:
			s.getAgentHandler().ServeHTTP(w, r)
		}
	}))

	// Batch control routes
	s.mux.HandleFunc("/api/batch/status", s.batchStatusHandler())
	s.mux.HandleFunc("/api/batch/start", s.batchStartHandler())
	s.mux.HandleFunc("/api/batch/stop", s.batchStopHandler())
	s.mux.HandleFunc("/api/batch/pause", s.batchPauseHandler())
	s.mux.HandleFunc("/api/batch/resume", s.batchResumeHandler())
	s.mux.HandleFunc("/api/batch/auto", s.batchAutoHandler())

	// PR routes
	s.mux.HandleFunc("/api/prs", s.listPRsHandler())
	s.mux.Handle("/api/prs/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/merge") {
			s.mergePRHandler().ServeHTTP(w, r)
		} else {
			writeError(w, http.StatusNotFound, "not found")
		}
	}))

	// Group priority routes
	s.mux.HandleFunc("/api/groups", s.listGroupsHandler())
	s.mux.Handle("/api/groups/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/priority") {
			switch r.Method {
			case http.MethodPut:
				s.setGroupPriorityHandler().ServeHTTP(w, r)
			case http.MethodDelete:
				s.deleteGroupPriorityHandler().ServeHTTP(w, r)
			default:
				writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			}
		} else {
			writeError(w, http.StatusNotFound, "not found")
		}
	}))

	// Static files with SPA fallback
	buildFS, _ := fs.Sub(ui.BuildFS, "build")
	fileServer := http.FileServer(http.FS(buildFS))
	s.mux.Handle("/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Try to serve the file
		path := r.URL.Path
		if path != "/" && !strings.HasPrefix(path, "/api/") {
			// Check if file exists
			if _, err := fs.Stat(buildFS, strings.TrimPrefix(path, "/")); err != nil {
				// File doesn't exist, serve index.html for SPA routing
				r.URL.Path = "/"
			}
		}
		fileServer.ServeHTTP(w, r)
	}))
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
