package notify

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// SlackNotifier sends notifications to Slack
type SlackNotifier struct {
	webhookURL string
	client     *http.Client
}

// SlackMessage represents a Slack message payload
type SlackMessage struct {
	Text        string            `json:"text"`
	Attachments []SlackAttachment `json:"attachments,omitempty"`
}

// SlackAttachment represents a Slack message attachment
type SlackAttachment struct {
	Color  string `json:"color"`
	Title  string `json:"title"`
	Text   string `json:"text"`
	Footer string `json:"footer,omitempty"`
}

// NewSlackNotifier creates a new Slack notifier
func NewSlackNotifier(webhookURL string) *SlackNotifier {
	return &SlackNotifier{
		webhookURL: webhookURL,
		client: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

// ToJSON converts the message to JSON
func (m *SlackMessage) ToJSON() ([]byte, error) {
	return json.Marshal(m)
}

// SlackColor returns the Slack color for a notification type
func SlackColor(t NotificationType) string {
	switch t {
	case NotifySuccess:
		return "good"
	case NotifyWarning:
		return "warning"
	case NotifyError:
		return "danger"
	default:
		return "#439FE0"
	}
}

// Send sends a notification to Slack
func (s *SlackNotifier) Send(n Notification) error {
	if s.webhookURL == "" {
		return nil // Disabled
	}

	msg := SlackMessage{
		Text: n.Title,
		Attachments: []SlackAttachment{
			{
				Color:  SlackColor(n.Type),
				Text:   n.Message,
				Footer: "ERP Orchestrator",
			},
		},
	}

	if n.TaskID != "" {
		msg.Attachments[0].Title = n.TaskID
	}

	payload, err := msg.ToJSON()
	if err != nil {
		return err
	}

	resp, err := s.client.Post(s.webhookURL, "application/json", bytes.NewReader(payload))
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("slack returned %d", resp.StatusCode)
	}

	return nil
}
