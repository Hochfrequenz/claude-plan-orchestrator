// internal/buildworker/client_test.go
package buildworker

import (
	"context"
	"testing"
	"time"
)

func TestWorkerConfig_Validate(t *testing.T) {
	tests := []struct {
		name    string
		config  WorkerConfig
		wantErr bool
	}{
		{
			name: "valid config",
			config: WorkerConfig{
				ServerURL:   "wss://localhost:8080/ws",
				WorkerID:    "worker-1",
				MaxJobs:     4,
				GitCacheDir: "/var/cache/repos",
				WorktreeDir: "/tmp/jobs",
			},
			wantErr: false,
		},
		{
			name: "missing server URL",
			config: WorkerConfig{
				WorkerID: "worker-1",
				MaxJobs:  4,
			},
			wantErr: true,
		},
		{
			name: "invalid max jobs",
			config: WorkerConfig{
				ServerURL: "wss://localhost:8080/ws",
				MaxJobs:   0,
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestWorker_JobTracking(t *testing.T) {
	config := WorkerConfig{
		ServerURL: "ws://localhost:9999/ws", // Won't connect
		WorkerID:  "test",
		MaxJobs:   2,
	}

	w, err := NewWorker(config)
	if err != nil {
		t.Fatalf("NewWorker: %v", err)
	}

	// Track a job
	ctx, cancel := context.WithCancel(context.Background())
	w.TrackJob("job-1", cancel)

	if !w.HasJob("job-1") {
		t.Error("HasJob(job-1) = false, want true")
	}

	// Cancel the job
	w.CancelJob("job-1")

	// Verify context was cancelled
	select {
	case <-ctx.Done():
		// Expected
	case <-time.After(100 * time.Millisecond):
		t.Error("context was not cancelled")
	}

	// Verify job is untracked
	if w.HasJob("job-1") {
		t.Error("HasJob(job-1) after cancel = true, want false")
	}
}

func TestWorker_ReconnectBackoff(t *testing.T) {
	// Test backoff calculation
	delays := []time.Duration{
		calculateBackoff(0),
		calculateBackoff(1),
		calculateBackoff(2),
		calculateBackoff(3),
		calculateBackoff(10), // Should cap at max
	}

	if delays[0] != 1*time.Second {
		t.Errorf("backoff(0) = %v, want 1s", delays[0])
	}
	if delays[1] != 2*time.Second {
		t.Errorf("backoff(1) = %v, want 2s", delays[1])
	}
	if delays[2] != 4*time.Second {
		t.Errorf("backoff(2) = %v, want 4s", delays[2])
	}
	if delays[4] > 60*time.Second {
		t.Errorf("backoff(10) = %v, want <= 60s (capped)", delays[4])
	}
}
