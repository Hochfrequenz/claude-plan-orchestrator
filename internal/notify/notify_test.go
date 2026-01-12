package notify

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSlackMessage_Build(t *testing.T) {
	msg := SlackMessage{
		Text: "Task completed",
		Attachments: []SlackAttachment{
			{
				Color: "good",
				Title: "technical/E05",
				Text:  "Validators implemented",
			},
		},
	}

	payload, err := msg.ToJSON()
	if err != nil {
		t.Fatal(err)
	}

	if len(payload) == 0 {
		t.Error("Payload should not be empty")
	}
}

func TestSlackNotifier_Send(t *testing.T) {
	// Mock Slack server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			t.Errorf("Expected POST, got %s", r.Method)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	notifier := NewSlackNotifier(server.URL)
	err := notifier.Send(Notification{
		Title:   "Test",
		Message: "Test message",
		Type:    NotifyInfo,
	})

	if err != nil {
		t.Errorf("Send failed: %v", err)
	}
}

func TestNotificationTypeColors(t *testing.T) {
	tests := []struct {
		typ  NotificationType
		want string
	}{
		{NotifySuccess, "good"},
		{NotifyWarning, "warning"},
		{NotifyError, "danger"},
		{NotifyInfo, "#439FE0"},
	}

	for _, tt := range tests {
		got := SlackColor(tt.typ)
		if got != tt.want {
			t.Errorf("SlackColor(%v) = %s, want %s", tt.typ, got, tt.want)
		}
	}
}

func TestMultiNotifier(t *testing.T) {
	var called []string

	mock1 := &mockNotifier{name: "mock1", calls: &called}
	mock2 := &mockNotifier{name: "mock2", calls: &called}

	multi := NewMultiNotifier(mock1, mock2)
	multi.Send(Notification{Title: "Test"})

	if len(called) != 2 {
		t.Errorf("Expected 2 calls, got %d", len(called))
	}
}

type mockNotifier struct {
	name  string
	calls *[]string
}

func (m *mockNotifier) Send(n Notification) error {
	*m.calls = append(*m.calls, m.name)
	return nil
}
