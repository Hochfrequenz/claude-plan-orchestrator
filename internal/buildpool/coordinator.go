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
		config.HeartbeatTimeout = 10 * time.Second
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

	for {
		_, message, err := conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				log.Printf("read error: %v", err)
			}
			return
		}

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

// Start starts the coordinator server
func (c *Coordinator) Start(ctx context.Context) error {
	mux := http.NewServeMux()
	mux.HandleFunc("/ws", c.HandleWebSocket)

	addr := fmt.Sprintf(":%d", c.config.WebSocketPort)
	c.server = &http.Server{
		Addr:    addr,
		Handler: mux,
	}

	go c.heartbeatLoop(ctx)

	log.Printf("coordinator listening on %s", addr)
	return c.server.ListenAndServe()
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
	ping, _ := buildprotocol.MarshalEnvelope(buildprotocol.TypePing, nil)

	for _, w := range c.registry.All() {
		// Check for heartbeat timeout
		lastHeartbeat := w.GetLastHeartbeat()
		if !lastHeartbeat.IsZero() && time.Since(lastHeartbeat) > c.config.HeartbeatTimeout {
			log.Printf("worker %s heartbeat timeout, evicting", w.ID)
			c.registry.Unregister(w.ID)
			c.dispatcher.RequeueWorkerJobs(w.ID)
			w.Conn.Close()
			continue
		}

		if err := w.WriteMessage(websocket.TextMessage, ping); err != nil {
			log.Printf("ping to %s failed: %v", w.ID, err)
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
