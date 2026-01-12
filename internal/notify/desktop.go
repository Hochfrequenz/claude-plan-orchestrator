package notify

import (
	"os/exec"
	"runtime"
)

// DesktopNotifier sends desktop notifications
type DesktopNotifier struct {
	enabled bool
}

// NewDesktopNotifier creates a new desktop notifier
func NewDesktopNotifier(enabled bool) *DesktopNotifier {
	return &DesktopNotifier{enabled: enabled}
}

// Send sends a desktop notification
func (d *DesktopNotifier) Send(n Notification) error {
	if !d.enabled {
		return nil
	}

	switch runtime.GOOS {
	case "darwin":
		return d.sendMacOS(n)
	case "linux":
		return d.sendLinux(n)
	default:
		return nil // Unsupported
	}
}

func (d *DesktopNotifier) sendMacOS(n Notification) error {
	script := `display notification "` + n.Message + `" with title "` + n.Title + `"`
	cmd := exec.Command("osascript", "-e", script)
	return cmd.Run()
}

func (d *DesktopNotifier) sendLinux(n Notification) error {
	// Try notify-send (most common)
	cmd := exec.Command("notify-send", n.Title, n.Message)
	return cmd.Run()
}

// IconForType returns an icon name for the notification type
func IconForType(t NotificationType) string {
	switch t {
	case NotifySuccess:
		return "dialog-positive"
	case NotifyWarning:
		return "dialog-warning"
	case NotifyError:
		return "dialog-error"
	default:
		return "dialog-information"
	}
}
