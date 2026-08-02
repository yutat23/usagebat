package render

// A 3x5 bitmap font. Each glyph is five rows; within a row the low three bits
// are the pixels, bit 2 leftmost. Keeping the font this small is what lets a
// readable percentage fit inside a battery that is only 13 dots wide.

const (
	glyphW = 3
	glyphH = 5
	// glyphGap is the spacing between glyphs, in dots.
	glyphGap = 1
)

var glyphs = map[rune][glyphH]uint8{
	'0': {0b111, 0b101, 0b101, 0b101, 0b111},
	'1': {0b010, 0b110, 0b010, 0b010, 0b111},
	'2': {0b111, 0b001, 0b111, 0b100, 0b111},
	'3': {0b111, 0b001, 0b111, 0b001, 0b111},
	'4': {0b101, 0b101, 0b111, 0b001, 0b001},
	'5': {0b111, 0b100, 0b111, 0b001, 0b111},
	'6': {0b111, 0b100, 0b111, 0b101, 0b111},
	'7': {0b111, 0b001, 0b001, 0b001, 0b001},
	'8': {0b111, 0b101, 0b111, 0b101, 0b111},
	'9': {0b111, 0b101, 0b111, 0b001, 0b111},
	'%': {0b101, 0b001, 0b010, 0b100, 0b101},
	'?': {0b111, 0b001, 0b011, 0b000, 0b010},
	'!': {0b010, 0b010, 0b010, 0b000, 0b010},
	'-': {0b000, 0b000, 0b111, 0b000, 0b000},
	'.': {0b000, 0b000, 0b000, 0b000, 0b010},
	':': {0b000, 0b010, 0b000, 0b010, 0b000},
	'/': {0b001, 0b001, 0b010, 0b100, 0b100},
	'C': {0b111, 0b100, 0b100, 0b100, 0b111},
	'D': {0b110, 0b101, 0b101, 0b101, 0b110},
	'H': {0b101, 0b101, 0b111, 0b101, 0b101},
	'K': {0b101, 0b110, 0b100, 0b110, 0b101},
	'L': {0b100, 0b100, 0b100, 0b100, 0b111},
	'M': {0b101, 0b111, 0b111, 0b101, 0b101},
	'O': {0b111, 0b101, 0b101, 0b101, 0b111},
	'W': {0b101, 0b101, 0b101, 0b111, 0b101},
	'X': {0b101, 0b101, 0b010, 0b101, 0b101},
	' ': {0b000, 0b000, 0b000, 0b000, 0b000},
}

// textWidth is the width in dots of s rendered with this font.
func textWidth(s string) int {
	n := len([]rune(s))
	if n == 0 {
		return 0
	}
	return n*glyphW + (n-1)*glyphGap
}

// drawText plots s onto the canvas with its top-left at (x, y). colorAt is
// consulted per pixel so callers can flip the ink where the glyph crosses the
// filled part of a gauge.
func drawText(c *canvas, x, y int, s string, colorAt func(px, py int) rgba) {
	cx := x
	for _, r := range s {
		g, ok := glyphs[r]
		if !ok {
			g = glyphs['?']
		}
		for row := 0; row < glyphH; row++ {
			bits := g[row]
			for col := 0; col < glyphW; col++ {
				if bits&(1<<(glyphW-1-col)) == 0 {
					continue
				}
				px, py := cx+col, y+row
				c.set(px, py, colorAt(px, py))
			}
		}
		cx += glyphW + glyphGap
	}
}

// solid returns a colorAt function that ignores position.
func solid(col rgba) func(int, int) rgba {
	return func(int, int) rgba { return col }
}
