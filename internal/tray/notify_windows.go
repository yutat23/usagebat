//go:build windows

package tray

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"os"
	"sync"

	"git.sr.ht/~jackmordaunt/go-toast/v2/wintoast"
)

const notificationAppID = "usagebat"

var (
	notifySetup sync.Once
	notifyErr   error
)

func notifyNative(n Notification) error {
	notifySetup.Do(func() {
		exe, err := os.Executable()
		if err != nil {
			notifyErr = err
			return
		}
		notifyErr = wintoast.SetAppData(wintoast.AppData{
			AppID: notificationAppID, IconPath: exe, IconBackgroundColor: "#00000000",
		})
	})
	if notifyErr != nil {
		return notifyErr
	}
	var title, body bytes.Buffer
	if err := xml.EscapeText(&title, []byte(n.Title)); err != nil {
		return err
	}
	if err := xml.EscapeText(&body, []byte(n.Body)); err != nil {
		return err
	}
	payload := fmt.Sprintf(`<toast><visual><binding template="ToastGeneric"><text>%s</text><text>%s</text></binding></visual><audio src="ms-winsoundevent:Notification.Default"/></toast>`, title.String(), body.String())
	// Call the COM path directly. The high-level package enables a PowerShell
	// fallback; usagebat deliberately never opens or spawns a shell for alerts.
	return wintoast.Push(notificationAppID, payload)
}
