package batch

import (
	"fmt"
	"sync"
	"time"

	"github.com/robfig/cron/v3"
)

// Scheduler manages scheduled batch runs
type Scheduler struct {
	configs  map[string]BatchConfig
	parser   cron.Parser
	lastRun  map[string]time.Time
	running  map[string]bool
	mu       sync.RWMutex
	stopChan chan struct{}
}

// NewScheduler creates a new batch scheduler
func NewScheduler(configs []BatchConfig) (*Scheduler, error) {
	s := &Scheduler{
		configs:  make(map[string]BatchConfig),
		parser:   cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow),
		lastRun:  make(map[string]time.Time),
		running:  make(map[string]bool),
		stopChan: make(chan struct{}),
	}

	for _, cfg := range configs {
		if err := cfg.Validate(); err != nil {
			return nil, err
		}
		s.configs[cfg.Name] = cfg
	}

	return s, nil
}

// ParseCron parses a cron expression
func ParseCron(expr string) (cron.Schedule, error) {
	parser := cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)
	return parser.Parse(expr)
}

// NextRun returns the next scheduled run time for a batch
func (s *Scheduler) NextRun(name string) time.Time {
	s.mu.RLock()
	defer s.mu.RUnlock()

	cfg, ok := s.configs[name]
	if !ok {
		return time.Time{}
	}

	sched, err := s.parser.Parse(cfg.Cron)
	if err != nil {
		return time.Time{}
	}

	return sched.Next(time.Now())
}

// ShouldRun returns true if a batch should run now
func (s *Scheduler) ShouldRun(name string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()

	cfg, ok := s.configs[name]
	if !ok {
		return false
	}

	if s.running[name] {
		return false
	}

	sched, err := s.parser.Parse(cfg.Cron)
	if err != nil {
		return false
	}

	lastRun := s.lastRun[name]
	if lastRun.IsZero() {
		lastRun = time.Now().Add(-24 * time.Hour)
	}

	nextRun := sched.Next(lastRun)
	return time.Now().After(nextRun)
}

// MarkRunning marks a batch as currently running
func (s *Scheduler) MarkRunning(name string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.running[name] = true
}

// MarkComplete marks a batch as complete
func (s *Scheduler) MarkComplete(name string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.running[name] = false
	s.lastRun[name] = time.Now()
}

// GetConfig returns the config for a batch
func (s *Scheduler) GetConfig(name string) (BatchConfig, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	cfg, ok := s.configs[name]
	return cfg, ok
}

// ListBatches returns all batch names
func (s *Scheduler) ListBatches() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	names := make([]string, 0, len(s.configs))
	for name := range s.configs {
		names = append(names, name)
	}
	return names
}

// Start begins the scheduler loop
func (s *Scheduler) Start(runFunc func(BatchConfig) error) {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-s.stopChan:
			return
		case <-ticker.C:
			for name := range s.configs {
				if s.ShouldRun(name) {
					cfg, _ := s.GetConfig(name)
					s.MarkRunning(name)
					go func(c BatchConfig) {
						if err := runFunc(c); err != nil {
							fmt.Printf("Batch %s failed: %v\n", c.Name, err)
						}
						s.MarkComplete(c.Name)
					}(cfg)
				}
			}
		}
	}
}

// Stop stops the scheduler
func (s *Scheduler) Stop() {
	close(s.stopChan)
}
