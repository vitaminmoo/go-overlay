package overlay

// Projection defines a coordinate transform from world space to projected space.
// The projected coordinates are then scaled and offset by the camera to produce
// screen coordinates.
type Projection interface {
	// Project converts world coordinates to projected coordinates.
	Project(wx, wy float64) (px, py float64)
}

// IsometricProjection implements a 2:1 dimetric isometric projection.
// Used by games like Diablo 2, Age of Empires, etc.
//
//	px = wx - wy
//	py = (wx + wy) / 2
type IsometricProjection struct{}

func (IsometricProjection) Project(wx, wy float64) (float64, float64) {
	return wx - wy, (wx + wy) / 2.0
}

// OrthogonalProjection is a 1:1 identity projection.
// Used by sidescrollers, top-down games, etc.
type OrthogonalProjection struct{}

func (OrthogonalProjection) Project(wx, wy float64) (float64, float64) {
	return wx, wy
}
