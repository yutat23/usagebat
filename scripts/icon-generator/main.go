package main

import (
	"flag"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"

	"github.com/yutat23/usagebat/internal/render"
)

// logicalSize mirrors the shared sprite's grid.
const logicalSize = render.SpriteSize

func main() {
	root := flag.String("root", ".", "repository root")
	flag.Parse()

	pixels := render.Sprite(render.FaceContent)
	must(writeSVG(filepath.Join(*root, "assets", "usagebat.svg"), pixels))
	must(writePNG(filepath.Join(*root, "assets", "usagebat.png"), pixels, 1024))
	must(writePreview(filepath.Join(*root, "build", "usagebat-icon-preview.png"), pixels, 1024))

	iconset := filepath.Join(*root, "build", "usagebat.iconset")
	macSizes := []struct {
		name string
		size int
	}{
		{"icon_16x16.png", 16},
		{"icon_16x16@2x.png", 32},
		{"icon_32x32.png", 32},
		{"icon_32x32@2x.png", 64},
		{"icon_128x128.png", 128},
		{"icon_128x128@2x.png", 256},
		{"icon_256x256.png", 256},
		{"icon_256x256@2x.png", 512},
		{"icon_512x512.png", 512},
		{"icon_512x512@2x.png", 1024},
	}
	for _, icon := range macSizes {
		must(writePNG(filepath.Join(iconset, icon.name), pixels, icon.size))
	}

	winres := filepath.Join(*root, "cmd", "usagebat", "winres")
	for _, size := range []int{16, 32, 48, 64, 256} {
		must(writePNG(filepath.Join(winres, fmt.Sprintf("icon_%d.png", size)), pixels, size))
	}

	// Windows toasts read their icon from a file on disk, not from the
	// executable's resources, so the tray package embeds its own copy.
	toast := outlined(pixels)
	must(writeGridPNG(filepath.Join(*root, "internal", "tray", "toasticon.png"), toast, toastScale))
	must(writeToastPreview(filepath.Join(*root, "build", "usagebat-toast-preview.png"), toast))
}

// A toast is drawn on the notification surface, which is near-black in
// Windows' dark theme — the same near-black as the icon's casing, ears and
// wings, which leaves the silhouette invisible. Outlining it in white keeps it
// readable on either theme.
const (
	// toastOutline is the outline's width in logical pixels: one base pixel of
	// the sprite's own grid, so it reads as part of the pixel art.
	toastOutline = 2
	// toastPadding leaves room for an outline around the wing tips, which run
	// all the way to the edge of the sprite.
	toastPadding = 4
	// toastScale keeps the upscale an integer, so no pixel comes out wider
	// than its neighbours.
	toastScale = 4
)

// outlined returns the sprite padded and surrounded by a white border.
func outlined(pixels [logicalSize][logicalSize]byte) [][]byte {
	side := logicalSize + 2*toastPadding
	grid := make([][]byte, side)
	for y := range grid {
		grid[y] = make([]byte, side)
	}
	for y := 0; y < logicalSize; y++ {
		for x := 0; x < logicalSize; x++ {
			grid[y+toastPadding][x+toastPadding] = pixels[y][x]
		}
	}

	// Every empty cell within the outline's reach of a drawn one turns white.
	// Reading the source and writing the result separately keeps the outline
	// from growing into itself.
	out := make([][]byte, side)
	for y := range out {
		out[y] = make([]byte, side)
		copy(out[y], grid[y])
	}
	for y := 0; y < side; y++ {
		for x := 0; x < side; x++ {
			if grid[y][x] != 0 || !nearDrawn(grid, x, y) {
				continue
			}
			out[y][x] = 'w'
		}
	}
	return out
}

