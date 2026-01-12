package notify

// NotificationType represents the type of notification
type NotificationType int

const (
	NotifyInfo NotificationType = iota
	NotifySuccess
	NotifyWarning
	NotifyError
)

// Notification represents a notification to be sent
type Notification struct {
	Title   string
	Message string
	Type    NotificationType
	TaskID  string // Optional task reference
	PRURL   string // Optional PR URL
}

// Notifier is the interface for sending notifications
type Notifier interface {
	Send(n Notification) error
}

// MultiNotifier sends to multiple notifiers
type MultiNotifier struct {
	notifiers []Notifier
}

// NewMultiNotifier creates a notifier that sends to all provided notifiers
func NewMultiNotifier(notifiers ...Notifier) *MultiNotifier {
	return &MultiNotifier{notifiers: notifiers}
}

// Send sends the notification to all notifiers
func (m *MultiNotifier) Send(n Notification) error {
	var lastErr error
	for _, notifier := range m.notifiers {
		if err := notifier.Send(n); err != nil {
			lastErr = err
		}
	}
	return lastErr
}

// NoopNotifier does nothing (for testing or disabled notifications)
type NoopNotifier struct{}

func (NoopNotifier) Send(n Notification) error { return nil }
