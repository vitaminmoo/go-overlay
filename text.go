package overlay

import (
	"image/color"

	"golang.org/x/image/font"
	"golang.org/x/image/font/basicfont"
	"golang.org/x/image/math/fixed"
)

// GlyphAtlas holds pre-rasterized alpha masks for ASCII glyphs,
// enabling direct pixel blitting without per-frame font rendering.
type GlyphAtlas struct {
	glyphs     [96]glyphData // 0x20..0x7F
	lineHeight int           // vertical spacing between lines
	ascent     int           // pixels above baseline
}

type glyphData struct {
	mask    []byte // alpha values, width*height
	width   int
	height  int
	offsetX int // horizontal offset from pen position to glyph left
	offsetY int // vertical offset from line top to glyph top
	advance int // horizontal advance to next character
}

// NewGlyphAtlas creates a glyph atlas from any font.Face by pre-rasterizing
// all printable ASCII characters.
func NewGlyphAtlas(face font.Face) *GlyphAtlas {
	metrics := face.Metrics()
	atlas := &GlyphAtlas{
		lineHeight: metrics.Height.Ceil(),
		ascent:     metrics.Ascent.Ceil(),
	}

	for r := rune(0x20); r < 0x80; r++ {
		idx := int(r) - 0x20

		dot := fixed.P(0, atlas.ascent)
		dr, mask, maskp, advance, ok := face.Glyph(dot, r)
		if !ok {
			continue
		}

		w := dr.Dx()
		h := dr.Dy()
		adv := advance.Ceil()

		if w <= 0 || h <= 0 {
			atlas.glyphs[idx] = glyphData{advance: adv}
			continue
		}

		alphaMask := make([]byte, w*h)
		for py := 0; py < h; py++ {
			for px := 0; px < w; px++ {
				_, _, _, a := mask.At(maskp.X+px, maskp.Y+py).RGBA()
				alphaMask[py*w+px] = byte(a >> 8)
			}
		}

		atlas.glyphs[idx] = glyphData{
			mask:    alphaMask,
			width:   w,
			height:  h,
			offsetX: dr.Min.X,
			offsetY: dr.Min.Y,
			advance: adv,
		}
	}

	return atlas
}

// DefaultGlyphAtlas creates a glyph atlas using basicfont.Face7x13.
func DefaultGlyphAtlas() *GlyphAtlas {
	return NewGlyphAtlas(basicfont.Face7x13)
}

// LineHeight returns the vertical spacing between text lines.
func (a *GlyphAtlas) LineHeight() int {
	return a.lineHeight
}

// TextWidth returns the pixel width of the first line of text.
func (a *GlyphAtlas) TextWidth(text string) int {
	w := 0
	for i := 0; i < len(text); i++ {
		if text[i] == '\n' {
			break
		}
		idx := int(text[i]) - 0x20
		if idx >= 0 && idx < len(a.glyphs) {
			w += a.glyphs[idx].advance
		}
	}
	return w
}

// drawText renders multi-line text by blitting glyphs directly to the pixel buffer.
func (a *GlyphAtlas) drawText(pb *pixBuf, text string, x, y float64, clr color.Color) {
	cr, cg, cb, ca := colorBytes(clr)
	penX := int(x)
	startX := penX
	lineIdx := 0

	for i := 0; i < len(text); i++ {
		ch := text[i]
		if ch == '\n' {
			lineIdx++
			penX = startX
			continue
		}

		idx := int(ch) - 0x20
		if idx < 0 || idx >= len(a.glyphs) {
			continue
		}

		g := &a.glyphs[idx]
		if g.mask == nil {
			penX += g.advance
			continue
		}

		glyphX := penX + g.offsetX
		glyphY := int(y) + g.offsetY + lineIdx*a.lineHeight

		for py := 0; py < g.height; py++ {
			sy := glyphY + py
			if sy < pb.clipY0 || sy >= pb.clipY1 {
				continue
			}
			rowOff := py * g.width
			for px := 0; px < g.width; px++ {
				if g.mask[rowOff+px] == 0 {
					continue
				}
				sx := glyphX + px
				if sx < pb.clipX0 || sx >= pb.clipX1 {
					continue
				}
				pb.setPixel(sx, sy, cr, cg, cb, ca)
			}
		}

		penX += g.advance
	}
}
