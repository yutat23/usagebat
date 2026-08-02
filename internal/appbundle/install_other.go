//go:build !darwin

package appbundle

import "fmt"

func Install(string) (string, error) {
	return "", fmt.Errorf("install-app is available only on macOS")
}

func Launch(string) error {
	return fmt.Errorf("install-app is available only on macOS")
}
