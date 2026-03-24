package overlay

import "image/color"

// Shape represents a visual marker style for entities.
type Shape string

const (
	ShapeCross      Shape = "cross"      // hollow cross / plus outline
	ShapeFilledRect Shape = "filledRect" // 8x8 filled rectangle
	ShapeDot        Shape = "dot"        // 4x4 filled dot
	ShapeDiamond    Shape = "diamond"    // rhombus outline in world space
	ShapeEllipse    Shape = "ellipse"    // ellipse with crosshairs
	ShapeWorldRect  Shape = "worldRect"  // filled rectangle with world-space dimensions
	ShapeNone       Shape = "none"       // no visual marker
)

// AllShapes lists all available shape values.
var AllShapes = []Shape{ShapeCross, ShapeFilledRect, ShapeDot, ShapeDiamond, ShapeEllipse, ShapeWorldRect, ShapeNone}

// DrawShape draws a shape at a world-space position.
// sizeX/sizeY are world-space dimensions used by ShapeWorldRect (0 = default 2).
func DrawShape(c *Canvas, shape Shape, wx, wy float64, clr color.Color, sizeX, sizeY float64) {
	switch shape {
	case ShapeCross:
		drawCross(c, wx, wy, clr)
	case ShapeFilledRect:
		sx, sy := c.WorldToScreen(wx, wy)
		c.FillRect(sx-4, sy-4, 8, 8, clr)
	case ShapeDot:
		sx, sy := c.WorldToScreen(wx, wy)
		c.FillRect(sx-2, sy-2, 4, 4, clr)
	case ShapeDiamond:
		drawDiamond(c, wx, wy, clr)
	case ShapeEllipse:
		drawEllipseShape(c, wx, wy, clr)
	case ShapeWorldRect:
		if sizeX <= 0 {
			sizeX = 2
		}
		if sizeY <= 0 {
			sizeY = 2
		}
		c.WorldFillRect(wx, wy, sizeX, sizeY, clr)
	case ShapeNone:
		// intentionally draw nothing
	default:
		drawCross(c, wx, wy, clr)
	}
}

// drawCross draws a 12-point cross outline in world space.
func drawCross(c *Canvas, wx, wy float64, clr color.Color) {
	const unitSize = 6.0
	s2 := unitSize / 2.0
	s6 := unitSize / 6.0

	worldPoints := [12][2]float64{
		{-s6, -s2}, {s6, -s2}, {s6, -s6},
		{s2, -s6}, {s2, s6}, {s6, s6},
		{s6, s2}, {-s6, s2}, {-s6, s6},
		{-s2, s6}, {-s2, -s6}, {-s6, -s6},
	}

	var screen [12][2]float64
	for i, pt := range worldPoints {
		screen[i][0], screen[i][1] = c.WorldToScreen(wx+pt[0], wy+pt[1])
	}

	for i := range screen {
		j := (i + 1) % len(screen)
		c.Line(screen[i][0], screen[i][1], screen[j][0], screen[j][1], clr)
	}
}

// drawDiamond draws a rhombus outline in world space.
func drawDiamond(c *Canvas, wx, wy float64, clr color.Color) {
	const size = 4.0
	points := [4][2]float64{
		{wx, wy - size},     // top
		{wx + size, wy},     // right
		{wx, wy + size},     // bottom
		{wx - size, wy},     // left
	}

	var screen [4][2]float64
	for i, pt := range points {
		screen[i][0], screen[i][1] = c.WorldToScreen(pt[0], pt[1])
	}

	for i := range screen {
		j := (i + 1) % 4
		c.Line(screen[i][0], screen[i][1], screen[j][0], screen[j][1], clr)
	}
}

// drawEllipseShape draws an ellipse with crosshairs, scaled to the current scale.
func drawEllipseShape(c *Canvas, wx, wy float64, clr color.Color) {
	sx, sy := c.WorldToScreen(wx, wy)

	baseRx := 24.0 / 8.0
	baseRy := 12.0 / 8.0
	baseLineXOff := 16.0 / 8.0
	baseLineYOff := 8.0 / 8.0

	rx := baseRx * c.scaleX
	ry := baseRy * c.scaleY
	lineXOff := baseLineXOff * c.scaleX
	lineYOff := baseLineYOff * c.scaleY

	c.Ellipse(sx, sy, rx, ry, clr)
	c.Line(sx-lineXOff, sy, sx+lineXOff, sy, clr)
	c.Line(sx, sy-lineYOff, sx, sy+lineYOff, clr)
}
