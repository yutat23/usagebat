// Package tray abstracts the platform tray/menu-bar integration.
//
// The two platforms differ enough that they get separate backends: macOS
// allows an arbitrarily wide menu-bar image, Windows allows only a fixed
// square icon. Layout reports which artwork the caller should produce.
package tray

// Layout is the artwork shape a backend can display.
type Layout int

const (
	// LayoutStrip is a wide image; the caller supplies PNG bytes.
	LayoutStrip Layout = iota
	// LayoutSquare is a fixed square icon; the caller supplies ICO bytes.
	LayoutSquare
)

// Appearance is the system bar theme the icon is currently drawn against.
type Appearance int

const (
	AppearanceLight Appearance = iota
	AppearanceDark
)

// Item is one flat menu row. Submenus are expressed with Indent instead of
// nesting, which both backends can represent faithfully.
type Item struct {
	// ID is returned to the click handler. Empty means the row is not clickable.
	ID        string
	Title     string
	Tooltip   string
	Disabled  bool
	Checkable bool
	Checked   bool
	Separator bool
	Indent    int
}

// IconData is a rendered icon ready for the platform.
type IconData struct {
	// Bytes is PNG on macOS and ICO on Windows.
	Bytes []byte
	// WidthPt and HeightPt are the logical size the image should occupy.
	// Only macOS uses them.
	WidthPt, HeightPt float64
}

// Backend is the platform integration.
//
// Run blocks until the tray exits. All other methods are safe to call from any
// goroutine and take effect on the platform's UI thread.
type Backend interface {
	Layout() Layout
	Appearance() Appearance
	Run(onReady func(), onClick func(id string)) error
	SetIcon(IconData)
	SetTooltip(string)
	SetMenu([]Item)
	Quit()
}

// New returns the backend for the current platform.
func New() Backend { return newBackend() }
