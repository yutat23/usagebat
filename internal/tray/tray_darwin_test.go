//go:build darwin

package tray

import (
	"errors"
	"testing"
)

func TestIsAppBundleExecutable(t *testing.T) {
	tests := []struct {
		path string
		want bool
	}{
		{"/Applications/usagebat.app/Contents/MacOS/usagebat", true},
		{"/Applications/UsageBat.APP/Contents/MacOS/usagebat", true},
		{"/Users/test/go/bin/usagebat", false},
		{"/tmp/go-build123/b001/exe/usagebat", false},
		{"/Applications/usagebat.app/MacOS/usagebat", false},
	}
	for _, test := range tests {
		if got := isAppBundleExecutable(test.path); got != test.want {
			t.Errorf("isAppBundleExecutable(%q) = %v, want %v", test.path, got, test.want)
		}
	}
}

func TestStandaloneNotificationReturnsUnavailable(t *testing.T) {
	err := (&darwinBackend{}).Notify(Notification{Title: "test", Body: "test"})
	if !errors.Is(err, ErrNotificationsUnavailable) {
		t.Fatalf("Notify() error = %v, want ErrNotificationsUnavailable", err)
	}
}
