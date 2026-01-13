// Package buildprotocol defines message types for worker-coordinator communication
// in the distributed build system. Messages flow over WebSocket connections.
package buildprotocol

import "encoding/json"

// Envelope wraps all messages with a type discriminator.
// When marshaling, Payload can be any message struct.
// When unmarshaling, use EnvelopeRaw for type-based dispatch.
type Envelope struct {
	Type    string      `json:"type"`
	Payload interface{} `json:"payload,omitempty"`
}

// EnvelopeRaw is used for receiving messages where the payload
// needs to be unmarshaled based on the message type.
type EnvelopeRaw struct {
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload,omitempty"`
}

// MarshalEnvelope creates an envelope with the given type and payload
func MarshalEnvelope(msgType string, payload interface{}) ([]byte, error) {
	return json.Marshal(Envelope{Type: msgType, Payload: payload})
}

// Worker -> Coordinator messages

// RegisterMessage sent when worker first connects
type RegisterMessage struct {
	WorkerID string `json:"worker_id"`
	MaxJobs  int    `json:"max_jobs"`
}

// ReadyMessage sent when worker has available job slots
type ReadyMessage struct {
	Slots int `json:"slots"`
}

// OutputMessage sent for streaming command output
type OutputMessage struct {
	JobID  string `json:"job_id"`
	Stream string `json:"stream"` // "stdout" or "stderr"
	Data   string `json:"data"`
}

// CompleteMessage sent when job finishes
type CompleteMessage struct {
	JobID      string `json:"job_id"`
	ExitCode   int    `json:"exit_code"`
	DurationMs int64  `json:"duration_ms"`
}

// ErrorMessage sent when job fails before completion
type ErrorMessage struct {
	JobID   string `json:"job_id"`
	Message string `json:"message"`
}

// Coordinator -> Worker messages

// JobMessage assigns work to a worker
type JobMessage struct {
	JobID   string            `json:"job_id"`
	Repo    string            `json:"repo"`
	Commit  string            `json:"commit"`
	Command string            `json:"command"`
	Env     map[string]string `json:"env,omitempty"`
	Timeout int               `json:"timeout_secs,omitempty"`
}

// CancelMessage requests job cancellation
type CancelMessage struct {
	JobID string `json:"job_id"`
}

// Message type constants
const (
	TypeRegister = "register"
	TypeReady    = "ready"
	TypeOutput   = "output"
	TypeComplete = "complete"
	TypeError    = "error"
	TypeJob      = "job"
	TypeCancel   = "cancel"
	TypePing     = "ping"
	TypePong     = "pong"
)
