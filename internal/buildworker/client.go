// internal/buildworker/client.go
package buildworker

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/hochfrequenz/claude-plan-orchestrator/internal/buildprotocol"
)

// WorkerConfig configures the worker client
type WorkerConfig struct {
	ServerURL   string
	WorkerID    string
	MaxJobs     int
	GitCacheDir string
	WorktreeDir string
	UseNixShell bool
}

// Validate checks the config is valid
func (c *WorkerConfig) Validate() error {
	if c.ServerURL == "" {
		return fmt.Errorf("server_url is required")
	}
	if c.MaxJobs <= 0 {
		return fmt.Errorf("max_jobs must be positive")
	}
	return nil
}

// Worker is a build agent that connects to a coordinator
type Worker struct {
	config   WorkerConfig
	pool     *Pool
	executor *Executor
	conn     *websocket.Conn
	mu       sync.Mutex

	// For graceful shutdown
	ctx    context.Context
	cancel context.CancelFunc

	// Job tracking for cancellation
	jobsMu sync.Mutex
	jobs   map[string]context.CancelFunc
}

// NewWorker creates a new worker client
func NewWorker(config WorkerConfig) (*Worker, error) {
	if err := config.Validate(); err != nil {
		return nil, err
	}

	ctx, cancel := context.WithCancel(context.Background())

	return &Worker{
		config: config,
		pool:   NewPool(config.MaxJobs),
		executor: NewExecutor(ExecutorConfig{
			GitCacheDir: config.GitCacheDir,
			WorktreeDir: config.WorktreeDir,
			UseNixShell: config.UseNixShell,
		}),
		ctx:    ctx,
		cancel: cancel,
		jobs:   make(map[string]context.CancelFunc),
	}, nil
}

// Connect establishes connection to the coordinator
func (w *Worker) Connect() error {
	conn, _, err := websocket.DefaultDialer.Dial(w.config.ServerURL, nil)
	if err != nil {
		return fmt.Errorf("dial failed: %w", err)
	}
	w.conn = conn

	// Send register message
	return w.send(buildprotocol.TypeRegister, buildprotocol.RegisterMessage{
		WorkerID: w.config.WorkerID,
		MaxJobs:  w.config.MaxJobs,
	})
}

// Run starts the worker loop
func (w *Worker) Run() error {
	// Send initial ready message
	if err := w.sendReady(); err != nil {
		return err
	}

	for {
		select {
		case <-w.ctx.Done():
			return nil
		default:
		}

		_, message, err := w.conn.ReadMessage()
		if err != nil {
			return fmt.Errorf("read failed: %w", err)
		}

		var env buildprotocol.EnvelopeRaw
		if err := json.Unmarshal(message, &env); err != nil {
			log.Printf("invalid message: %v", err)
			continue
		}

		switch env.Type {
		case buildprotocol.TypeJob:
			var job buildprotocol.JobMessage
			if err := json.Unmarshal(env.Payload, &job); err != nil {
				log.Printf("invalid job message: %v", err)
				continue
			}
			go w.handleJob(job)

		case buildprotocol.TypePing:
			w.send(buildprotocol.TypePong, nil)

		case buildprotocol.TypeCancel:
			var cancel buildprotocol.CancelMessage
			json.Unmarshal(env.Payload, &cancel)
			// TODO: implement job cancellation
		}
	}
}

func (w *Worker) handleJob(jobMsg buildprotocol.JobMessage) {
	if !w.pool.Acquire() {
		w.send(buildprotocol.TypeError, buildprotocol.ErrorMessage{
			JobID:   jobMsg.JobID,
			Message: "no slots available",
		})
		return
	}
	defer func() {
		w.pool.Release()
		w.UntrackJob(jobMsg.JobID)
		w.sendReady()
	}()

	timeout := time.Duration(jobMsg.Timeout) * time.Second
	if timeout == 0 {
		timeout = 5 * time.Minute
	}

	ctx, cancel := context.WithTimeout(w.ctx, timeout)
	defer cancel()

	// Track this job for cancellation
	w.TrackJob(jobMsg.JobID, cancel)

	job := Job{
		ID:      jobMsg.JobID,
		Repo:    jobMsg.Repo,
		Commit:  jobMsg.Commit,
		Command: jobMsg.Command,
		Env:     jobMsg.Env,
		Timeout: timeout,
	}

	result, err := w.executor.RunJob(ctx, job, func(stream, data string) {
		w.send(buildprotocol.TypeOutput, buildprotocol.OutputMessage{
			JobID:  jobMsg.JobID,
			Stream: stream,
			Data:   data,
		})
	})

	if err != nil {
		w.send(buildprotocol.TypeError, buildprotocol.ErrorMessage{
			JobID:   jobMsg.JobID,
			Message: err.Error(),
		})
		return
	}

	w.send(buildprotocol.TypeComplete, buildprotocol.CompleteMessage{
		JobID:      jobMsg.JobID,
		ExitCode:   result.ExitCode,
		DurationMs: int64(result.DurationSecs * 1000),
	})
}

func (w *Worker) sendReady() error {
	return w.send(buildprotocol.TypeReady, buildprotocol.ReadyMessage{
		Slots: w.pool.Available(),
	})
}

func (w *Worker) send(msgType string, payload interface{}) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	data, err := buildprotocol.MarshalEnvelope(msgType, payload)
	if err != nil {
		return err
	}
	return w.conn.WriteMessage(websocket.TextMessage, data)
}

// Stop gracefully shuts down the worker
func (w *Worker) Stop() {
	w.cancel()
	if w.conn != nil {
		w.conn.Close()
	}
}

// TrackJob registers a job's cancel function for later cancellation
func (w *Worker) TrackJob(jobID string, cancel context.CancelFunc) {
	w.jobsMu.Lock()
	defer w.jobsMu.Unlock()
	w.jobs[jobID] = cancel
}

// UntrackJob removes a job from tracking
func (w *Worker) UntrackJob(jobID string) {
	w.jobsMu.Lock()
	defer w.jobsMu.Unlock()
	delete(w.jobs, jobID)
}

// HasJob checks if a job is being tracked
func (w *Worker) HasJob(jobID string) bool {
	w.jobsMu.Lock()
	defer w.jobsMu.Unlock()
	_, ok := w.jobs[jobID]
	return ok
}

// CancelJob cancels a running job
func (w *Worker) CancelJob(jobID string) {
	w.jobsMu.Lock()
	cancel, ok := w.jobs[jobID]
	if ok {
		delete(w.jobs, jobID)
	}
	w.jobsMu.Unlock()

	if ok && cancel != nil {
		cancel()
	}
}
