//go:build !darwin && !windows

package autostart

import "fmt"

func Supported() bool { return false }

func Enabled() (bool, error) { return false, nil }

func Set(bool) error { return fmt.Errorf("launch at startup is not supported on this OS") }
