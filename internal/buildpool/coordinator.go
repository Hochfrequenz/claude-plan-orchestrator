// internal/buildpool/coordinator.go
package buildpool

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/hochfrequenz/claude-plan-orchestrator/internal/buildprotocol"
)

// CoordinatorConfig configures the coordinator
type CoordinatorConfig struct {
	WebSocketPort     int
	GitDaemonPort     int
	HeartbeatInterval time.Duration
	HeartbeatTimeout  time.Duration
}

// Coordinator manages workers and dispatches jobs
type Coordinator struct {
	config     CoordinatorConfig
	registry   *Registry
	dispatcher *Dispatcher
	upgrader   websocket.Upgrader

	server *http.Server
	mu     sync.Mutex

	// Output accumulator for streaming output from workers
	outputMu     sync.Mutex
	outputBuffer map[string]*strings.Builder
}

// NewCoordinator creates a new coordinator
func NewCoordinator(config CoordinatorConfig, registry *Registry, dispatcher *Dispatcher) *Coordinator {
	if config.HeartbeatInterval == 0 {
		config.HeartbeatInterval = 30 * time.Second
	}
	if config.HeartbeatTimeout == 0 {
		config.HeartbeatTimeout = 90 * time.Second // Allow missing 2 heartbeats before disconnect
	}

	c := &Coordinator{
		config:     config,
		registry:   registry,
		dispatcher: dispatcher,
		upgrader: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool { return true },
		},
		outputBuffer: make(map[string]*strings.Builder),
	}

	c.dispatcher.SetSendFunc(c.sendJobToWorker)
	c.dispatcher.SetCancelFunc(c.sendCancelToWorker)

	return c
}

// Registry returns the worker registry
func (c *Coordinator) Registry() *Registry {
	return c.registry
}

// Dispatcher returns the job dispatcher
func (c *Coordinator) Dispatcher() *Dispatcher {
	return c.dispatcher
}

// HandleWebSocket handles incoming WebSocket connections from workers
func (c *Coordinator) HandleWebSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := c.upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("upgrade failed: %v", err)
		return
	}

	// Handle this connection
	go c.handleWorkerConnection(conn)
}

func (c *Coordinator) handleWorkerConnection(conn *websocket.Conn) {
	var workerID string
	defer func() {
		conn.Close()
		if workerID != "" {
			c.registry.Unregister(workerID)
			c.dispatcher.RequeueWorkerJobs(workerID)
			c.dispatcher.TryDispatch()
			log.Printf("worker %s disconnected", workerID)
		}
	}()

	// Set up WebSocket-level pong handler to extend read deadline
	conn.SetReadDeadline(time.Now().Add(c.config.HeartbeatTimeout))
	conn.SetPongHandler(func(string) error {
		conn.SetReadDeadline(time.Now().Add(c.config.HeartbeatTimeout))
		return nil
	})

	for {
		_, message, err := conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				log.Printf("read error: %v", err)
			}
			return
		}

		// Extend read deadline on any message received
		conn.SetReadDeadline(time.Now().Add(c.config.HeartbeatTimeout))

		var env buildprotocol.EnvelopeRaw
		if err := json.Unmarshal(message, &env); err != nil {
			log.Printf("invalid message: %v", err)
			continue
		}

		switch env.Type {
		case buildprotocol.TypeRegister:
			var reg buildprotocol.RegisterMessage
			if err := json.Unmarshal(env.Payload, &reg); err != nil {
				log.Printf("invalid register: %v", err)
				continue
			}
			workerID = reg.WorkerID
			c.registry.Register(&ConnectedWorker{
				ID:      reg.WorkerID,
				MaxJobs: reg.MaxJobs,
				Slots:   reg.MaxJobs,
				Conn:    conn,
			})
			log.Printf("worker %s registered (max_jobs=%d)", reg.WorkerID, reg.MaxJobs)

		case buildprotocol.TypeReady:
			var ready buildprotocol.ReadyMessage
			if err := json.Unmarshal(env.Payload, &ready); err != nil {
				log.Printf("failed to unmarshal %s message: %v", env.Type, err)
				continue
			}
			if w := c.registry.Get(workerID); w != nil {
				w.UpdateSlots(ready.Slots)
				c.dispatcher.TryDispatch()
			}

		case buildprotocol.TypeOutput:
			var output buildprotocol.OutputMessage
			if err := json.Unmarshal(env.Payload, &output); err != nil {
				log.Printf("failed to unmarshal %s message: %v", env.Type, err)
				continue
			}
			c.AccumulateOutput(output.JobID, output.Stream, output.Data)

		case buildprotocol.TypeComplete:
			var complete buildprotocol.CompleteMessage
			if err := json.Unmarshal(env.Payload, &complete); err != nil {
				log.Printf("failed to unmarshal %s message: %v", env.Type, err)
				continue
			}
			output := c.GetAndClearOutput(complete.JobID)
			c.dispatcher.Complete(complete.JobID, &buildprotocol.JobResult{
				JobID:        complete.JobID,
				ExitCode:     complete.ExitCode,
				Output:       output,
				DurationSecs: float64(complete.DurationMs) / 1000,
			})

		case buildprotocol.TypeError:
			var errMsg buildprotocol.ErrorMessage
			if err := json.Unmarshal(env.Payload, &errMsg); err != nil {
				log.Printf("failed to unmarshal %s message: %v", env.Type, err)
				continue
			}
			output := c.GetAndClearOutput(errMsg.JobID)
			c.dispatcher.Complete(errMsg.JobID, &buildprotocol.JobResult{
				JobID:    errMsg.JobID,
				ExitCode: -1,
				Output:   output + "Error: " + errMsg.Message,
			})

		case buildprotocol.TypePong:
			if w := c.registry.Get(workerID); w != nil {
				w.SetLastHeartbeat(time.Now())
			}
		}
	}
}

