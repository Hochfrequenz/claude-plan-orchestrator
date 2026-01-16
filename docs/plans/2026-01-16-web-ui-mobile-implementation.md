# Web UI Mobile Enhancement Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Extend the web UI to provide mobile-friendly access for status monitoring and quick actions (batch control, agent management, PR review, group priorities).

**Architecture:** REST API expansion with new endpoints for batch/agent/PR/group operations. Svelte frontend with bottom tab navigation, responsive layout, and SSE for real-time updates. Backend receives AgentManager, Scheduler, and PRBot dependencies.

**Tech Stack:** Go (backend), Svelte 5 (frontend), SSE (real-time), CSS media queries (responsive)

---

## Task 1: Expand Server Dependencies

Add Scheduler and PRBot to the web API server struct so handlers can control batch execution and PR operations.

**Files:**
- Modify: `web/api/server.go:19-39`

**Step 1: Update Server struct and constructor**

In `web/api/server.go`, update the Server struct to include new dependencies:

```go
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
```

Update the constructor:

```go
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
```

Add imports at top of file:

```go
import (
	"sync"

	"github.com/anthropics/claude-plan-orchestrator/internal/executor"
	"github.com/anthropics/claude-plan-orchestrator/internal/prbot"
	"github.com/anthropics/claude-plan-orchestrator/internal/scheduler"
)
```

**Step 2: Update server initialization in commands.go**

In `cmd/claude-orch/commands.go`, find `runServe` function and update the server creation:

```go
server := api.NewServer(adapter, agentMgr, sched, prBot, addr)
```

Note: This requires passing agentMgr, scheduler, and prbot which are created earlier in the TUI setup. For now, pass nil for scheduler and prbot - we'll wire them up after testing the API structure.

**Step 3: Verify compilation**

Run: `go build ./cmd/claude-orch`
Expected: Compiles without errors

**Step 4: Commit**

```bash
git add web/api/server.go cmd/claude-orch/commands.go
git commit -m "feat(api): expand server dependencies for batch/PR control"
```

---

## Task 2: Add Batch Control Endpoints

Add REST endpoints for starting, stopping, pausing, and resuming batch execution.

**Files:**
- Modify: `web/api/handlers.go`
- Modify: `web/api/server.go` (routes)

**Step 1: Add batch response types**

In `web/api/handlers.go`, add after the existing response types:

```go
type BatchStatusResponse struct {
	Running bool `json:"running"`
	Paused  bool `json:"paused"`
	Auto    bool `json:"auto"`
}
```

**Step 2: Add batch status handler**

```go
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
```

**Step 3: Add batch start handler**

```go
func (s *Server) batchStartHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}

		s.batchMu.Lock()
		s.batchRunning = true
		s.batchPaused = false
		s.batchMu.Unlock()

		s.Broadcast(SSEEvent{Type: "batch_update", Data: BatchStatusResponse{
			Running: true,
			Paused:  false,
			Auto:    s.autoMode,
		}})

		writeJSON(w, map[string]string{"status": "started"})
	}
}
```

**Step 4: Add batch stop handler**

```go
func (s *Server) batchStopHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}

		s.batchMu.Lock()
		s.batchRunning = false
		s.batchPaused = false
		s.batchMu.Unlock()

		s.Broadcast(SSEEvent{Type: "batch_update", Data: BatchStatusResponse{
			Running: false,
			Paused:  false,
			Auto:    s.autoMode,
		}})

		writeJSON(w, map[string]string{"status": "stopped"})
	}
}
```

**Step 5: Add batch pause handler**

```go
func (s *Server) batchPauseHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}

		s.batchMu.Lock()
		s.batchPaused = true
		s.batchMu.Unlock()

		s.Broadcast(SSEEvent{Type: "batch_update", Data: BatchStatusResponse{
			Running: s.batchRunning,
			Paused:  true,
			Auto:    s.autoMode,
		}})

		writeJSON(w, map[string]string{"status": "paused"})
	}
}
```

**Step 6: Add batch resume handler**

```go
func (s *Server) batchResumeHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}

		s.batchMu.Lock()
		s.batchPaused = false
		s.batchMu.Unlock()

		s.Broadcast(SSEEvent{Type: "batch_update", Data: BatchStatusResponse{
			Running: s.batchRunning,
			Paused:  false,
			Auto:    s.autoMode,
		}})

		writeJSON(w, map[string]string{"status": "resumed"})
	}
}
```

**Step 7: Add auto mode toggle handler**

```go
func (s *Server) batchAutoHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}

		s.batchMu.Lock()
		s.autoMode = !s.autoMode
		autoMode := s.autoMode
		s.batchMu.Unlock()

		s.Broadcast(SSEEvent{Type: "batch_update", Data: BatchStatusResponse{
			Running: s.batchRunning,
			Paused:  s.batchPaused,
			Auto:    autoMode,
		}})

		writeJSON(w, map[string]bool{"auto": autoMode})
	}
}
```

**Step 8: Register batch routes**

In `web/api/server.go` `setupRoutes()`, add:

```go
// Batch control routes
s.mux.HandleFunc("/api/batch/status", s.batchStatusHandler())
s.mux.HandleFunc("/api/batch/start", s.batchStartHandler())
s.mux.HandleFunc("/api/batch/stop", s.batchStopHandler())
s.mux.HandleFunc("/api/batch/pause", s.batchPauseHandler())
s.mux.HandleFunc("/api/batch/resume", s.batchResumeHandler())
s.mux.HandleFunc("/api/batch/auto", s.batchAutoHandler())
```

**Step 9: Verify compilation**

Run: `go build ./cmd/claude-orch`
Expected: Compiles without errors

**Step 10: Commit**

```bash
git add web/api/handlers.go web/api/server.go
git commit -m "feat(api): add batch control endpoints"
```

---

## Task 3: Add Agent Control Endpoints

Add endpoints for listing agents with details, stopping running agents, and resuming failed agents.

**Files:**
- Modify: `web/api/handlers.go`
- Modify: `web/api/server.go` (routes)

**Step 1: Add agent response types**

In `web/api/handlers.go`, replace the existing `AgentResponse` with:

```go
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
```

