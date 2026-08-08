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

// Mascot renders the bat as inline SVG.
func Mascot(label string) Chart {
	sprite := Sprite()
	var b strings.Builder
	fmt.Fprintf(&b, `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 %d %d" `+
		`shape-rendering="crispEdges" role="img" aria-label="%s">`,
		SpriteSize, SpriteSize, escapeAttr(label))

	// Neighbouring dots of one colour merge in both directions. The bat is
	// four thousand dots; a rectangle each would weigh more than the charts.
	taken := make([]bool, SpriteSize*SpriteSize)
	for y := 0; y < SpriteSize; y++ {
		for x := 0; x < SpriteSize; x++ {
			value := sprite[y][x]
			if value == 0 || taken[y*SpriteSize+x] {
				continue
			}
			width := 1
			for x+width < SpriteSize &&
				!taken[y*SpriteSize+x+width] && sprite[y][x+width] == value {
				width++
			}
			height := 1
			for y+height < SpriteSize && spriteRowMatches(sprite, taken, x, y+height, width, value) {
				height++
			}
			for dy := 0; dy < height; dy++ {
				for dx := 0; dx < width; dx++ {
					taken[(y+dy)*SpriteSize+x+dx] = true
				}
			}
			colour := SpritePalette[value]
			fmt.Fprintf(&b, `<rect x="%d" y="%d" width="%d" height="%d" fill="#%02x%02x%02x"/>`,
				x, y, width, height, colour.R, colour.G, colour.B)
		}
	}
	b.WriteString("</svg>")
	return Chart{SVG: b.String(), Width: SpriteSize, Height: SpriteSize}
}

func spriteRowMatches(sprite [SpriteSize][SpriteSize]byte, taken []bool,
	x, y, width int, value byte) bool {

	for dx := 0; dx < width; dx++ {
		if taken[y*SpriteSize+x+dx] || sprite[y][x+dx] != value {
			return false
		}
	}
	return true
}
