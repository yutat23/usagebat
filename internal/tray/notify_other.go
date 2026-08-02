//go:build !darwin && !windows

package tray

func notifyNative(Notification) error { return nil }
