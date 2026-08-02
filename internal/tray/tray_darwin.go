//go:build darwin

package tray

/*
#cgo CFLAGS: -x objective-c -fobjc-arc
#cgo LDFLAGS: -framework Cocoa
#include <stdlib.h>
#include "tray_darwin.h"
*/
import "C"

import (
	"errors"
	"sync"
	"unsafe"
)

// darwinBackend keeps the desired state and hands it to AppKit from the main
// thread inside ubTick. Nothing here touches AppKit directly.
type darwinBackend struct {
	mu sync.Mutex

	icon      IconData
	iconDirty bool

	tooltip      string
	tooltipDirty bool

	menu      []Item
	menuDirty bool
	tags      map[int]string

	onReady func()
	onClick func(string)
	started bool
}

var (
	backendOnce sync.Once
	backend     *darwinBackend
)

func newBackend() Backend {
	backendOnce.Do(func() {
		backend = &darwinBackend{tags: map[int]string{}}
	})
	return backend
}

// Layout implements Backend. macOS status items accept a wide image.
func (b *darwinBackend) Layout() Layout { return LayoutStrip }

// Run implements Backend. It must be called from the main goroutine, which the
// caller keeps locked to the main OS thread.
func (b *darwinBackend) Run(onReady func(), onClick func(id string)) error {
	b.mu.Lock()
	if b.started {
		b.mu.Unlock()
		return errors.New("tray already running")
	}
	b.started = true
	b.onReady, b.onClick = onReady, onClick
	b.mu.Unlock()

	C.ubRun()
	return nil
}

// SetIcon implements Backend.
func (b *darwinBackend) SetIcon(d IconData) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.icon, b.iconDirty = d, true
}

// SetTooltip implements Backend.
func (b *darwinBackend) SetTooltip(s string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.tooltip, b.tooltipDirty = s, true
}

// SetMenu implements Backend.
func (b *darwinBackend) SetMenu(items []Item) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.menu = append(b.menu[:0:0], items...)
	b.menuDirty = true
}

// Quit implements Backend.
func (b *darwinBackend) Quit() { C.ubQuit() }

//export ubReady
func ubReady() {
	b := backend
	if b == nil {
		return
	}
	b.mu.Lock()
	f := b.onReady
	b.mu.Unlock()
	if f != nil {
		// Off the main thread: onReady kicks off the refresh loop and must not
		// stall the run loop.
		go f()
	}
}

//export ubTick
func ubTick() {
	b := backend
	if b == nil {
		return
	}
	b.mu.Lock()
	icon, iconDirty := b.icon, b.iconDirty
	tooltip, tooltipDirty := b.tooltip, b.tooltipDirty
	menu, menuDirty := b.menu, b.menuDirty
	b.iconDirty, b.tooltipDirty, b.menuDirty = false, false, false
	if menuDirty {
		b.tags = map[int]string{}
		for i, it := range menu {
			if it.ID != "" {
				b.tags[i+1] = it.ID
			}
		}
	}
	b.mu.Unlock()

	if iconDirty && len(icon.Bytes) > 0 {
		C.ubSetIcon(unsafe.Pointer(&icon.Bytes[0]), C.int(len(icon.Bytes)),
			C.double(icon.WidthPt), C.double(icon.HeightPt))
	}
	if tooltipDirty {
		cs := C.CString(tooltip)
		C.ubSetTooltip(cs)
		C.free(unsafe.Pointer(cs))
	}
	if menuDirty {
		C.ubClearMenu()
		for i, it := range menu {
			if it.Separator {
				C.ubAddItem(nil, 0, 0, 0, 0, 1)
				continue
			}
			title := C.CString(it.Title)
			C.ubAddItem(title, C.int(i+1), cbool(!it.Disabled && it.ID != ""),
				cbool(it.Checkable && it.Checked), C.int(it.Indent), 0)
			C.free(unsafe.Pointer(title))
		}
	}
}

//export ubMenuClicked
func ubMenuClicked(tag C.int) {
	b := backend
	if b == nil {
		return
	}
	b.mu.Lock()
	id := b.tags[int(tag)]
	f := b.onClick
	b.mu.Unlock()
	if id != "" && f != nil {
		// Handlers do file I/O and re-render; keep them off the main thread.
		go f(id)
	}
}

func cbool(v bool) C.int {
	if v {
		return 1
	}
	return 0
}
