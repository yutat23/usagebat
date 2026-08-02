//go:build darwin

package i18n

/*
#cgo LDFLAGS: -framework Foundation
#include <stdlib.h>
char *usagebatPreferredLanguage(void);
*/
import "C"

import (
	"strings"
	"unsafe"
)

func systemLanguage() string {
	p := C.usagebatPreferredLanguage()
	if p == nil {
		return EN
	}
	defer C.free(unsafe.Pointer(p))
	if strings.HasPrefix(strings.ToLower(C.GoString(p)), "ja") {
		return JA
	}
	return EN
}