func (c *Coordinator) sendJobToWorker(w *ConnectedWorker, job *buildprotocol.JobMessage) error {
	data, err := buildprotocol.MarshalEnvelope(buildprotocol.TypeJob, job)
	if err != nil {
		return err
	}
	return w.WriteMessage(websocket.TextMessage, data)
}

func (c *Coordinator) sendCancelToWorker(workerID, jobID string) error {
	w := c.registry.Get(workerID)
	if w == nil {
		return fmt.Errorf("worker %s not found", workerID)
	}

	data, err := buildprotocol.MarshalEnvelope(buildprotocol.TypeCancel, buildprotocol.CancelMessage{
		JobID: jobID,
	})
	if err != nil {
		return err
	}
	return w.WriteMessage(websocket.TextMessage, data)
}

// Start starts the coordinator server
func (c *Coordinator) Start(ctx context.Context) error {
	mux := http.NewServeMux()
	mux.HandleFunc("/ws", c.HandleWebSocket)
	mux.HandleFunc("/status", c.HandleStatus)
	mux.HandleFunc("/job", c.HandleJobSubmit)

	addr := fmt.Sprintf(":%d", c.config.WebSocketPort)
	c.server = &http.Server{
		Addr:    addr,
		Handler: mux,
	}

	go c.heartbeatLoop(ctx)

	log.Printf("coordinator listening on %s", addr)
	return c.server.ListenAndServe()
}

// HandleStatus returns the current status of workers and jobs
func (c *Coordinator) HandleStatus(w http.ResponseWriter, r *http.Request) {
	workers := []map[string]interface{}{}
	for _, worker := range c.registry.All() {
		maxJobs, slots, connectedAt := worker.GetStatus()
		workers = append(workers, map[string]interface{}{
			"id":              worker.ID,
			"max_jobs":        maxJobs,
			"active_jobs":     maxJobs - slots,
			"connected_since": connectedAt.Format(time.RFC3339),
		})
	}

	status := map[string]interface{}{
		"workers":               workers,
		"queued_jobs":           c.dispatcher.QueuedCount(),
		"local_fallback_active": c.dispatcher.LocalFallbackActive(),
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(status)
}

// JobRequest represents an HTTP job submission request
type JobRequest struct {
	Command string `json:"command"`
	Repo    string `json:"repo"`
	Commit  string `json:"commit"`
	Timeout int    `json:"timeout,omitempty"`
}

// JobResponse represents an HTTP job submission response
type JobResponse struct {
	JobID    string `json:"job_id"`
	ExitCode int    `json:"exit_code"`
	Output   string `json:"output"`
	Error    string `json:"error,omitempty"`
}

// HandleJobSubmit handles HTTP job submissions (POST /job)
func (c *Coordinator) HandleJobSubmit(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req JobRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body: "+err.Error(), http.StatusBadRequest)
		return
	}

	if req.Command == "" {
		http.Error(w, "command is required", http.StatusBadRequest)
		return
	}

	// Generate job ID
	jobID := fmt.Sprintf("http-%d", time.Now().UnixNano())

	// Create job
	job := &buildprotocol.JobMessage{
		JobID:   jobID,
		Repo:    req.Repo,
		Commit:  req.Commit,
		Command: req.Command,
		Timeout: req.Timeout,
	}

	// Submit to dispatcher
	resultCh := c.dispatcher.Submit(job)
	c.dispatcher.TryDispatch()

	// Wait for result (with timeout)
	timeout := time.Duration(req.Timeout) * time.Second
	if timeout == 0 {
		timeout = 5 * time.Minute
	}

	select {
	case result := <-resultCh:
		resp := JobResponse{
			JobID:    result.JobID,
			ExitCode: result.ExitCode,
			Output:   result.Output,
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	case <-time.After(timeout):
		http.Error(w, "job timed out", http.StatusGatewayTimeout)
	}
}

// Stop stops the coordinator server
func (c *Coordinator) Stop() error {
	if c.server != nil {
		return c.server.Close()
	}
	return nil
}

func (c *Coordinator) heartbeatLoop(ctx context.Context) {
	ticker := time.NewTicker(c.config.HeartbeatInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			c.sendHeartbeats()
		}
	}
}

func (c *Coordinator) sendHeartbeats() {
	for _, w := range c.registry.All() {
		// Send WebSocket protocol-level ping (not application-level)
		// This triggers the pong handler on the worker side, keeping the connection alive
		w.writeMu.Lock()
		w.Conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
		err := w.Conn.WriteMessage(websocket.PingMessage, nil)
		w.Conn.SetWriteDeadline(time.Time{}) // Clear deadline
		w.writeMu.Unlock()

		if err != nil {
			log.Printf("ping to %s failed: %v", w.ID, err)
			// Connection is broken, close it (the read loop will handle cleanup)
			w.Conn.Close()
		}
	}
}

// AccumulateOutput appends output for a job
func (c *Coordinator) AccumulateOutput(jobID, stream, data string) {
	c.outputMu.Lock()
	defer c.outputMu.Unlock()

	if c.outputBuffer[jobID] == nil {
		c.outputBuffer[jobID] = &strings.Builder{}
	}
	c.outputBuffer[jobID].WriteString(data)
}

// GetAndClearOutput returns accumulated output and clears the buffer
func (c *Coordinator) GetAndClearOutput(jobID string) string {
	c.outputMu.Lock()
	defer c.outputMu.Unlock()

	if buf, ok := c.outputBuffer[jobID]; ok {
		output := buf.String()
		delete(c.outputBuffer, jobID)
		return output
	}
	return ""
}
