package render

import (
	"image"
	"image/color"
	"strconv"
	"strings"
)

// rgba is a straight-alpha colour on the dot grid.
type rgba struct{ R, G, B, A uint8 }

var transparent = rgba{}

// canvas is a dot-grid bitmap. All art is composed here at one dot per cell and
// only scaled up on the way out, which is what keeps the pixel look crisp.
type canvas struct {
	w, h int
	px   []rgba
}

func newCanvas(w, h int) *canvas {
	if w < 0 {
		w = 0
	}
	if h < 0 {
		h = 0
	}
	return &canvas{w: w, h: h, px: make([]rgba, w*h)}
}

func (c *canvas) inBounds(x, y int) bool { return x >= 0 && y >= 0 && x < c.w && y < c.h }

func (c *canvas) set(x, y int, col rgba) {
	if !c.inBounds(x, y) || col.A == 0 {
		return
	}
	c.px[y*c.w+x] = col
}

// fillRect paints a solid block.
func (c *canvas) fillRect(x, y, w, h int, col rgba) {
	for dy := 0; dy < h; dy++ {
		for dx := 0; dx < w; dx++ {
			c.set(x+dx, y+dy, col)
		}
	}
}

// strokeRect paints a one-dot outline.
func (c *canvas) strokeRect(x, y, w, h int, col rgba) {
	if w <= 0 || h <= 0 {
		return
	}
	for dx := 0; dx < w; dx++ {
		c.set(x+dx, y, col)
		c.set(x+dx, y+h-1, col)
	}
	for dy := 0; dy < h; dy++ {
		c.set(x, y+dy, col)
		c.set(x+w-1, y+dy, col)
	}
}

// blit copies src onto c with src's origin at (x, y).
func (c *canvas) blit(src *canvas, x, y int) {
	for sy := 0; sy < src.h; sy++ {
		for sx := 0; sx < src.w; sx++ {
			c.set(x+sx, y+sy, src.px[sy*src.w+sx])
		}
	}
}

// toImage scales the dot grid up by an integer factor using nearest-neighbour,
// so every dot stays a hard-edged square.
func (c *canvas) toImage(scale int) *image.RGBA {
	if scale < 1 {
		scale = 1
	}
	img := image.NewRGBA(image.Rect(0, 0, c.w*scale, c.h*scale))
	for y := 0; y < c.h; y++ {
		for x := 0; x < c.w; x++ {
			p := c.px[y*c.w+x]
			if p.A == 0 {
				continue
			}
			// image.RGBA is premultiplied; our palette is fully opaque or fully
			// clear, so a direct copy is correct.
			col := color.RGBA{R: p.R, G: p.G, B: p.B, A: p.A}
			for dy := 0; dy < scale; dy++ {
				for dx := 0; dx < scale; dx++ {
					img.SetRGBA(x*scale+dx, y*scale+dy, col)
				}
			}
		}
	}
	return img
}

// mix blends a towards b by t (0..1), keeping the result opaque.
func mix(a, b rgba, t float64) rgba {
	lerp := func(x, y uint8) uint8 {
		return uint8(float64(x)*(1-t) + float64(y)*t + 0.5)
	}
	return rgba{R: lerp(a.R, b.R), G: lerp(a.G, b.G), B: lerp(a.B, b.B), A: 0xFF}
}

// pale is the unfilled part of a gauge. Painting it rather than leaving it
// clear is what lets the digits be knocked out in one dark ink regardless of
// whether the menu bar behind them is light or dark.
func pale(c rgba) rgba { return mix(c, rgba{0xFF, 0xFF, 0xFF, 0xFF}, 0.72) }

// ink darkens an accent for text drawn straight onto the menu bar, where there
// is no painted backdrop to guarantee contrast.
func ink(c rgba) rgba { return mix(c, rgba{0, 0, 0, 0xFF}, 0.3) }

// parseHex reads "#rrggbb" or "#rrggbbaa", falling back to fallback.
func parseHex(s string, fallback rgba) rgba {
	s = strings.TrimPrefix(strings.TrimSpace(s), "#")
	if len(s) != 6 && len(s) != 8 {
		return fallback
	}
	v, err := strconv.ParseUint(s, 16, 64)
	if err != nil {
		return fallback
	}
	if len(s) == 6 {
		return rgba{R: uint8(v >> 16), G: uint8(v >> 8), B: uint8(v), A: 0xFF}
	}
	return rgba{R: uint8(v >> 24), G: uint8(v >> 16), B: uint8(v >> 8), A: uint8(v)}
}
