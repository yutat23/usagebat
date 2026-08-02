package main

import (
	"flag"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
)

const (
	baseSize    = 32
	logicalSize = 64
)

var palette = map[byte]color.NRGBA{
	'#': {R: 0x20, G: 0x25, B: 0x2b, A: 0xff},
	'w': {R: 0xff, G: 0xfa, B: 0xf0, A: 0xff},
	'g': {R: 0x59, G: 0xb9, B: 0x2f, A: 0xff},
	'a': {R: 0xff, G: 0xad, B: 0x14, A: 0xff},
}

func main() {
	root := flag.String("root", ".", "repository root")
	flag.Parse()

	pixels := sprite()
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
}

func sprite() [logicalSize][logicalSize]byte {
	var p [logicalSize][logicalSize]byte
	fillRaw := func(x0, y0, x1, y1 int, value byte) {
		for y := y0; y < y1; y++ {
			for x := x0; x < x1; x++ {
				p[y][x] = value
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
		fill(baseSize-x1, y, baseSize-x0, y+1, '#')
	}

	// Feet.
	fill(11, 23, 14, 24, '#')
	fill(12, 24, 14, 25, '#')
	fill(19, 23, 22, 24, '#')
	fill(19, 24, 21, 25, '#')

	// Three charge bars.
	fill(9, 13, 10, 20, 'g')
	fill(11, 13, 12, 20, 'g')
	fill(13, 13, 14, 20, 'g')

	// The eyes are one base pixel wide and two high. The smile is shifted half
	// a base pixel left so its center sits exactly between them.
	fillRaw(32, 29, 34, 33, '#')
	fillRaw(44, 29, 46, 33, '#')
	fillRaw(35, 34, 37, 36, '#')
	fillRaw(41, 34, 43, 36, '#')
	fillRaw(37, 36, 41, 38, '#')
	fill(15, 17, 16, 18, 'a')
	fill(23, 17, 24, 18, 'a')

	return p
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
			if c, ok := palette[value]; ok {
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
			if c, ok := palette[value]; ok {
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
			c := palette[value]
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
