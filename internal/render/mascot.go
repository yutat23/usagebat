package render

import (
	"fmt"
	"strings"
)

// The mascot is the app icon, drawn large with a stable expression. It is
// deliberately the one pixel-art thing left
// on the settings page: the charts are data and read better smooth, but the
// bat is the icon, and the icon is pixel art everywhere else it appears.
//
// It is not a gauge. The numbers are on the same screen, and a face cannot
// say anything the battery beside it does not say better.

// The casing, ears and wings are near-black. On a dark settings page that is
// the same ink as the background, so the silhouette vanishes — the same
// problem the Windows toast already solved with a white outline. One base
// pixel of cream around the sprite, plus padding so the wing tips are not
// clipped.
const (
	mascotOutline = 2
	mascotPad     = 4
)

// Mascot renders the bat as inline SVG. Dark is the settings page's dark
// theme: the animal is unchanged, and a cream outline keeps the silhouette.
func Mascot(label string, dark bool) Chart {
	grid := mascotGrid(dark)
	side := len(grid)
	var b strings.Builder
	fmt.Fprintf(&b, `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 %d %d" `+
		`shape-rendering="crispEdges" role="img" aria-label="%s">`,
		side, side, escapeAttr(label))

	// Neighbouring dots of one colour merge in both directions. The bat is
	// four thousand dots; a rectangle each would weigh more than the charts.
	taken := make([]bool, side*side)
	for y := 0; y < side; y++ {
		for x := 0; x < side; x++ {
			value := grid[y][x]
			if value == 0 || taken[y*side+x] {
				continue
			}
			width := 1
			for x+width < side &&
				!taken[y*side+x+width] && grid[y][x+width] == value {
				width++
			}
			height := 1
			for y+height < side && mascotRowMatches(grid, taken, side, x, y+height, width, value) {
				height++
			}
			for dy := 0; dy < height; dy++ {
				for dx := 0; dx < width; dx++ {
					taken[(y+dy)*side+x+dx] = true
				}
			}
			colour := SpritePalette[value]
			fmt.Fprintf(&b, `<rect x="%d" y="%d" width="%d" height="%d" fill="#%02x%02x%02x"/>`,
				x, y, width, height, colour.R, colour.G, colour.B)
		}
	}
	b.WriteString("</svg>")
	return Chart{SVG: b.String(), Width: float64(side), Height: float64(side)}
}

func mascotGrid(dark bool) [][]byte {
	src := Sprite()
	if !dark {
		out := make([][]byte, SpriteSize)
		for y := range out {
			out[y] = src[y][:]
		}
		return out
	}

	side := SpriteSize + 2*mascotPad
	placed := make([][]byte, side)
	for y := range placed {
		placed[y] = make([]byte, side)
	}
	for y := 0; y < SpriteSize; y++ {
		copy(placed[y+mascotPad][mascotPad:], src[y][:])
	}
	out := make([][]byte, side)
	for y := range out {
		out[y] = append([]byte(nil), placed[y]...)
	}
	for y := 0; y < side; y++ {
		for x := 0; x < side; x++ {
			if placed[y][x] != 0 || !mascotNearDrawn(placed, x, y) {
				continue
			}
			out[y][x] = 'w'
		}
	}
	return out
}

func mascotNearDrawn(grid [][]byte, x, y int) bool {
	side := len(grid)
	for dy := -mascotOutline; dy <= mascotOutline; dy++ {
		for dx := -mascotOutline; dx <= mascotOutline; dx++ {
			ny, nx := y+dy, x+dx
			if ny < 0 || ny >= side || nx < 0 || nx >= side {
				continue
			}
			if grid[ny][nx] != 0 {
				return true
			}
		}
	}
	return false
}

func mascotRowMatches(grid [][]byte, taken []bool, side, x, y, width int, value byte) bool {
	for dx := 0; dx < width; dx++ {
		if taken[y*side+x+dx] || grid[y][x+dx] != value {
			return false
		}
	}
	return true
}
