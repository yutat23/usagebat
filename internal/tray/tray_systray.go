//go:build !darwin

package tray

import (
	"strings"
	"sync"

	"fyne.io/systray"
)

// systrayBackend drives the tray on platforms whose icons are a fixed square.
// The constraint that pushed macOS onto a hand-written backend — forcing the
// image to a small square — costs nothing here, so the shared library is used.
//
// systray's menu items cannot be reordered or removed once created, so the
// backend keeps a growable pool of rows and rewrites their contents on every
// update, hiding whatever is currently unused.
type systrayBackend struct {
	mu      sync.Mutex
	pool    []*systray.MenuItem
	ids     []string
	pending []Item

	icon    IconData
	tooltip string
	ready   bool
	onClick func(string)
	onReady func()
}

var (
	backendOnce sync.Once
	backend     *systrayBackend
)

func newBackend() Backend {
	backendOnce.Do(func() {
		backend = &systrayBackend{}
	})
	return backend
}

// Layout implements Backend.
func (b *systrayBackend) Layout() Layout { return LayoutSquare }

func (b *systrayBackend) Appearance() Appearance { return systemAppearance() }

// Run implements Backend.
func (b *systrayBackend) Run(onReady func(), onClick func(id string)) error {
	b.mu.Lock()
	b.onReady, b.onClick = onReady, onClick
	b.mu.Unlock()

	systray.Run(func() {
		b.mu.Lock()
		b.ready = true
		icon, tooltip, pending := b.icon, b.tooltip, b.pending
		f := b.onReady
		b.mu.Unlock()

		if len(icon.Bytes) > 0 {
			systray.SetIcon(icon.Bytes)
		}
		if tooltip != "" {
			systray.SetTooltip(tooltip)
		}
		if pending != nil {
			b.applyMenu(pending)
		}
		if f != nil {
			go f()
		}
	}, nil)
	return nil
}

// SetIcon implements Backend.
func (b *systrayBackend) SetIcon(d IconData) {
	b.mu.Lock()
	b.icon = d
	ready := b.ready
	b.mu.Unlock()
	if ready && len(d.Bytes) > 0 {
		systray.SetIcon(d.Bytes)
	}
}

// SetTooltip implements Backend.
func (b *systrayBackend) SetTooltip(s string) {
	b.mu.Lock()
	b.tooltip = s
	ready := b.ready
	b.mu.Unlock()
	if ready {
		systray.SetTooltip(s)
	}
}

// SetMenu implements Backend.
func (b *systrayBackend) SetMenu(items []Item) {
	b.mu.Lock()
	b.pending = append(items[:0:0], items...)
	ready := b.ready
	b.mu.Unlock()
	if ready {
		b.applyMenu(items)
	}
}

func (b *systrayBackend) Notify(n Notification) error { return notifyNative(n) }

// separatorTitle stands in for a real separator: systray rows are permanent, so
// a drawn rule that can be re-titled is the only form that survives a rebuild.
const separatorTitle = "──────────────"

func (b *systrayBackend) applyMenu(items []Item) {
	b.grow(len(items))

	b.mu.Lock()
	for i, it := range items {
		title := it.Title
		if it.Separator {
			title = separatorTitle
		} else if it.Indent > 0 {
			title = strings.Repeat("   ", it.Indent) + title
		}
		b.ids[i] = it.ID

		mi := b.pool[i]
		mi.SetTitle(title)
		// Set unconditionally: rows are recycled, and a stale tooltip from
		// whatever this row used to be would otherwise stick to it.
		mi.SetTooltip(it.Tooltip)
		if it.Checkable && it.Checked {
			mi.Check()
		} else {
			mi.Uncheck()
		}
		if it.Disabled || it.ID == "" {
			mi.Disable()
		} else {
			mi.Enable()
		}
		mi.Show()
	}
	for i := len(items); i < len(b.pool); i++ {
		b.ids[i] = ""
		b.pool[i].Hide()
	}
	b.mu.Unlock()
}

// grow makes sure the pool has at least n rows, wiring a click forwarder for
// each new one. The forwarder reads the row's current ID at click time because
// rows are recycled across rebuilds.
func (b *systrayBackend) grow(n int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for len(b.pool) < n {
		idx := len(b.pool)
		mi := systray.AddMenuItem("", "")
		b.pool = append(b.pool, mi)
		b.ids = append(b.ids, "")
		go func(idx int, mi *systray.MenuItem) {
			for range mi.ClickedCh {
				b.mu.Lock()
				id := b.ids[idx]
				f := b.onClick
				b.mu.Unlock()
				if id != "" && f != nil {
					f(id)
				}
			}
		}(idx, mi)
	}
}

// Quit implements Backend.
func (b *systrayBackend) Quit() { systray.Quit() }
