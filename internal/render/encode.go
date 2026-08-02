package render

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/draw"
	"image/png"
)

// PNG encodes an image for the macOS status item.
func PNG(img image.Image) ([]byte, error) {
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// icoSizes cover the common Windows taskbar DPI sizes so the shell can choose
// an exact image instead of shrinking one and leaving the artwork undersized.
var icoSizes = []int{16, 20, 24, 32, 40, 48, 64}

// ResizeNearest scales pixel art to an exact Windows icon size. DPI-scaled
// taskbars commonly request non-integer multiples such as 20, 24 or 40px; an
// exact entry avoids Windows shrinking a 32px image and making the art look
// smaller and softer.
func ResizeNearest(src *image.RGBA, size int) *image.RGBA {
	dst := image.NewRGBA(image.Rect(0, 0, size, size))
	w, h := src.Bounds().Dx(), src.Bounds().Dy()
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			sx := src.Bounds().Min.X + x*w/size
			sy := src.Bounds().Min.Y + y*h/size
			dst.SetRGBA(x, y, src.RGBAAt(sx, sy))
		}
	}
	return dst
}

// ICO encodes a square dot canvas as a multi-resolution Windows icon.
//
// The entries use the classic uncompressed DIB form rather than embedded PNGs:
// PNG-compressed entries are only understood by some of the Win32 icon loaders,
// and Shell_NotifyIcon paths still go through the ones that are not.
func ICO(render func(size int) *image.RGBA) ([]byte, error) {
	type blob struct {
		size int
		data []byte
	}
	blobs := make([]blob, 0, len(icoSizes))
	for _, s := range icoSizes {
		img := render(s)
		blobs = append(blobs, blob{size: s, data: encodeDIB(img, s)})
	}

	var buf bytes.Buffer
	// ICONDIR
	binary.Write(&buf, binary.LittleEndian, uint16(0)) // reserved
	binary.Write(&buf, binary.LittleEndian, uint16(1)) // type: icon
	binary.Write(&buf, binary.LittleEndian, uint16(len(blobs)))

	offset := 6 + 16*len(blobs)
	for _, b := range blobs {
		buf.WriteByte(byte(b.size))                         // width  (0 means 256; our sizes are smaller)
		buf.WriteByte(byte(b.size))                         // height
		buf.WriteByte(0)                                    // palette size
		buf.WriteByte(0)                                    // reserved
		binary.Write(&buf, binary.LittleEndian, uint16(1))  // planes
		binary.Write(&buf, binary.LittleEndian, uint16(32)) // bits per pixel
		binary.Write(&buf, binary.LittleEndian, uint32(len(b.data)))
		binary.Write(&buf, binary.LittleEndian, uint32(offset))
		offset += len(b.data)
	}
	for _, b := range blobs {
		buf.Write(b.data)
	}
	return buf.Bytes(), nil
}

// encodeDIB writes a BITMAPINFOHEADER, a bottom-up 32-bit BGRA image and the
// 1bpp AND mask an ICO entry requires.
func encodeDIB(src *image.RGBA, size int) []byte {
	img := image.NewRGBA(image.Rect(0, 0, size, size))
	draw.Draw(img, img.Bounds(), src, src.Bounds().Min, draw.Src)

	maskStride := ((size + 31) / 32) * 4
	xorSize := size * size * 4
	andSize := maskStride * size

	var buf bytes.Buffer
	binary.Write(&buf, binary.LittleEndian, uint32(40))    // biSize
	binary.Write(&buf, binary.LittleEndian, int32(size))   // biWidth
	binary.Write(&buf, binary.LittleEndian, int32(size*2)) // biHeight: XOR + AND
	binary.Write(&buf, binary.LittleEndian, uint16(1))     // biPlanes
	binary.Write(&buf, binary.LittleEndian, uint16(32))    // biBitCount
	binary.Write(&buf, binary.LittleEndian, uint32(0))     // biCompression: BI_RGB
	binary.Write(&buf, binary.LittleEndian, uint32(xorSize+andSize))
	binary.Write(&buf, binary.LittleEndian, int32(0))
	binary.Write(&buf, binary.LittleEndian, int32(0))
	binary.Write(&buf, binary.LittleEndian, uint32(0))
	binary.Write(&buf, binary.LittleEndian, uint32(0))

	for y := size - 1; y >= 0; y-- {
		for x := 0; x < size; x++ {
			c := img.RGBAAt(x, y)
			buf.Write([]byte{c.B, c.G, c.R, c.A})
		}
	}
	// The AND mask is redundant for 32-bit icons but must still be present and
	// correctly sized; zeroed means "use the alpha channel".
	buf.Write(make([]byte, andSize))
	return buf.Bytes()
}
