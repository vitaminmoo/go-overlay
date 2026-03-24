package overlay

import "math"

// StaticCache pre-renders geometry to a pixel buffer that can be blitted
// to the frame canvas each frame, avoiding per-frame rasterization of
// expensive static content like map boundaries.
type StaticCache struct {
	pix     []byte
	w, h    int
	originX float64
	originY float64
	scaleX  float64
	scaleY  float64
	tag     uint64
	valid   bool
}

// IsValid returns true if the cache was built with matching parameters.
func (sc *StaticCache) IsValid(scaleX, scaleY float64, tag uint64) bool {
	return sc.valid && sc.scaleX == scaleX && sc.scaleY == scaleY && sc.tag == tag
}

// Rebuild re-renders the cache. worldBounds is a list of world-space points
// that define the bounding region to cache. The draw callback receives a
// Canvas that draws into the cache buffer.
func (sc *StaticCache) Rebuild(worldBounds [][2]float64, scaleX, scaleY float64, tag uint64,
	proj Projection, atlas *GlyphAtlas, draw func(*Canvas)) {

	if len(worldBounds) == 0 {
		sc.valid = false
		return
	}

	// Compute screen-space bounding box of all world points
	minSX, minSY := math.Inf(1), math.Inf(1)
	maxSX, maxSY := math.Inf(-1), math.Inf(-1)
	for _, pt := range worldBounds {
		px, py := pt[0], pt[1]
		if proj != nil {
			px, py = proj.Project(pt[0], pt[1])
		}
		sx := px * scaleX
		sy := py * scaleY
		if sx < minSX {
			minSX = sx
		}
		if sx > maxSX {
			maxSX = sx
		}
		if sy < minSY {
			minSY = sy
		}
		if sy > maxSY {
			maxSY = sy
		}
	}

	// Add 2px margin
	minSX -= 2
	minSY -= 2
	maxSX += 2
	maxSY += 2

	w := int(math.Ceil(maxSX - minSX))
	h := int(math.Ceil(maxSY - minSY))
	if w <= 0 || h <= 0 {
		sc.valid = false
		return
	}

	sc.pix = make([]byte, w*h*4)
	sc.w = w
	sc.h = h
	sc.originX = minSX
	sc.originY = minSY
	sc.scaleX = scaleX
	sc.scaleY = scaleY
	sc.tag = tag
	sc.valid = true

	cachePB := newPixBuf(sc.pix, w, h)

	// The cache canvas uses offsetX/offsetY = originX/originY so that
	// world-space drawing lands at the correct cache-relative pixels.
	cacheCanvas := &Canvas{
		pb:         cachePB,
		projection: proj,
		scaleX:     scaleX,
		scaleY:     scaleY,
		offsetX:    sc.originX,
		offsetY:    sc.originY,
		atlas:      atlas,
		screenW:    w,
		screenH:    h,
	}

	draw(cacheCanvas)
}

// Blit copies the visible portion of the cache to the target canvas.
func (sc *StaticCache) Blit(dst *Canvas) {
	if !sc.valid {
		return
	}

	blitX := int(math.Round(sc.originX - dst.offsetX))
	blitY := int(math.Round(sc.originY - dst.offsetY))

	srcX0 := 0
	srcY0 := 0
	dstX0 := blitX
	dstY0 := blitY

	if dstX0 < dst.pb.clipX0 {
		srcX0 += dst.pb.clipX0 - dstX0
		dstX0 = dst.pb.clipX0
	}
	if dstY0 < dst.pb.clipY0 {
		srcY0 += dst.pb.clipY0 - dstY0
		dstY0 = dst.pb.clipY0
	}

	dstX1 := blitX + sc.w
	dstY1 := blitY + sc.h
	if dstX1 > dst.pb.clipX1 {
		dstX1 = dst.pb.clipX1
	}
	if dstY1 > dst.pb.clipY1 {
		dstY1 = dst.pb.clipY1
	}

	copyW := dstX1 - dstX0
	copyH := dstY1 - dstY0
	if copyW <= 0 || copyH <= 0 {
		return
	}

	srcStride := sc.w * 4
	dstStride := dst.pb.stride
	rowBytes := copyW * 4

	for row := 0; row < copyH; row++ {
		srcOff := (srcY0+row)*srcStride + srcX0*4
		dstOff := (dstY0+row)*dstStride + dstX0*4
		copy(dst.pb.pix[dstOff:dstOff+rowBytes], sc.pix[srcOff:srcOff+rowBytes])
	}
}
