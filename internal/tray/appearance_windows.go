//go:build windows

package tray

import "golang.org/x/sys/windows/registry"

const personalizeKey = `Software\Microsoft\Windows\CurrentVersion\Themes\Personalize`

func systemAppearance() Appearance {
	key, err := registry.OpenKey(registry.CURRENT_USER, personalizeKey, registry.QUERY_VALUE)
	if err != nil {
		return AppearanceLight
	}
	defer key.Close()
	light, _, err := key.GetIntegerValue("SystemUsesLightTheme")
	if err == nil && light == 0 {
		return AppearanceDark
	}
	return AppearanceLight
}
