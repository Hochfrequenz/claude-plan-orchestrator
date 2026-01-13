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

func TestJobResult_ParseTestOutput(t *testing.T) {
	output := `running 5 tests
test test_one ... ok
test test_two ... ok
test test_three ... FAILED
test test_four ... ok
test test_five ... ignored

test result: FAILED. 3 passed; 1 failed; 1 ignored`

	result := JobResult{
		ExitCode: 1,
		Output:   output,
	}

	result.ParseTestOutput()

	if result.TestsPassed != 3 {
		t.Errorf("got passed=%d, want 3", result.TestsPassed)
	}
	if result.TestsFailed != 1 {
		t.Errorf("got failed=%d, want 1", result.TestsFailed)
	}
	if result.TestsIgnored != 1 {
		t.Errorf("got ignored=%d, want 1", result.TestsIgnored)
	}
}
