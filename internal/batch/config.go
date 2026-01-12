package batch

import (
	"fmt"
	"os"
	"time"

	"github.com/pelletier/go-toml/v2"
)

// BatchConfig represents a scheduled batch configuration
type BatchConfig struct {
	Name             string        `toml:"name"`
	Cron             string        `toml:"cron"`
	MaxTasks         int           `toml:"max_tasks"`
	MaxDuration      time.Duration `toml:"max_duration"`
	NotifyOnComplete bool          `toml:"notify_on_complete"`
}

// ScheduleConfig holds all batch configurations
type ScheduleConfig struct {
	Batches []BatchConfig `toml:"batch"`
}

// Validate checks if the config is valid
func (c *BatchConfig) Validate() error {
	if c.Name == "" {
		return fmt.Errorf("batch name is required")
	}
	if c.Cron == "" {
		return fmt.Errorf("cron expression is required")
	}
	if _, err := ParseCron(c.Cron); err != nil {
		return fmt.Errorf("invalid cron expression: %w", err)
	}
	if c.MaxTasks <= 0 {
		c.MaxTasks = 10 // Default
	}
	if c.MaxDuration <= 0 {
		c.MaxDuration = 4 * time.Hour // Default
	}
	return nil
}

// LoadScheduleConfig loads batch configuration from a TOML file
func LoadScheduleConfig(path string) (*ScheduleConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &ScheduleConfig{}, nil
		}
		return nil, err
	}

	var cfg ScheduleConfig
	if err := toml.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}

	// Validate all batches
	for i := range cfg.Batches {
		if err := cfg.Batches[i].Validate(); err != nil {
			return nil, fmt.Errorf("batch %d: %w", i, err)
		}
	}

	return &cfg, nil
}
