// internal/buildworker/client_test.go
package buildworker

import (
	"testing"
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