**Step 2: Add helper to convert agent to response**

```go
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
```

Add import for `time` at top of file.

**Step 3: Update list agents handler**

Replace the existing `listAgentsHandler`:

```go
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
```

**Step 4: Add get single agent handler**

```go
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
```

**Step 5: Add stop agent handler**

```go
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
```

**Step 6: Add resume agent handler**

```go
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
```

Add import for `context` and `strings` at top of file.

**Step 7: Add agent logs handler**

```go
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
```

**Step 8: Register agent routes**

In `web/api/server.go` `setupRoutes()`, update agent routes:

```go
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
```

Add import for `strings` at top of server.go.

**Step 9: Verify compilation**

Run: `go build ./cmd/claude-orch`
Expected: Compiles without errors

**Step 10: Commit**

```bash
git add web/api/handlers.go web/api/server.go
git commit -m "feat(api): add agent control endpoints (list, stop, resume, logs)"
```

---

## Task 4: Add PR Management Endpoints

Add endpoints for listing flagged PRs and merging them.

**Files:**
- Modify: `web/api/handlers.go`
- Modify: `web/api/server.go` (routes)
- Modify: `web/api/server.go` (Store interface)

**Step 1: Extend Store interface**

In `web/api/server.go`, update the Store interface:

```go
type Store interface {
	ListTasks(opts interface{}) ([]*domain.Task, error)
	GetTask(id string) (*domain.Task, error)
	ListFlaggedPRs() ([]*PRRecord, error)
	GetPR(taskID string) (*PRRecord, error)
	UpdatePRStatus(taskID string, status string) error
}

type PRRecord struct {
	TaskID     string
	PRNumber   int
	Title      string
	FlagReason string
	CreatedAt  time.Time
	Status     string
}
```

**Step 2: Add PR response types**

In `web/api/handlers.go`:

```go
type PRResponse struct {
	TaskID     string `json:"task_id"`
	PRNumber   int    `json:"pr_number"`
	Title      string `json:"title"`
	FlagReason string `json:"flag_reason"`
	CreatedAt  string `json:"created_at"`
	Status     string `json:"status"`
	URL        string `json:"url"`
}
```

**Step 3: Add list PRs handler**

```go
func (s *Server) listPRsHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}

		prs, err := s.store.ListFlaggedPRs()
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}

		resp := make([]PRResponse, 0, len(prs))
		for _, pr := range prs {
			resp = append(resp, PRResponse{
				TaskID:     pr.TaskID,
				PRNumber:   pr.PRNumber,
				Title:      pr.Title,
				FlagReason: pr.FlagReason,
				CreatedAt:  pr.CreatedAt.Format(time.RFC3339),
				Status:     pr.Status,
				URL:        fmt.Sprintf("https://github.com/OWNER/REPO/pull/%d", pr.PRNumber),
			})
		}

		writeJSON(w, resp)
	}
}
```

Add import for `fmt` at top of file.

**Step 4: Add merge PR handler**

```go
func (s *Server) mergePRHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}

		if s.prbot == nil {
			writeError(w, http.StatusServiceUnavailable, "PR bot not available")
			return
		}

		// Extract PR number: /api/prs/{prNumber}/merge
		path := strings.TrimPrefix(r.URL.Path, "/api/prs/")
		path = strings.TrimSuffix(path, "/merge")
		prNumber, err := strconv.Atoi(path)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid PR number")
			return
		}

		if err := s.prbot.MergePR(prNumber); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}

		s.Broadcast(SSEEvent{Type: "pr_update", Data: map[string]interface{}{
			"pr_number": prNumber,
			"status":    "merged",
		}})

		writeJSON(w, map[string]string{"status": "merged"})
	}
}
```

Add import for `strconv` at top of file.

**Step 5: Register PR routes**

In `web/api/server.go` `setupRoutes()`:

```go
// PR routes
s.mux.HandleFunc("/api/prs", s.listPRsHandler())
s.mux.Handle("/api/prs/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
	if strings.HasSuffix(r.URL.Path, "/merge") {
		s.mergePRHandler().ServeHTTP(w, r)
	} else {
		writeError(w, http.StatusNotFound, "not found")
	}
}))
```

**Step 6: Verify compilation**

Run: `go build ./cmd/claude-orch`
Expected: Compiles without errors (may need Store adapter updates)

**Step 7: Commit**

```bash
git add web/api/handlers.go web/api/server.go
git commit -m "feat(api): add PR management endpoints (list, merge)"
```

---

## Task 5: Add Group Priority Endpoints

Add endpoints for viewing and modifying group priority tiers.

**Files:**
- Modify: `web/api/handlers.go`
- Modify: `web/api/server.go` (routes, Store interface)

**Step 1: Extend Store interface for groups**

In `web/api/server.go`, add to Store interface:

```go
type Store interface {
	// ... existing methods ...
	GetGroupPriorities() (map[string]int, error)
	SetGroupPriority(group string, priority int) error
	RemoveGroupPriority(group string) error
	GetGroupsWithTaskCounts() ([]GroupStats, error)
}

type GroupStats struct {
	Name      string
	Priority  int
	Total     int
	Completed int
}
```

**Step 2: Add group response types**

In `web/api/handlers.go`:

```go
type GroupResponse struct {
	Name      string `json:"name"`
	Priority  int    `json:"priority"`
	Total     int    `json:"total"`
	Completed int    `json:"completed"`
}
```

**Step 3: Add list groups handler**

```go
func (s *Server) listGroupsHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}

		groups, err := s.store.GetGroupsWithTaskCounts()
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}

		resp := make([]GroupResponse, 0, len(groups))
		for _, g := range groups {
			resp = append(resp, GroupResponse{
				Name:      g.Name,
				Priority:  g.Priority,
				Total:     g.Total,
				Completed: g.Completed,
			})
		}

		writeJSON(w, resp)
	}
}
```

**Step 4: Add set group priority handler**

