//go:build windows

package i18n

import (
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

func systemLanguage() string {
	buf := make([]uint16, 85)
	proc := windows.NewLazySystemDLL("kernel32.dll").NewProc("GetUserDefaultLocaleName")
	r, _, _ := proc.Call(uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	if r != 0 && strings.HasPrefix(strings.ToLower(windows.UTF16ToString(buf)), "ja") {
		return JA
	}
	return EN
}
