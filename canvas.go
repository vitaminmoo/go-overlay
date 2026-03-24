package overlay

import "image/color"

// Canvas is the drawing surface passed to Scene.Draw and RunFunc callbacks.
// It provides both screen-space and world-space drawing methods.
// World-space methods project coordinates through the configured Projection
// and apply camera offset and scale.
type Canvas struct {
	pb         *pixBuf
	projection Projection
	cameraX    float64
	cameraY    float64
	scaleX     float64
	scaleY     float64
	offsetX    float64 // precomputed: projCamX*scaleX - screenW/2
	offsetY    float64 // precomputed: projCamY*scaleY - screenH/2
	atlas      *GlyphAtlas
	scanXs     []float64 // scratch for polygon fill
	quality    QualityLevel
	screenW    int
	screenH    int
	windowRect *WindowRect
}

// reset prepares the canvas for a new frame with a fresh pixel buffer.
func (c *Canvas) reset(pb *pixBuf, w, h int) {
	c.pb = pb
	c.screenW = w
	c.screenH = h
	c.windowRect = nil
	c.quality = QualityFull
	c.recomputeOffset()
}

func (c *Canvas) recomputeOffset() {
	px, py := c.cameraX, c.cameraY
	if c.projection != nil {
		px, py = c.projection.Project(c.cameraX, c.cameraY)
	}
	c.offsetX = px*c.scaleX - float64(c.screenW)/2
	c.offsetY = py*c.scaleY - float64(c.screenH)/2
}

// --- Camera and projection ---

// SetCamera sets the world-space camera center and scale factors.
// The camera center maps to the screen center.
func (c *Canvas) SetCamera(cx, cy, scaleX, scaleY float64) {
	c.cameraX = cx
	c.cameraY = cy
	c.scaleX = scaleX
	c.scaleY = scaleY
	c.recomputeOffset()
}

// SetProjection changes the coordinate projection.
func (c *Canvas) SetProjection(p Projection) {
	c.projection = p
	c.recomputeOffset()
}

// Projection returns the current projection.
func (c *Canvas) Projection() Projection {
	return c.projection
}

// ScaleX returns the current horizontal scale factor.
func (c *Canvas) ScaleX() float64 { return c.scaleX }

// ScaleY returns the current vertical scale factor.
func (c *Canvas) ScaleY() float64 { return c.scaleY }

// --- Coordinate conversion ---

// WorldToScreen converts world coordinates to screen coordinates using the
// current projection, camera, and scale.
func (c *Canvas) WorldToScreen(wx, wy float64) (float64, float64) {
	px, py := wx, wy
	if c.projection != nil {
		px, py = c.projection.Project(wx, wy)
	}
	return px*c.scaleX - c.offsetX, py*c.scaleY - c.offsetY
}

// --- Viewport ---

// Width returns the screen width in pixels.
func (c *Canvas) Width() int { return c.screenW }

// Height returns the screen height in pixels.
func (c *Canvas) Height() int { return c.screenH }

// SetClip restricts drawing to a rectangular region.
func (c *Canvas) SetClip(x, y, w, h int) {
	c.pb.clipX0 = x
	c.pb.clipY0 = y
	c.pb.clipX1 = x + w
	c.pb.clipY1 = y + h
}

// ClearClip removes the clip restriction, allowing drawing to the full surface.
func (c *Canvas) ClearClip() {
	c.pb.clipX0 = 0
	c.pb.clipY0 = 0
	c.pb.clipX1 = c.pb.width
	c.pb.clipY1 = c.pb.height
}

// Clip returns the current clip rectangle as (x, y, w, h).
func (c *Canvas) Clip() (x, y, w, h int) {
	return c.pb.clipX0, c.pb.clipY0,
		c.pb.clipX1 - c.pb.clipX0, c.pb.clipY1 - c.pb.clipY0
}

// --- Quality ---

// Quality returns the current adaptive quality level.
func (c *Canvas) Quality() QualityLevel { return c.quality }

// --- Window ---