```go
func (s *Server) setGroupPriorityHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}

		// Extract group name: /api/groups/{name}/priority
		path := strings.TrimPrefix(r.URL.Path, "/api/groups/")
		path = strings.TrimSuffix(path, "/priority")
		groupName := path

		var req struct {
			Priority int `json:"priority"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}

		if err := s.store.SetGroupPriority(groupName, req.Priority); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}

		s.Broadcast(SSEEvent{Type: "group_update", Data: map[string]interface{}{
			"name":     groupName,
			"priority": req.Priority,
		}})

		writeJSON(w, map[string]string{"status": "updated"})
	}
}
```

Add import for `encoding/json` at top of file.

**Step 5: Add delete group priority handler**

```go
func (s *Server) deleteGroupPriorityHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}

		// Extract group name: /api/groups/{name}/priority
		path := strings.TrimPrefix(r.URL.Path, "/api/groups/")
		path = strings.TrimSuffix(path, "/priority")
		groupName := path

		if err := s.store.RemoveGroupPriority(groupName); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}

		s.Broadcast(SSEEvent{Type: "group_update", Data: map[string]interface{}{
			"name":     groupName,
			"priority": -1,
		}})

		writeJSON(w, map[string]string{"status": "removed"})
	}
}
```

**Step 6: Register group routes**

In `web/api/server.go` `setupRoutes()`:

```go
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
```

**Step 7: Verify compilation**

Run: `go build ./cmd/claude-orch`
Expected: Compiles without errors

**Step 8: Commit**

```bash
git add web/api/handlers.go web/api/server.go
git commit -m "feat(api): add group priority endpoints (list, set, delete)"
```

---

## Task 6: Update Store Adapter

Create an adapter that implements the expanded Store interface.

**Files:**
- Modify: `cmd/claude-orch/commands.go`

**Step 1: Expand storeAdapter**

In `cmd/claude-orch/commands.go`, find the `storeAdapter` struct and expand it:

```go
type storeAdapter struct {
	store *taskstore.Store
}

func (a *storeAdapter) ListTasks(opts interface{}) ([]*domain.Task, error) {
	return a.store.ListTasks(taskstore.ListOptions{})
}

func (a *storeAdapter) GetTask(id string) (*domain.Task, error) {
	return a.store.GetTask(id)
}

func (a *storeAdapter) ListFlaggedPRs() ([]*api.PRRecord, error) {
	// TODO: Implement when PR table exists
	return []*api.PRRecord{}, nil
}

func (a *storeAdapter) GetPR(taskID string) (*api.PRRecord, error) {
	// TODO: Implement when PR table exists
	return nil, fmt.Errorf("not found")
}

func (a *storeAdapter) UpdatePRStatus(taskID string, status string) error {
	// TODO: Implement when PR table exists
	return nil
}

func (a *storeAdapter) GetGroupPriorities() (map[string]int, error) {
	return a.store.GetGroupPriorities()
}

func (a *storeAdapter) SetGroupPriority(group string, priority int) error {
	return a.store.SetGroupPriority(group, priority)
}

func (a *storeAdapter) RemoveGroupPriority(group string) error {
	return a.store.RemoveGroupPriority(group)
}

func (a *storeAdapter) GetGroupsWithTaskCounts() ([]api.GroupStats, error) {
	stats, err := a.store.GetGroupsWithTaskCounts()
	if err != nil {
		return nil, err
	}
	result := make([]api.GroupStats, len(stats))
	for i, s := range stats {
		result[i] = api.GroupStats{
			Name:      s.Name,
			Priority:  s.Priority,
			Total:     s.Total,
			Completed: s.Completed,
		}
	}
	return result, nil
}
```

**Step 2: Verify compilation**

Run: `go build ./cmd/claude-orch`
Expected: Compiles without errors

**Step 3: Commit**

```bash
git add cmd/claude-orch/commands.go
git commit -m "feat(api): expand store adapter for groups and PRs"
```

---

## Task 7: Create Svelte Component Structure

Set up the component file structure and bottom navigation.

**Files:**
- Create: `web/ui/src/components/BottomNav.svelte`
- Create: `web/ui/src/components/Dashboard.svelte`
- Create: `web/ui/src/components/Agents.svelte`
- Create: `web/ui/src/components/PRs.svelte`
- Create: `web/ui/src/components/Groups.svelte`
- Modify: `web/ui/src/App.svelte`

**Step 1: Create BottomNav component**

Create `web/ui/src/components/BottomNav.svelte`:

```svelte
<script>
  export let activeTab = 'dashboard'
  export let onTabChange = () => {}

  const tabs = [
    { id: 'dashboard', label: 'Dashboard', icon: '📊' },
    { id: 'agents', label: 'Agents', icon: '🤖' },
    { id: 'prs', label: 'PRs', icon: '🔀' },
    { id: 'groups', label: 'Groups', icon: '📁' },
  ]
</script>

