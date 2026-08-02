//go:build !darwin && !windows

package tray

func systemAppearance() Appearance { return AppearanceLight }
