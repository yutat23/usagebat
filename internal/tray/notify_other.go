//go:build !darwin && !windows

package tray

func notifyNative(Notification) error { return nil }

func notificationDiagnostics() []string {
	return []string{"notifications: not implemented on this platform"}
}
