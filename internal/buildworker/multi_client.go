// internal/buildworker/multi_client.go
package buildworker

import (
	"context"
	"fmt"
	"log"
	"sync"

	"golang.org/x/sync/errgroup"
)

// ServerConfig defines a connection to a single orchestrator
type ServerConfig struct {
	URL  string
	Name string // Optional, for logging
}

// MultiClientConfig configures the multi-orchestrator client
type MultiClientConfig struct {
	Servers     []ServerConfig
	WorkerID    string
	MaxJobs     int
	GitCacheDir string
	WorktreeDir string
	UseNixShell bool
	Debug       bool
}

// Validate checks the config is valid
func (c *MultiClientConfig) Validate() error {
	if len(c.Servers) == 0 {
		return fmt.Errorf("at least one server is required")
	}
	for i, srv := range c.Servers {
		if srv.URL == "" {
			return fmt.Errorf("server[%d].url is required", i)
		}
	}
	if c.MaxJobs <= 0 {
		return fmt.Errorf("max_jobs must be positive")
	}
	return nil
}

// MultiClient manages connections to multiple orchestrators
type MultiClient struct {
	config   MultiClientConfig
	pool     *Pool
	executor *Executor
	workers  []*Worker
	mu       sync.Mutex

	ctx    context.Context
	cancel context.CancelFunc
}

// NewMultiClient creates a new multi-orchestrator client
func NewMultiClient(config MultiClientConfig) (*MultiClient, error) {
	if err := config.Validate(); err != nil {
		return nil, err
	}

	ctx, cancel := context.WithCancel(context.Background())

	// Create shared pool and executor
	pool := NewPool(config.MaxJobs)
	executor := NewExecutor(ExecutorConfig{
		GitCacheDir: config.GitCacheDir,
		WorktreeDir: config.WorktreeDir,
		UseNixShell: config.UseNixShell,
		Debug:       config.Debug,
	})

	mc := &MultiClient{
		config:   config,
		pool:     pool,
		executor: executor,
		workers:  make([]*Worker, 0, len(config.Servers)),
		ctx:      ctx,
		cancel:   cancel,
	}

	// Create a worker for each server
	for _, srv := range config.Servers {
		name := srv.Name
		if name == "" {
			name = srv.URL
		}

		worker, err := NewWorkerWithSharedResources(WorkerConfig{
			ServerURL:   srv.URL,
			WorkerID:    config.WorkerID,
			MaxJobs:     config.MaxJobs,
			GitCacheDir: config.GitCacheDir,
			WorktreeDir: config.WorktreeDir,
			UseNixShell: config.UseNixShell,
			Debug:       config.Debug,
		}, pool, executor, name)
		if err != nil {
			cancel()
			return nil, fmt.Errorf("creating worker for %s: %w", name, err)
		}

		mc.workers = append(mc.workers, worker)
	}

	// Set up pool callback to broadcast ready messages to all workers
	pool.SetOnSlotsChanged(func(available int) {
		mc.broadcastReady()
	})

	return mc, nil
}

// broadcastReady sends a ReadyMessage to all connected orchestrators
func (mc *MultiClient) broadcastReady() {
	mc.mu.Lock()
	workers := make([]*Worker, len(mc.workers))
	copy(workers, mc.workers)
	mc.mu.Unlock()

	for _, w := range workers {
		if err := w.sendReadyIfConnected(); err != nil {
			if mc.config.Debug {
				log.Printf("[multi-client] failed to send ready to %s: %v", w.orchestratorName, err)
			}
		}
	}
}

// Run starts all workers concurrently and blocks until all are done or context is cancelled
func (mc *MultiClient) Run() error {
	g, ctx := errgroup.WithContext(mc.ctx)

	for _, worker := range mc.workers {
		w := worker // capture for goroutine
		g.Go(func() error {
			// Use the shared context so all workers stop together if context is cancelled
			return w.RunWithReconnectContext(ctx)
		})
	}

	// Wait for all workers (only returns error if all workers fail)
	return g.Wait()
}

// RunWithReconnect starts all workers with automatic reconnection
// Individual connection failures don't affect other connections
func (mc *MultiClient) RunWithReconnect() error {
	var wg sync.WaitGroup

	for _, worker := range mc.workers {
		wg.Add(1)
		go func(w *Worker) {
			defer wg.Done()
			// Each worker handles its own reconnection
			if err := w.RunWithReconnectContext(mc.ctx); err != nil {
				if mc.config.Debug {
					log.Printf("[multi-client] worker %s stopped: %v", w.orchestratorName, err)
				}
			}
		}(worker)
	}

	// Wait for context cancellation
	<-mc.ctx.Done()

	// Wait for all workers to finish
	wg.Wait()

	return nil
}

// Stop gracefully shuts down all workers
func (mc *MultiClient) Stop() {
	mc.cancel()

	mc.mu.Lock()
	defer mc.mu.Unlock()

	for _, w := range mc.workers {
		w.Stop()
	}
}

// ServerCount returns the number of configured servers
func (mc *MultiClient) ServerCount() int {
	return len(mc.workers)
}

// AvailableSlots returns the number of available job slots
func (mc *MultiClient) AvailableSlots() int {
	return mc.pool.Available()
}