<nav class="bottom-nav">
  {#each tabs as tab}
    <button
      class="tab"
      class:active={activeTab === tab.id}
      on:click={() => onTabChange(tab.id)}
    >
      <span class="icon">{tab.icon}</span>
      <span class="label">{tab.label}</span>
    </button>
  {/each}
</nav>

<style>
  .bottom-nav {
    position: fixed;
    bottom: 0;
    left: 0;
    right: 0;
    display: flex;
    background: #fff;
    border-top: 1px solid #ddd;
    padding: 8px 0;
    padding-bottom: max(8px, env(safe-area-inset-bottom));
    z-index: 100;
  }

  .tab {
    flex: 1;
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: 4px;
    padding: 8px;
    border: none;
    background: none;
    cursor: pointer;
    color: #666;
    font-size: 12px;
  }

  .tab.active {
    color: #0066cc;
  }

  .icon {
    font-size: 20px;
  }

  .label {
    font-size: 11px;
  }

  @media (min-width: 768px) {
    .bottom-nav {
      position: static;
      flex-direction: column;
      width: 80px;
      height: 100vh;
      border-top: none;
      border-right: 1px solid #ddd;
      padding: 16px 0;
    }

    .tab {
      padding: 16px 8px;
    }
  }
</style>
```

**Step 2: Create placeholder Dashboard component**

Create `web/ui/src/components/Dashboard.svelte`:

```svelte
<script>
  export let status = {}
  export let batchStatus = { running: false, paused: false, auto: false }
  export let onBatchAction = () => {}
</script>

<div class="dashboard">
  <div class="stats-grid">
    <div class="stat-card">
      <span class="value">{status.agents_running || 0}</span>
      <span class="label">Running</span>
    </div>
    <div class="stat-card">
      <span class="value">{status.complete || 0}/{status.total || 0}</span>
      <span class="label">Complete</span>
    </div>
    <div class="stat-card">
      <span class="value">{status.in_progress || 0}</span>
      <span class="label">In Progress</span>
    </div>
    <div class="stat-card">
      <span class="value">{status.not_started || 0}</span>
      <span class="label">Queued</span>
    </div>
  </div>

  <div class="batch-controls">
    <h3>Batch Execution</h3>
    <div class="batch-status">
      {#if batchStatus.running}
        {#if batchStatus.paused}
          <span class="badge paused">Paused</span>
        {:else}
          <span class="badge running">Running</span>
        {/if}
      {:else}
        <span class="badge idle">Idle</span>
      {/if}
    </div>
    <div class="batch-buttons">
      {#if !batchStatus.running}
        <button class="btn primary" on:click={() => onBatchAction('start')}>Start</button>
      {:else if batchStatus.paused}
        <button class="btn primary" on:click={() => onBatchAction('resume')}>Resume</button>
        <button class="btn" on:click={() => onBatchAction('stop')}>Stop</button>
      {:else}
        <button class="btn" on:click={() => onBatchAction('pause')}>Pause</button>
        <button class="btn danger" on:click={() => onBatchAction('stop')}>Stop</button>
      {/if}
    </div>
    <label class="auto-toggle">
      <input type="checkbox" checked={batchStatus.auto} on:change={() => onBatchAction('auto')} />
      Auto Mode
    </label>
  </div>
</div>

<style>
  .dashboard {
    padding: 16px;
  }

  .stats-grid {
    display: grid;
    grid-template-columns: repeat(2, 1fr);
    gap: 12px;
    margin-bottom: 24px;
  }

  .stat-card {
    background: #f5f5f5;
    border-radius: 8px;
    padding: 16px;
    text-align: center;
  }

  .stat-card .value {
    display: block;
    font-size: 24px;
    font-weight: bold;
    color: #333;
  }

  .stat-card .label {
    font-size: 12px;
    color: #666;
  }

  .batch-controls {
    background: #fff;
    border: 1px solid #ddd;
    border-radius: 8px;
    padding: 16px;
  }

  .batch-controls h3 {
    margin: 0 0 12px 0;
    font-size: 16px;
  }

  .batch-status {
    margin-bottom: 12px;
  }

  .badge {
    display: inline-block;
    padding: 4px 12px;
    border-radius: 12px;
    font-size: 12px;
    font-weight: 500;
  }

  .badge.running { background: #e3f2fd; color: #1976d2; }
  .badge.paused { background: #fff3e0; color: #f57c00; }
  .badge.idle { background: #f5f5f5; color: #666; }

  .batch-buttons {
    display: flex;
    gap: 8px;
    margin-bottom: 12px;
  }

  .btn {
    padding: 10px 20px;
    border: 1px solid #ddd;
    border-radius: 6px;
    background: #fff;
    cursor: pointer;
    font-size: 14px;
  }

  .btn.primary {
    background: #0066cc;
    color: #fff;
    border-color: #0066cc;
  }

  .btn.danger {
    color: #d32f2f;
    border-color: #d32f2f;
  }

  .auto-toggle {
    display: flex;
    align-items: center;
    gap: 8px;
    font-size: 14px;
  }

  @media (min-width: 768px) {
    .stats-grid {
      grid-template-columns: repeat(4, 1fr);
    }
  }
</style>
```

**Step 3: Create placeholder Agents component**

Create `web/ui/src/components/Agents.svelte`:

```svelte
<script>
  export let agents = []
  export let onAgentAction = () => {}

  let expandedId = null

  function toggleExpand(id) {
    expandedId = expandedId === id ? null : id
  }

  function getStatusColor(status) {
    switch (status) {
      case 'running': return '#1976d2'
      case 'completed': return '#388e3c'
      case 'failed': return '#d32f2f'
      case 'stuck': return '#f57c00'
      default: return '#666'
    }
  }
</script>

<div class="agents">
  <h2>Agents</h2>
  {#if agents.length === 0}
    <p class="empty">No active agents</p>
  {:else}
    {#each agents as agent}
      <div class="agent-card" class:expanded={expandedId === agent.id}>
        <button class="agent-header" on:click={() => toggleExpand(agent.id)}>
          <div class="agent-info">
            <span class="task-id">{agent.task_id}</span>
            <span class="status" style="color: {getStatusColor(agent.status)}">{agent.status}</span>
          </div>
          <span class="duration">{agent.duration}</span>
        </button>

        {#if expandedId === agent.id}
          <div class="agent-details">
            <div class="detail-row">
              <span>Tokens:</span>
              <span>{agent.tokens_input} in / {agent.tokens_output} out</span>
            </div>
            <div class="detail-row">
              <span>Cost:</span>
              <span>${agent.cost_usd.toFixed(4)}</span>
            </div>
            {#if agent.log_lines && agent.log_lines.length > 0}
              <div class="log-preview">
                {#each agent.log_lines.slice(-5) as line}
                  <div class="log-line">{line}</div>
                {/each}
              </div>
            {/if}
            <div class="agent-actions">
              {#if agent.status === 'running' || agent.status === 'stuck'}
                <button class="btn danger" on:click={() => onAgentAction(agent.task_id, 'stop')}>Stop</button>
              {/if}
              {#if agent.status === 'failed'}
                <button class="btn primary" on:click={() => onAgentAction(agent.task_id, 'resume')}>Resume</button>
              {/if}
              <a href="/logs/{encodeURIComponent(agent.task_id)}" target="_blank" class="btn">Full Logs</a>
            </div>
          </div>
        {/if}
      </div>
    {/each}
  {/if}
</div>

<style>
  .agents {
    padding: 16px;
  }

  h2 {
    margin: 0 0 16px 0;
    font-size: 18px;
  }

  .empty {
    color: #666;
    text-align: center;
    padding: 32px;
  }

  .agent-card {
    background: #fff;
    border: 1px solid #ddd;
    border-radius: 8px;
    margin-bottom: 8px;
    overflow: hidden;
  }

  .agent-header {
    width: 100%;
    display: flex;
    justify-content: space-between;
    align-items: center;
    padding: 12px 16px;
    border: none;
    background: none;
    cursor: pointer;
    text-align: left;
  }

  .agent-info {
    display: flex;
    flex-direction: column;
    gap: 4px;
  }

  .task-id {
    font-weight: 500;
  }

  .status {
    font-size: 12px;
    text-transform: uppercase;
  }

  .duration {
    color: #666;
    font-size: 14px;
  }

  .agent-details {
    padding: 0 16px 16px;
    border-top: 1px solid #eee;
  }

  .detail-row {
    display: flex;
    justify-content: space-between;
    padding: 8px 0;
    font-size: 14px;
  }

  .log-preview {
    background: #1e1e1e;
    color: #d4d4d4;
    font-family: monospace;
    font-size: 11px;
    padding: 8px;
    border-radius: 4px;
    margin: 8px 0;
    max-height: 120px;
    overflow-y: auto;
  }

  .log-line {
    white-space: pre-wrap;
    word-break: break-all;
  }

  .agent-actions {
    display: flex;
    gap: 8px;
    margin-top: 12px;
  }

  .btn {
    padding: 8px 16px;
    border: 1px solid #ddd;
    border-radius: 6px;
    background: #fff;
    cursor: pointer;
    font-size: 13px;
    text-decoration: none;
    color: inherit;
  }

  .btn.primary {
    background: #0066cc;
    color: #fff;
    border-color: #0066cc;
  }

  .btn.danger {
    color: #d32f2f;
    border-color: #d32f2f;
  }
</style>
```

**Step 4: Create PRs component**

Create `web/ui/src/components/PRs.svelte`:

```svelte
<script>
  export let prs = []
  export let onMerge = () => {}

  let expandedId = null
  let confirmingMerge = null

  function toggleExpand(id) {
    expandedId = expandedId === id ? null : id
    confirmingMerge = null
  }

  function handleMerge(pr) {
    if (confirmingMerge === pr.pr_number) {
      onMerge(pr.pr_number)
      confirmingMerge = null
    } else {
      confirmingMerge = pr.pr_number
    }
  }

  function getFlagColor(reason) {
    switch (reason) {
      case 'security': return '#d32f2f'
      case 'architecture': return '#7b1fa2'
      case 'migration': return '#f57c00'
      default: return '#666'
    }
  }
</script>

<div class="prs">
  <h2>Flagged PRs</h2>
  {#if prs.length === 0}
    <p class="empty">No PRs need review</p>
  {:else}
    {#each prs as pr}
      <div class="pr-card" class:expanded={expandedId === pr.pr_number}>
        <button class="pr-header" on:click={() => toggleExpand(pr.pr_number)}>
          <div class="pr-info">
            <span class="pr-number">#{pr.pr_number}</span>
            <span class="task-id">{pr.task_id}</span>
          </div>
          <span class="flag-badge" style="background: {getFlagColor(pr.flag_reason)}">{pr.flag_reason}</span>
        </button>

        {#if expandedId === pr.pr_number}
          <div class="pr-details">
            <p class="pr-title">{pr.title}</p>
            <div class="pr-actions">
              <button
                class="btn"
                class:confirming={confirmingMerge === pr.pr_number}
                on:click={() => handleMerge(pr)}
              >
                {confirmingMerge === pr.pr_number ? 'Confirm Merge' : 'Merge'}
              </button>
              <a href={pr.url} target="_blank" class="btn">View on GitHub</a>
            </div>
            {#if confirmingMerge === pr.pr_number}
              <p class="confirm-hint">Tap again to confirm merge</p>
            {/if}
          </div>
        {/if}
      </div>
    {/each}
  {/if}
</div>

<style>
  .prs {
    padding: 16px;
  }

  h2 {
    margin: 0 0 16px 0;
    font-size: 18px;
  }

  .empty {
    color: #666;
    text-align: center;
    padding: 32px;
  }

  .pr-card {
    background: #fff;
    border: 1px solid #ddd;
    border-radius: 8px;
    margin-bottom: 8px;
    overflow: hidden;
  }

  .pr-header {
    width: 100%;
    display: flex;
    justify-content: space-between;
    align-items: center;
    padding: 12px 16px;
    border: none;
    background: none;
    cursor: pointer;
    text-align: left;
  }

  .pr-info {
    display: flex;
    gap: 8px;
    align-items: center;
  }

  .pr-number {
    font-weight: 500;
  }

  .task-id {
    color: #666;
    font-size: 13px;
  }

  .flag-badge {
    color: #fff;
    font-size: 11px;
    padding: 3px 8px;
    border-radius: 10px;
    text-transform: uppercase;
  }

  .pr-details {
    padding: 0 16px 16px;
    border-top: 1px solid #eee;
  }

  .pr-title {
    margin: 12px 0;
    font-size: 14px;
  }

  .pr-actions {
    display: flex;
    gap: 8px;
  }

  .btn {
    padding: 8px 16px;
    border: 1px solid #ddd;
    border-radius: 6px;
    background: #fff;
    cursor: pointer;
    font-size: 13px;
    text-decoration: none;
    color: inherit;
  }

  .btn.confirming {
    background: #0066cc;
    color: #fff;
    border-color: #0066cc;
  }

  .confirm-hint {
    font-size: 12px;
    color: #666;
    margin-top: 8px;
  }
</style>
```

**Step 5: Create Groups component**

Create `web/ui/src/components/Groups.svelte`:

```svelte
<script>
  export let groups = []
  export let onPriorityChange = () => {}

  // Group by priority tier
  $: groupedByTier = groups.reduce((acc, g) => {
    const tier = g.priority < 0 ? 'unassigned' : g.priority
    if (!acc[tier]) acc[tier] = []
    acc[tier].push(g)
    return acc
  }, {})

  $: sortedTiers = Object.keys(groupedByTier)
    .filter(t => t !== 'unassigned')
    .map(Number)
    .sort((a, b) => a - b)

  function changePriority(group, delta) {
    const newPriority = Math.max(0, group.priority + delta)
    onPriorityChange(group.name, newPriority)
  }

  function unassign(group) {
    onPriorityChange(group.name, -1)
  }
</script>

<div class="groups">
  <h2>Group Priorities</h2>

  {#each sortedTiers as tier}
    <div class="tier">
      <h3>Tier {tier} {tier === 0 ? '(runs first)' : ''}</h3>
      {#each groupedByTier[tier] as group}
        <div class="group-row">
          <span class="group-name">{group.name}</span>
          <div class="progress-bar">
            <div class="progress-fill" style="width: {(group.completed / group.total) * 100}%"></div>
          </div>
          <span class="progress-text">{group.completed}/{group.total}</span>
          <div class="priority-controls">
            <button class="tier-btn" on:click={() => changePriority(group, -1)} disabled={group.priority === 0}>↑</button>
            <button class="tier-btn" on:click={() => changePriority(group, 1)}>↓</button>
            <button class="tier-btn unassign" on:click={() => unassign(group)}>×</button>
          </div>
        </div>
      {/each}
    </div>
  {/each}

  {#if groupedByTier['unassigned']?.length > 0}
    <div class="tier unassigned">
      <h3>Unassigned</h3>
      {#each groupedByTier['unassigned'] as group}
        <div class="group-row">
          <span class="group-name">{group.name}</span>
          <div class="progress-bar">
            <div class="progress-fill" style="width: {(group.completed / group.total) * 100}%"></div>
          </div>
          <span class="progress-text">{group.completed}/{group.total}</span>
          <div class="priority-controls">
            <button class="tier-btn" on:click={() => onPriorityChange(group.name, 0)}>+ Add to Tier 0</button>
          </div>
        </div>
      {/each}
    </div>
  {/if}
</div>

<style>
  .groups {
    padding: 16px;
  }

  h2 {
    margin: 0 0 16px 0;
    font-size: 18px;
  }

  .tier {
    margin-bottom: 24px;
  }

  .tier h3 {
    font-size: 14px;
    color: #666;
    margin: 0 0 8px 0;
  }

  .group-row {
    display: flex;
    align-items: center;
    gap: 12px;
    padding: 12px;
    background: #fff;
    border: 1px solid #ddd;
    border-radius: 8px;
    margin-bottom: 8px;
  }

  .group-name {
    font-weight: 500;
    min-width: 100px;
  }

  .progress-bar {
    flex: 1;
    height: 8px;
    background: #eee;
    border-radius: 4px;
    overflow: hidden;
  }

  .progress-fill {
    height: 100%;
    background: #4caf50;
    transition: width 0.3s;
  }

  .progress-text {
    font-size: 13px;
    color: #666;
    min-width: 50px;
    text-align: right;
  }

  .priority-controls {
    display: flex;
    gap: 4px;
  }

  .tier-btn {
    width: 32px;
    height: 32px;
    border: 1px solid #ddd;
    border-radius: 6px;
    background: #fff;
    cursor: pointer;
    font-size: 14px;
  }

  .tier-btn:disabled {
    opacity: 0.3;
    cursor: not-allowed;
  }

  .tier-btn.unassign {
    color: #d32f2f;
  }

  .unassigned .tier-btn {
    width: auto;
    padding: 0 12px;
    font-size: 12px;
  }
</style>
```

**Step 6: Commit component files**

```bash
git add web/ui/src/components/
git commit -m "feat(ui): add mobile-friendly component structure"
```

---

## Task 8: Update App.svelte with Navigation

Wire up the components and navigation in the main App.

**Files:**
- Modify: `web/ui/src/App.svelte`
- Modify: `web/ui/src/lib/api.js`

**Step 1: Extend api.js with new endpoints**

In `web/ui/src/lib/api.js`:

```javascript
const API_BASE = '/api'

export async function fetchStatus() {
  const res = await fetch(`${API_BASE}/status`)
  return res.json()
}

export async function fetchTasks(params) {
  const url = new URL(`${API_BASE}/tasks`, window.location.origin)
  if (params) {
    Object.entries(params).forEach(([k, v]) => url.searchParams.set(k, v))
  }
  const res = await fetch(url)
  return res.json()
}

export async function fetchTask(id) {
  const res = await fetch(`${API_BASE}/tasks/${encodeURIComponent(id)}`)
  return res.json()
}

// Batch control
export async function fetchBatchStatus() {
  const res = await fetch(`${API_BASE}/batch/status`)
  return res.json()
}

export async function batchStart() {
  const res = await fetch(`${API_BASE}/batch/start`, { method: 'POST' })
  return res.json()
}

export async function batchStop() {
  const res = await fetch(`${API_BASE}/batch/stop`, { method: 'POST' })
  return res.json()
}

export async function batchPause() {
  const res = await fetch(`${API_BASE}/batch/pause`, { method: 'POST' })
  return res.json()
}

export async function batchResume() {
  const res = await fetch(`${API_BASE}/batch/resume`, { method: 'POST' })
  return res.json()
}

export async function batchToggleAuto() {
  const res = await fetch(`${API_BASE}/batch/auto`, { method: 'POST' })
  return res.json()
}

// Agents
export async function fetchAgents() {
  const res = await fetch(`${API_BASE}/agents`)
  return res.json()
}

export async function stopAgent(taskId) {
  const res = await fetch(`${API_BASE}/agents/${encodeURIComponent(taskId)}/stop`, { method: 'POST' })
  return res.json()
}

export async function resumeAgent(taskId) {
  const res = await fetch(`${API_BASE}/agents/${encodeURIComponent(taskId)}/resume`, { method: 'POST' })
  return res.json()
}

export async function fetchAgentLogs(taskId) {
  const res = await fetch(`${API_BASE}/agents/${encodeURIComponent(taskId)}/logs`)
  return res.json()
}

// PRs
export async function fetchPRs() {
  const res = await fetch(`${API_BASE}/prs`)
  return res.json()
}

export async function mergePR(prNumber) {
  const res = await fetch(`${API_BASE}/prs/${prNumber}/merge`, { method: 'POST' })
  return res.json()
}

// Groups
export async function fetchGroups() {
  const res = await fetch(`${API_BASE}/groups`)
  return res.json()
}

export async function setGroupPriority(name, priority) {
  const res = await fetch(`${API_BASE}/groups/${encodeURIComponent(name)}/priority`, {
    method: priority < 0 ? 'DELETE' : 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: priority >= 0 ? JSON.stringify({ priority }) : undefined,
  })
  return res.json()
}

// SSE
export function createEventSource() {
  return new EventSource(`${API_BASE}/events`)
}
```

**Step 2: Update App.svelte**

Replace `web/ui/src/App.svelte`:

```svelte
<script>
  import { onMount, onDestroy } from 'svelte'
  import BottomNav from './components/BottomNav.svelte'
  import Dashboard from './components/Dashboard.svelte'
  import Agents from './components/Agents.svelte'
  import PRs from './components/PRs.svelte'
  import Groups from './components/Groups.svelte'
  import {
    fetchStatus,
    fetchBatchStatus,
    fetchAgents,
    fetchPRs,
    fetchGroups,
    batchStart,
    batchStop,
    batchPause,
    batchResume,
    batchToggleAuto,
    stopAgent,
    resumeAgent,
    mergePR,
    setGroupPriority,
    createEventSource
  } from './lib/api.js'

  let activeTab = 'dashboard'
  let status = {}
  let batchStatus = { running: false, paused: false, auto: false }
  let agents = []
  let prs = []
  let groups = []
  let eventSource = null

  onMount(async () => {
    await loadData()
    setupSSE()
  })

  onDestroy(() => {
    if (eventSource) {
      eventSource.close()
    }
  })

  async function loadData() {
    const results = await Promise.all([
      fetchStatus(),
      fetchBatchStatus().catch(() => ({ running: false, paused: false, auto: false })),
      fetchAgents().catch(() => []),
      fetchPRs().catch(() => []),
      fetchGroups().catch(() => []),
    ])
    status = results[0]
    batchStatus = results[1]
    agents = results[2]
    prs = results[3]
    groups = results[4]
  }

  function setupSSE() {
    eventSource = createEventSource()
    eventSource.onmessage = (event) => {
      const data = JSON.parse(event.data)
      handleEvent(data)
    }
    eventSource.onerror = () => {
      setTimeout(setupSSE, 5000)
    }
  }

  function handleEvent(event) {
    switch (event.type) {
      case 'status_update':
        status = event.data
        break
      case 'batch_update':
        batchStatus = event.data
        break
      case 'agent_update':
        const idx = agents.findIndex(a => a.task_id === event.data.task_id)
        if (idx >= 0) {
          agents[idx] = { ...agents[idx], ...event.data }
          agents = agents
        } else {
          agents = [...agents, event.data]
        }
        break
      case 'pr_update':
        if (event.data.status === 'merged') {
          prs = prs.filter(p => p.pr_number !== event.data.pr_number)
        }
        break
      case 'group_update':
        const gIdx = groups.findIndex(g => g.name === event.data.name)
        if (gIdx >= 0) {
          groups[gIdx].priority = event.data.priority
          groups = groups
        }
        break
    }
  }

  async function handleBatchAction(action) {
    switch (action) {
      case 'start': await batchStart(); break
      case 'stop': await batchStop(); break
      case 'pause': await batchPause(); break
      case 'resume': await batchResume(); break
      case 'auto': await batchToggleAuto(); break
    }
  }

  async function handleAgentAction(taskId, action) {
    if (action === 'stop') {
      await stopAgent(taskId)
    } else if (action === 'resume') {
      await resumeAgent(taskId)
    }
  }

  async function handleMergePR(prNumber) {
    await mergePR(prNumber)
  }

  async function handlePriorityChange(name, priority) {
    await setGroupPriority(name, priority)
    groups = await fetchGroups()
  }
</script>

<div class="app" class:desktop={typeof window !== 'undefined' && window.innerWidth >= 768}>
  <BottomNav {activeTab} onTabChange={(tab) => activeTab = tab} />

  <main class="content">
    {#if activeTab === 'dashboard'}
      <Dashboard {status} {batchStatus} onBatchAction={handleBatchAction} />
    {:else if activeTab === 'agents'}
      <Agents {agents} onAgentAction={handleAgentAction} />
    {:else if activeTab === 'prs'}
      <PRs {prs} onMerge={handleMergePR} />
    {:else if activeTab === 'groups'}
      <Groups {groups} onPriorityChange={handlePriorityChange} />
    {/if}
  </main>
</div>

<style>
  :global(body) {
    margin: 0;
    font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif;
    background: #f5f5f5;
  }

  .app {
    min-height: 100vh;
    padding-bottom: 72px;
  }

  .content {
    max-width: 800px;
    margin: 0 auto;
  }

  @media (min-width: 768px) {
    .app {
      display: flex;
      padding-bottom: 0;
    }

    .content {
      flex: 1;
      margin-left: 80px;
      max-width: none;
      padding: 16px;
    }
  }
</style>
```

**Step 3: Verify frontend builds**

Run:
```bash
cd web/ui && npm install && npm run build
```
Expected: Build succeeds

**Step 4: Commit**

```bash
git add web/ui/src/App.svelte web/ui/src/lib/api.js
git commit -m "feat(ui): wire up navigation and data loading"
```

---

## Task 9: Add Full Logs Page

Create a dedicated page for viewing full agent logs.

**Files:**
- Create: `web/ui/src/routes/logs.svelte` (or handle in App.svelte with routing)
- Modify: `web/api/server.go` (add logs page route)

**Step 1: Add logs page handler in Go**

Since we're serving a SPA, we need to handle the `/logs/*` route to serve the same index.html. In `web/api/server.go`, update static file handling:

```go
func (s *Server) setupRoutes() {
	// API routes
	s.mux.HandleFunc("/api/status", s.statusHandler())
	s.mux.HandleFunc("/api/tasks", s.listTasksHandler())
	s.mux.HandleFunc("/api/tasks/", s.getTaskHandler())
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
	s.mux.HandleFunc("/api/events", s.sseHandler())

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
```

**Step 2: Add routing to App.svelte**

Update `web/ui/src/App.svelte` to handle `/logs/:taskId` route:

Add to the script section:

```svelte
<script>
  // ... existing imports ...
  import { fetchAgentLogs } from './lib/api.js'

  // ... existing state ...
  let currentRoute = window.location.pathname
  let logsTaskId = null
  let logsContent = []
  let logsLoading = false

  // Check for logs route on mount
  onMount(async () => {
    if (currentRoute.startsWith('/logs/')) {
      logsTaskId = decodeURIComponent(currentRoute.replace('/logs/', ''))
      await loadLogs()
    } else {
      await loadData()
      setupSSE()
    }
  })

  async function loadLogs() {
    logsLoading = true
    try {
      const data = await fetchAgentLogs(logsTaskId)
      logsContent = data.lines || []
    } catch (e) {
      logsContent = ['Error loading logs: ' + e.message]
    }
    logsLoading = false
  }

  // ... rest of existing code ...
</script>
```

Update the template:

```svelte
{#if logsTaskId}
  <div class="logs-page">
    <header class="logs-header">
      <a href="/" class="back-btn">← Back</a>
      <h1>Logs: {logsTaskId}</h1>
    </header>
    <div class="logs-content">
      {#if logsLoading}
        <p>Loading...</p>
      {:else}
        {#each logsContent as line}
          <div class="log-line">{line}</div>
        {/each}
      {/if}
    </div>
  </div>
{:else}
  <!-- existing app content -->
  <div class="app" ...>
    ...
  </div>
{/if}
```

Add styles:

```svelte
<style>
  /* ... existing styles ... */

  .logs-page {
    min-height: 100vh;
    background: #1e1e1e;
    color: #d4d4d4;
  }

  .logs-header {
    display: flex;
    align-items: center;
    gap: 16px;
    padding: 16px;
    background: #2d2d2d;
    border-bottom: 1px solid #404040;
  }

  .logs-header h1 {
    margin: 0;
    font-size: 16px;
    font-weight: normal;
  }

  .back-btn {
    color: #569cd6;
    text-decoration: none;
  }

  .logs-content {
    padding: 16px;
    font-family: 'SF Mono', Monaco, 'Courier New', monospace;
    font-size: 12px;
    line-height: 1.5;
    overflow-x: auto;
  }

  .log-line {
    white-space: pre-wrap;
    word-break: break-all;
  }
</style>
```

**Step 3: Build and verify**

Run:
```bash
cd web/ui && npm run build
```
Expected: Build succeeds

**Step 4: Commit**

```bash
git add web/api/server.go web/ui/src/App.svelte
git commit -m "feat(ui): add full logs page with SPA routing"
```

---

## Task 10: Add Responsive Styles

Ensure all components work well on mobile and desktop.

**Files:**
- Modify: `web/ui/src/app.css` (or add global styles)

**Step 1: Create/update global styles**

Create `web/ui/src/app.css`:

```css
/* Reset and base styles */
*, *::before, *::after {
  box-sizing: border-box;
}

html {
  -webkit-text-size-adjust: 100%;
}

body {
  margin: 0;
  font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, Oxygen, Ubuntu, sans-serif;
  font-size: 16px;
  line-height: 1.5;
  background: #f5f5f5;
  color: #333;
}

/* Touch-friendly targets */
button, a, input, select {
  min-height: 44px;
  min-width: 44px;
}

/* Safe area insets for notched phones */
@supports (padding: max(0px)) {
  .app {
    padding-left: max(16px, env(safe-area-inset-left));
    padding-right: max(16px, env(safe-area-inset-right));
  }
}

/* Prevent text selection on buttons */
button {
  -webkit-user-select: none;
  user-select: none;
}

/* Smooth scrolling */
html {
  scroll-behavior: smooth;
}

/* Focus styles for accessibility */
:focus-visible {
  outline: 2px solid #0066cc;
  outline-offset: 2px;
}

/* Responsive typography */
@media (max-width: 480px) {
  body {
    font-size: 14px;
  }
}
```

**Step 2: Import in main.js**

Update `web/ui/src/main.js`:

```javascript
import './app.css'
import App from './App.svelte'

const app = new App({
  target: document.getElementById('app'),
})

export default app
```

**Step 3: Build and verify**

Run:
```bash
cd web/ui && npm run build
```
Expected: Build succeeds

**Step 4: Commit**

```bash
git add web/ui/src/app.css web/ui/src/main.js
git commit -m "feat(ui): add responsive global styles"
```

---

## Task 11: Integration Testing

Test the complete flow manually.

**Step 1: Build backend**

Run: `go build -o claude-orch ./cmd/claude-orch`
Expected: Compiles without errors

**Step 2: Build frontend**

Run: `cd web/ui && npm run build`
Expected: Build succeeds

**Step 3: Start server**

Run: `./claude-orch serve`
Expected: Server starts on configured port

**Step 4: Test in browser**

Open browser to `http://localhost:8080`

Verify:
- [ ] Bottom navigation appears
- [ ] Dashboard shows status cards
- [ ] Batch controls work (start/pause/stop)
- [ ] Agents tab lists any active agents
- [ ] Agent expansion shows details
- [ ] PRs tab shows flagged PRs (if any)
- [ ] Groups tab shows priority tiers
- [ ] Priority up/down buttons work
- [ ] Mobile view (resize to <768px) shows bottom nav
- [ ] Desktop view (>768px) shows side nav

**Step 5: Test on mobile**

Use browser dev tools mobile emulation or actual device.

Verify:
- [ ] Touch targets are large enough
- [ ] Scrolling works smoothly
- [ ] Cards expand/collapse correctly
- [ ] Buttons respond to taps

**Step 6: Final commit**

```bash
git add -A
git commit -m "feat: complete web UI mobile enhancement"
```

---

## Summary

This plan implements:
1. **Backend**: Expanded REST API with batch, agent, PR, and group endpoints
2. **Frontend**: Mobile-first Svelte components with bottom navigation
3. **Real-time**: SSE events for all state changes
4. **Responsive**: CSS media queries for mobile/tablet/desktop

Total tasks: 11
Estimated commits: 11
