package overlay

import "context"

// WindowRect holds a window's position and size in output-relative coordinates.
type WindowRect struct {
	X, Y, W, H int
}

// WindowInfo describes a found window's location and state.
type WindowInfo struct {
	Rect    WindowRect
	Output  string // compositor output name (e.g. "HDMI-1", "DP-2")
	Visible bool
}

// WindowMatcher finds the target window for the overlay.
// Implementations are compositor-specific (see SwayMatcher).
type WindowMatcher interface {
	// FindWindow locates the target window and returns its info.
	// Returns ok=false if the window is not found.
	FindWindow() (info WindowInfo, ok bool)
}

// WindowWatcher is optionally implemented by a WindowMatcher to provide
// event-driven window change notifications instead of polling.
type WindowWatcher interface {
	// WatchEvents returns a channel that fires when window state changes.
	// The channel is closed when the context is cancelled or watching fails.
	WatchEvents(ctx context.Context) <-chan struct{}
}
