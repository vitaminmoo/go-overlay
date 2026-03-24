package overlay

import (
	"image/color"

	"deedles.dev/ximage/format"
)

// pixBuf provides fast direct pixel access to an ARGB8888 buffer,
// bypassing the expensive color model conversion in format.Image.Set().
type pixBuf struct {
	pix    []byte
	stride int
	width  int
	height int
	// Clip rect — setPixel only writes within these bounds.
	clipX0, clipY0, clipX1, clipY1 int
}

func newPixBufFromImage(fimg *format.Image) *pixBuf {
	bounds := fimg.Rect
	w := bounds.Dx()
	h := bounds.Dy()
	return &pixBuf{
		pix:    fimg.Pix,
		stride: w * 4,
		width:  w,
		height: h,
		clipX0: 0,
		clipY0: 0,
		clipX1: w,
		clipY1: h,
	}
}

func newPixBuf(pix []byte, width, height int) *pixBuf {
	return &pixBuf{
		pix:    pix,
		stride: width * 4,
		width:  width,
		height: height,
		clipX0: 0,
		clipY0: 0,
		clipX1: width,
		clipY1: height,
	}
}

// setPixel writes a pixel in ARGB8888 little-endian format (B, G, R, A bytes).
func (pb *pixBuf) setPixel(x, y int, r, g, b, a byte) {
	if x < pb.clipX0 || x >= pb.clipX1 || y < pb.clipY0 || y >= pb.clipY1 {
		return
	}
	i := y*pb.stride + x*4
	pb.pix[i] = b
	pb.pix[i+1] = g
	pb.pix[i+2] = r
	pb.pix[i+3] = a
}

// colorBytes extracts RGBA byte components from a color.Color.
func colorBytes(c color.Color) (r, g, b, a byte) {
	if rgba, ok := c.(color.RGBA); ok {
		return rgba.R, rgba.G, rgba.B, rgba.A
	}
	r16, g16, b16, a16 := c.RGBA()
	return byte(r16 >> 8), byte(g16 >> 8), byte(b16 >> 8), byte(a16 >> 8)
}
