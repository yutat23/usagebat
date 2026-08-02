package version

import "testing"

func TestStringPrefersInjectedVersion(t *testing.T) {
	previous := Value
	Value = "0.2.0"
	t.Cleanup(func() { Value = previous })
	if got := String(); got != "0.2.0" {
		t.Fatalf("String() = %q", got)
	}
}
