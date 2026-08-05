package render

import "image/color"

// The battery bat. It is the app icon on both platforms, the toast icon on
// Windows, and — with a different face — the mascot on the settings page.
//
// The sprite lives here rather than in the icon generator so that the drawn
// mascot and the shipped icon cannot drift apart: they are the same animal.

// SpriteSize is the sprite's side, in dots. Everything is laid out on a
// 32-dot grid and doubled, so a "base pixel" is two dots square.
const SpriteSize = 64

const spriteBase = 32

// SpritePalette maps the sprite's cell values to colours.
var SpritePalette = map[byte]color.NRGBA{
	'#': {R: 0x20, G: 0x25, B: 0x2b, A: 0xff}, // casing, ears, wings
	'w': {R: 0xff, G: 0xfa, B: 0xf0, A: 0xff}, // battery face
	'g': {R: 0x59, G: 0xb9, B: 0x2f, A: 0xff}, // charge bars
	'a': {R: 0xff, G: 0xad, B: 0x14, A: 0xff}, // cheeks
}

// Face is the expression the bat wears.
type Face int

const (
	// FaceContent is the shipped icon's expression, and the one for a limit
	// with room to spare.
	FaceContent Face = iota
	// FaceWorried is headroom inside the warning band.
	FaceWorried
	// FaceAlarmed is headroom inside the critical band.
	FaceAlarmed
	// FaceBlank is nothing reported, so nothing to feel about it.
	FaceBlank
)

// FaceFor picks the expression from the same thresholds the battery colours
// itself by, so the two never disagree.
func FaceFor(remaining float64, known bool, p Palette) Face {
	switch {
	case !known:
		return FaceBlank
	case remaining <= p.CriticalBelow:
		return FaceAlarmed
	case remaining <= p.WarnBelow:
		return FaceWorried
	default:
		return FaceContent
	}
}

// Sprite draws the bat wearing the given face.
func Sprite(face Face) [SpriteSize][SpriteSize]byte {
	var p [SpriteSize][SpriteSize]byte
	fillRaw := func(x0, y0, x1, y1 int, value byte) {
		for y := y0; y < y1; y++ {
			for x := x0; x < x1; x++ {
				if y >= 0 && y < SpriteSize && x >= 0 && x < SpriteSize {
					p[y][x] = value
				}
			}
		}
	}
	fill := func(x0, y0, x1, y1 int, value byte) {
		fillRaw(x0*2, y0*2, x1*2, y1*2, value)
	}

	// Ears.
	fill(12, 4, 13, 5, '#')
	fill(11, 5, 14, 7, '#')
	fill(10, 7, 15, 10, '#')
	fill(19, 4, 20, 5, '#')
	fill(18, 5, 21, 7, '#')
	fill(17, 7, 22, 10, '#')

	// Battery casing.
	fill(8, 10, 24, 23, '#')
	fill(7, 11, 25, 22, '#')
	fill(9, 11, 23, 12, 'w')
	fill(8, 12, 24, 21, 'w')
	fill(9, 21, 23, 22, 'w')

	// Symmetric wings. Each root overlaps the casing side by one pixel.
	leftWing := [][3]int{
		{13, 4, 8}, {14, 3, 8}, {15, 2, 8}, {16, 1, 8}, {17, 0, 8},
		{18, 0, 4}, {18, 6, 8}, {19, 0, 3}, {19, 6, 8},
		{20, 0, 2}, {20, 6, 8},
	}
	for _, run := range leftWing {
		y, x0, x1 := run[0], run[1], run[2]
		fill(x0, y, x1, y+1, '#')
		fill(spriteBase-x1, y, spriteBase-x0, y+1, '#')
	}

	// Feet.
	fill(11, 23, 14, 24, '#')
	fill(12, 24, 14, 25, '#')
	fill(19, 23, 22, 24, '#')
	fill(19, 24, 21, 25, '#')

	// Charge bars. A worried bat has fewer of them, which is the one place the
	// expression and the reading say the same thing twice on purpose.
	bars := 3
	switch face {
	case FaceWorried:
		bars = 2
	case FaceAlarmed:
		bars = 1
	case FaceBlank:
		bars = 0
	}
	for i := 0; i < bars; i++ {
		x := 9 + i*2
		fill(x, 13, x+1, 20, 'g')
	}

	drawFace(fillRaw, fill, face)
	return p
}

// drawFace places the eyes and mouth. They sit on the half-pixel grid, which
// is why they are the only part drawn in raw dots: a mouth on the base grid
// would be twice as wide as the face has room for.
func drawFace(fillRaw, fill func(x0, y0, x1, y1 int, value byte), face Face) {
	// Cheeks stay put; they are part of the animal rather than the mood.
	fill(15, 17, 16, 18, 'a')
	fill(23, 17, 24, 18, 'a')

	switch face {
	case FaceBlank:
		// Eyes closed. Nothing is being reported, so the bat has nothing to
		// look at rather than something to worry about.
		fillRaw(31, 31, 35, 33, '#')
		fillRaw(43, 31, 47, 33, '#')
		fillRaw(37, 35, 41, 37, '#')
		return

	case FaceWorried:
		// The same eyes, and the smile flattened out.
		fillRaw(32, 29, 34, 33, '#')
		fillRaw(44, 29, 46, 33, '#')
		fillRaw(36, 35, 42, 37, '#')
		return

	case FaceAlarmed:
		// Wide eyes and an open mouth.
		fillRaw(31, 28, 35, 33, '#')
		fillRaw(43, 28, 47, 33, '#')
		fillRaw(36, 34, 42, 38, '#')
		return
	}

	// FaceContent: the shipped icon. The smile is shifted half a base pixel
	// left so its centre sits exactly between the eyes.
	fillRaw(32, 29, 34, 33, '#')
	fillRaw(44, 29, 46, 33, '#')
	fillRaw(35, 34, 37, 36, '#')
	fillRaw(41, 34, 43, 36, '#')
	fillRaw(37, 36, 41, 38, '#')
}
