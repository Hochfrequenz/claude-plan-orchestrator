// internal/buildworker/multi_client_test.go
package buildworker

import (
	"testing"
)

func TestMultiClientConfig_Validate(t *testing.T) {
	tests := []struct {
		name    string
		config  MultiClientConfig
		wantErr bool
	}{
		{
			name: "valid_single_server",
			config: MultiClientConfig{
				Servers:  []ServerConfig{{URL: "ws://localhost:8081/ws"}},
				WorkerID: "test-worker",
				MaxJobs:  4,
			},
			wantErr: false,
		},
		{
			name: "valid_multiple_servers",
			config: MultiClientConfig{
				Servers: []ServerConfig{
					{URL: "ws://orch1:8081/ws", Name: "project-a"},
					{URL: "ws://orch2:8081/ws", Name: "project-b"},
				},
				WorkerID: "test-worker",
				MaxJobs:  4,
			},
			wantErr: false,
		},
		{
			name: "no_servers",
			config: MultiClientConfig{
				Servers:  []ServerConfig{},
				WorkerID: "test-worker",
				MaxJobs:  4,
			},
			wantErr: true,
		},
		{
			name: "server_without_url",
			config: MultiClientConfig{
				Servers:  []ServerConfig{{Name: "broken"}},
				WorkerID: "test-worker",
				MaxJobs:  4,
			},
			wantErr: true,
		},
		{
			name: "invalid_max_jobs",
			config: MultiClientConfig{
				Servers:  []ServerConfig{{URL: "ws://localhost:8081/ws"}},
				WorkerID: "test-worker",
				MaxJobs:  0,
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

func TestNewMultiClient(t *testing.T) {
	t.Run("creates_workers_for_each_server", func(t *testing.T) {
		client, err := NewMultiClient(MultiClientConfig{
			Servers: []ServerConfig{
				{URL: "ws://orch1:8081/ws", Name: "project-a"},
				{URL: "ws://orch2:8081/ws", Name: "project-b"},
				{URL: "ws://orch3:8081/ws"},
			},
			WorkerID: "test-worker",
			MaxJobs:  4,
		})
		if err != nil {
			t.Fatalf("NewMultiClient() error = %v", err)
		}
		defer client.Stop()

		if client.ServerCount() != 3 {
			t.Errorf("ServerCount() = %d, want 3", client.ServerCount())
		}
	})

	t.Run("shares_pool_capacity", func(t *testing.T) {
		client, err := NewMultiClient(MultiClientConfig{
			Servers: []ServerConfig{
				{URL: "ws://orch1:8081/ws"},
				{URL: "ws://orch2:8081/ws"},
			},
			WorkerID: "test-worker",
			MaxJobs:  4,
		})
		if err != nil {
			t.Fatalf("NewMultiClient() error = %v", err)
		}
		defer client.Stop()

		// Pool is shared, so total capacity should be MaxJobs
		if client.AvailableSlots() != 4 {
			t.Errorf("AvailableSlots() = %d, want 4", client.AvailableSlots())
		}
	})

	t.Run("validation_error", func(t *testing.T) {
		_, err := NewMultiClient(MultiClientConfig{
			Servers:  []ServerConfig{},
			WorkerID: "test-worker",
			MaxJobs:  4,
		})
		if err == nil {
			t.Error("NewMultiClient() expected error for empty servers")
		}
	})
}

func TestMultiClient_Stop(t *testing.T) {
	client, err := NewMultiClient(MultiClientConfig{
		Servers: []ServerConfig{
			{URL: "ws://orch1:8081/ws"},
		},
		WorkerID: "test-worker",
		MaxJobs:  4,
	})
	if err != nil {
		t.Fatalf("NewMultiClient() error = %v", err)
	}

	// Stop should not panic
	client.Stop()

	// Calling Stop again should not panic
	client.Stop()
}
