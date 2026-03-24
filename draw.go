package overlay

import (
	"image/color"
	"math"
)

// strokeLine draws a line using Bresenham's algorithm.
func strokeLine(pb *pixBuf, x1, y1, x2, y2 float64, clr color.Color) {
	cr, cg, cb, ca := colorBytes(clr)
	ix1 := int(math.Round(x1))
	iy1 := int(math.Round(y1))
	ix2 := int(math.Round(x2))
	iy2 := int(math.Round(y2))

	dx := intAbs(ix2 - ix1)
	dy := -intAbs(iy2 - iy1)
	sx := 1
	if ix1 >= ix2 {
		sx = -1
	}
	sy := 1
	if iy1 >= iy2 {
		sy = -1
	}
	err := dx + dy

	for {
		pb.setPixel(ix1, iy1, cr, cg, cb, ca)
		if ix1 == ix2 && iy1 == iy2 {
			break
		}
		e2 := 2 * err
		if e2 >= dy {
			err += dy
			ix1 += sx
		}
		if e2 <= dx {
			err += dx
			iy1 += sy
		}
	}
}

// drawFilledRect draws a filled axis-aligned rectangle.
func drawFilledRect(pb *pixBuf, x, y, w, h float64, clr color.Color) {
	cr, cg, cb, ca := colorBytes(clr)
	x0 := int(x)
	y0 := int(y)
	x1 := int(x + w)
	y1 := int(y + h)
	if x0 < 0 {
		x0 = 0
	}
	if y0 < 0 {
		y0 = 0
	}
	if x1 > pb.width {
		x1 = pb.width
	}
	if y1 > pb.height {
		y1 = pb.height
	}
	for py := y0; py < y1; py++ {
		for px := x0; px < x1; px++ {
			pb.setPixel(px, py, cr, cg, cb, ca)
		}
	}
}

// strokeEllipse draws an ellipse outline using the midpoint ellipse algorithm.
func strokeEllipse(pb *pixBuf, cx, cy, rx, ry float64, clr color.Color) {
	cr, cg, cb, ca := colorBytes(clr)
	icx := int(math.Round(cx))
	icy := int(math.Round(cy))
	a := int(math.Round(rx))
	b := int(math.Round(ry))
	if a <= 0 || b <= 0 {
		return
	}

	plot := func(x, y int) {
		pb.setPixel(icx+x, icy+y, cr, cg, cb, ca)
		pb.setPixel(icx-x, icy+y, cr, cg, cb, ca)
		pb.setPixel(icx+x, icy-y, cr, cg, cb, ca)
		pb.setPixel(icx-x, icy-y, cr, cg, cb, ca)
	}

	a2 := a * a
	b2 := b * b

	// Region 1: slope magnitude < 1
	x, y := 0, b
	d := 4*b2 - 4*a2*b + a2
	plot(x, y)
	for 2*b2*x < 2*a2*y {
		if d < 0 {
			d += 4 * b2 * (2*x + 3)
		} else {
			d += 4*b2*(2*x+3) + 4*a2*(-2*y+2)
			y--
		}
		x++
		plot(x, y)
	}

	// Region 2: slope magnitude >= 1
	fx := float64(x) + 0.5
	fy := float64(y) - 1.0
	d2 := float64(b2)*fx*fx + float64(a2)*fy*fy - float64(a2*b2)
	for y > 0 {
		if d2 > 0 {
			d2 += float64(a2) * (-2.0*float64(y) + 3.0)
		} else {
			d2 += float64(b2)*(2.0*float64(x)+2.0) + float64(a2)*(-2.0*float64(y)+3.0)
			x++
		}
		y--
		plot(x, y)
	}
}

// fillPolygon fills a polygon using scanline fill with direct pixel access.
// scanXs is a scratch buffer for intersection x-coordinates, reused across calls.
func fillPolygon(pb *pixBuf, points [][2]float64, clr color.Color, scanXs *[]float64) {
	if len(points) < 3 {
		return
	}

	cr, cg, cb, ca := colorBytes(clr)

	minY := points[0][1]
	maxY := points[0][1]
	for _, p := range points[1:] {
		if p[1] < minY {
			minY = p[1]
		}
		if p[1] > maxY {
			maxY = p[1]
		}
	}

	iy0 := int(math.Floor(minY))
	iy1 := int(math.Ceil(maxY))
	if iy0 < 0 {
		iy0 = 0
	}
	if iy1 > pb.height {
		iy1 = pb.height
	}

	n := len(points)
	for y := iy0; y < iy1; y++ {
		fy := float64(y) + 0.5
		xs := (*scanXs)[:0]
		for i := range n {
			j := (i + 1) % n
			y0 := points[i][1]
			y1 := points[j][1]
			if y0 == y1 {
				continue
			}
			if (fy < y0 && fy < y1) || (fy >= y0 && fy >= y1) {
				continue
			}
			t := (fy - y0) / (y1 - y0)
			ix := points[i][0] + t*(points[j][0]-points[i][0])
			xs = append(xs, ix)
		}
		// Sort intersections (insertion sort, usually very few)
		for i := 0; i < len(xs)-1; i++ {
			for j := i + 1; j < len(xs); j++ {
				if xs[j] < xs[i] {
					xs[i], xs[j] = xs[j], xs[i]
				}
			}
		}
		*scanXs = xs
		// Fill between pairs
		for i := 0; i+1 < len(xs); i += 2 {
			x0 := int(math.Ceil(xs[i]))
			x1 := int(math.Floor(xs[i+1]))
			if x0 < 0 {
				x0 = 0
			}
			if x1 >= pb.width {
				x1 = pb.width - 1
			}
			for x := x0; x <= x1; x++ {
				pb.setPixel(x, y, cr, cg, cb, ca)
			}
		}
	}
}

// ClipLineToRect clips a line endpoint to a rectangle. Returns whether the
// endpoint was clipped and the new endpoint coordinates.
func ClipLineToRect(x1, y1, x2, y2, minX, minY, maxX, maxY float64) (clipped bool, newX2, newY2 float64) {
	if x2 >= minX && x2 < maxX && y2 >= minY && y2 < maxY {
		return false, x2, y2
	}

	dx := x2 - x1
	dy := y2 - y1

	minT := 1.0

	if dx < 0 {
		minT = math.Min(minT, (minX-x1)/dx)
	}
	if dx > 0 {
		minT = math.Min(minT, (maxX-1-x1)/dx)
	}
	if dy < 0 {
		minT = math.Min(minT, (minY-y1)/dy)
	}
	if dy > 0 {
		minT = math.Min(minT, (maxY-1-y1)/dy)
	}

	if minT < 1.0 && minT > 0 {
		return true, x1 + minT*dx, y1 + minT*dy
	}

	return false, x2, y2
}

func intAbs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}