// WindowRect returns the tracked target window's position and size,
// or ok=false if no window is being tracked.
func (c *Canvas) WindowRect() (WindowRect, bool) {
	if c.windowRect == nil {
		return WindowRect{}, false
	}
	return *c.windowRect, true
}

// --- Screen-space drawing ---

// SetPixel sets a single pixel at screen coordinates.
func (c *Canvas) SetPixel(x, y int, clr color.Color) {
	r, g, b, a := colorBytes(clr)
	c.pb.setPixel(x, y, r, g, b, a)
}

// Line draws a line between two screen-space points.
func (c *Canvas) Line(x0, y0, x1, y1 float64, clr color.Color) {
	strokeLine(c.pb, x0, y0, x1, y1, clr)
}

// FillRect draws a filled rectangle at screen-space coordinates.
func (c *Canvas) FillRect(x, y, w, h float64, clr color.Color) {
	drawFilledRect(c.pb, x, y, w, h, clr)
}

// Ellipse draws an ellipse outline at screen-space coordinates.
func (c *Canvas) Ellipse(cx, cy, rx, ry float64, clr color.Color) {
	strokeEllipse(c.pb, cx, cy, rx, ry, clr)
}

// FillPolygon fills a polygon defined by screen-space vertex pairs.
func (c *Canvas) FillPolygon(points [][2]float64, clr color.Color) {
	fillPolygon(c.pb, points, clr, &c.scanXs)
}

// Text renders text at screen-space coordinates.
func (c *Canvas) Text(text string, x, y float64, clr color.Color) {
	c.atlas.drawText(c.pb, text, x, y, clr)
}

// TextWidth returns the pixel width of the first line of text.
func (c *Canvas) TextWidth(text string) int {
	return c.atlas.TextWidth(text)
}

// TextLineHeight returns the line height of the current font.
func (c *Canvas) TextLineHeight() int {
	return c.atlas.LineHeight()
}

// --- World-space drawing ---

// WorldLine draws a line between two world-space points.
func (c *Canvas) WorldLine(x0, y0, x1, y1 float64, clr color.Color) {
	sx0, sy0 := c.WorldToScreen(x0, y0)
	sx1, sy1 := c.WorldToScreen(x1, y1)
	strokeLine(c.pb, sx0, sy0, sx1, sy1, clr)
}

// WorldFillRect draws a filled rectangle in world space, projected through
// the current projection. The rectangle is defined by its center and size
// in world coordinates.
func (c *Canvas) WorldFillRect(cx, cy, w, h float64, clr color.Color) {
	halfW, halfH := w/2, h/2
	p0x, p0y := c.WorldToScreen(cx-halfW, cy-halfH)
	p1x, p1y := c.WorldToScreen(cx+halfW, cy-halfH)
	p2x, p2y := c.WorldToScreen(cx+halfW, cy+halfH)
	p3x, p3y := c.WorldToScreen(cx-halfW, cy+halfH)
	fillPolygon(c.pb, [][2]float64{
		{p0x, p0y}, {p1x, p1y}, {p2x, p2y}, {p3x, p3y},
	}, clr, &c.scanXs)
}

// WorldPolygon fills a polygon with vertices in world space.
func (c *Canvas) WorldPolygon(points [][2]float64, clr color.Color) {
	screen := make([][2]float64, len(points))
	for i, pt := range points {
		screen[i][0], screen[i][1] = c.WorldToScreen(pt[0], pt[1])
	}
	fillPolygon(c.pb, screen, clr, &c.scanXs)
}

// WorldOutline draws a closed polygon outline with vertices in world space.
func (c *Canvas) WorldOutline(points [][2]float64, clr color.Color) {
	if len(points) < 2 {
		return
	}
	screen := make([][2]float64, len(points))
	for i, pt := range points {
		screen[i][0], screen[i][1] = c.WorldToScreen(pt[0], pt[1])
	}
	for i := range screen {
		j := (i + 1) % len(screen)
		strokeLine(c.pb, screen[i][0], screen[i][1], screen[j][0], screen[j][1], clr)
	}
}
