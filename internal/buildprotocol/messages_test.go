// internal/buildprotocol/messages_test.go
package buildprotocol

import (
	"encoding/json"
	"testing"
)

func TestRegisterMessage_Marshal(t *testing.T) {
	msg := RegisterMessage{
		WorkerID: "worker-1",
		MaxJobs:  4,
	}

	data, err := json.Marshal(Envelope{Type: "register", Payload: msg})
	if err != nil {
		t.Fatal(err)
	}

	var env Envelope
	if err := json.Unmarshal(data, &env); err != nil {
		t.Fatal(err)
	}

	if env.Type != "register" {
		t.Errorf("got type %q, want %q", env.Type, "register")
	}
}

func TestJobMessage_Marshal(t *testing.T) {
	msg := JobMessage{
		JobID:   "job-123",
		Repo:    "git://central:9418/project",
		Commit:  "abc123",
		Command: "cargo test --lib",
	}

	data, err := json.Marshal(Envelope{Type: "job", Payload: msg})
	if err != nil {
		t.Fatal(err)
	}

	if len(data) == 0 {
		t.Error("expected non-empty JSON")
	}
}