func nearDrawn(grid [][]byte, x, y int) bool {
	for dy := -toastOutline; dy <= toastOutline; dy++ {
		for dx := -toastOutline; dx <= toastOutline; dx++ {
			ny, nx := y+dy, x+dx
			if ny < 0 || ny >= len(grid) || nx < 0 || nx >= len(grid[ny]) {
				continue
			}
			if grid[ny][nx] != 0 {
				return true
			}
		}
	}
	return false
}

// writeToastPreview puts the toast icon on the two notification surfaces it
// has to survive — Windows' dark theme on the left, its light theme on the
// right — which is the only way to judge the outline without a Windows box.
func writeToastPreview(path string, grid [][]byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	const scale = 3
	side := len(grid) * scale
	surfaces := []color.NRGBA{
		{R: 0x2b, G: 0x2b, B: 0x2b, A: 0xff},
		{R: 0xf3, G: 0xf3, B: 0xf3, A: 0xff},
	}
	img := image.NewNRGBA(image.Rect(0, 0, side*len(surfaces), side))
	for panel, surface := range surfaces {
		for y := 0; y < side; y++ {
			for x := 0; x < side; x++ {
				pixel := surface
				if c, ok := render.SpritePalette[grid[y/scale][x/scale]]; ok {
					pixel = c
				}
				img.SetNRGBA(panel*side+x, y, pixel)
			}
		}
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return png.Encode(f, img)
}

// writeGridPNG scales a grid up by a whole number of device pixels per cell.
func writeGridPNG(path string, grid [][]byte, scale int) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	size := len(grid) * scale
	img := image.NewNRGBA(image.Rect(0, 0, size, size))
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			if c, ok := render.SpritePalette[grid[y/scale][x/scale]]; ok {
				img.SetNRGBA(x, y, c)
			}
		}
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return png.Encode(f, img)
}

func writePNG(path string, pixels [logicalSize][logicalSize]byte, size int) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	img := image.NewNRGBA(image.Rect(0, 0, size, size))
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			sourceX := x * logicalSize / size
			sourceY := y * logicalSize / size
			if size < logicalSize {
				sourceX += logicalSize / size / 2
				sourceY += logicalSize / size / 2
			}
			value := pixels[sourceY][sourceX]
			if c, ok := render.SpritePalette[value]; ok {
				img.SetNRGBA(x, y, c)
			}
		}
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return png.Encode(f, img)
}

func writePreview(path string, pixels [logicalSize][logicalSize]byte, size int) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	img := image.NewNRGBA(image.Rect(0, 0, size, size))
	background := color.NRGBA{R: 0xe8, G: 0xea, B: 0xed, A: 0xff}
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			img.SetNRGBA(x, y, background)
			value := pixels[y*logicalSize/size][x*logicalSize/size]
			if c, ok := render.SpritePalette[value]; ok {
				img.SetNRGBA(x, y, c)
			}
		}
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return png.Encode(f, img)
}

func writeSVG(path string, pixels [logicalSize][logicalSize]byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	if _, err := fmt.Fprintf(f, `<svg xmlns="http://www.w3.org/2000/svg" width="1024" height="1024" viewBox="0 0 %d %d" shape-rendering="crispEdges">`+"\n", logicalSize, logicalSize); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(f, "  <title>usagebat</title>"); err != nil {
		return err
	}
	for y, row := range pixels {
		for x := 0; x < logicalSize; {
			value := row[x]
			if value == 0 {
				x++
				continue
			}
			x1 := x + 1
			for x1 < logicalSize && row[x1] == value {
				x1++
			}
			c := render.SpritePalette[value]
			fill := fmt.Sprintf("#%02x%02x%02x", c.R, c.G, c.B)
			if _, err := fmt.Fprintf(f, "  <rect x=\"%d\" y=\"%d\" width=\"%d\" height=\"1\" fill=\"%s\"/>\n", x, y, x1-x, fill); err != nil {
				return err
			}
			x = x1
		}
	}
	_, err = fmt.Fprintln(f, "</svg>")
	return err
}

func must(err error) {
	if err != nil {
		panic(err)
	}
}
