//go:build !darwin && !windows

package i18n

import (
	"os"
	"strings"
)

func systemLanguage() string {
	for _, key := range []string{"LC_ALL", "LC_MESSAGES", "LANG"} {
		if strings.HasPrefix(strings.ToLower(os.Getenv(key)), "ja") {
			return JA
		}
	}
	return EN
}
