package overlay

import (
	"fmt"
	"log"
	"os/exec"
	"strconv"
	"strings"
)

// KWinMatcher finds windows using kdotool on KDE Plasma Wayland.
// Returns geometry in logical (Wayland-native) coordinates, avoiding
// X11/Xwayland coordinate space issues with fractional scaling.
//
// Requires kdotool to be installed (cargo install kdotool).
type KWinMatcher struct {
	// Match returns true for window names that should be tracked.
	Match func(windowName string) bool

	trackedID string
}

// NewKWinMatcher creates a WindowMatcher that uses kdotool to find windows.
func NewKWinMatcher(match func(string) bool) *KWinMatcher {
	return &KWinMatcher{Match: match}
}

// FindWindow implements WindowMatcher.
func (km *KWinMatcher) FindWindow() (WindowInfo, bool) {
	// Search all windows.
	out, err := exec.Command("kdotool", "search", "--name", "").Output()
	if err != nil {
		return WindowInfo{}, false
	}

	ids := strings.Fields(strings.TrimSpace(string(out)))
	if len(ids) == 0 {
		return WindowInfo{}, false
	}

	// Find matching windows by name.
	type candidate struct {
		id   string
		name string
	}
	var candidates []candidate
	for _, id := range ids {
		name := kdotoolGetName(id)
		if name != "" && km.Match(name) {
			candidates = append(candidates, candidate{id: id, name: name})
		}
	}

	if len(candidates) == 0 {
		return WindowInfo{}, false
	}

	// Prefer previously tracked window.
	var win candidate
	if km.trackedID != "" {
		for _, c := range candidates {
			if c.id == km.trackedID {
				win = c
				break
			}
		}
	}
	if win.id == "" {
		win = candidates[0]
		if km.trackedID != win.id {
			log.Printf("kwin: locking onto window %q id=%s (%d candidates)",
				win.name, win.id, len(candidates))
		}
		km.trackedID = win.id
	}

	// Get geometry in logical coordinates.
	x, y, w, h, err := kdotoolGetGeometry(win.id)
	if err != nil {
		return WindowInfo{}, false
	}

	// Determine output (single-output: use first xrandr output name).
	output := firstOutputName()

	return WindowInfo{
		Rect: WindowRect{
			X: int(x + 0.5),
			Y: int(y + 0.5),
			W: int(w + 0.5),
			H: int(h + 0.5),
		},
		Output:  output,
		Visible: true,
	}, true
}

func kdotoolGetName(id string) string {
	out, err := exec.Command("kdotool", "getwindowname", id).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func kdotoolGetGeometry(id string) (x, y, w, h float64, err error) {
	out, oerr := exec.Command("kdotool", "getwindowgeometry", id).Output()
	if oerr != nil {
		return 0, 0, 0, 0, fmt.Errorf("kdotool getwindowgeometry: %w", oerr)
	}

	// kdotool returns frame geometry (including title bar).
	// Parse position and size, then adjust for title bar below.
	var frameX, frameY, frameW, frameH float64
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "Position:") {
			coords := strings.TrimSpace(strings.TrimPrefix(line, "Position:"))
			parts := strings.Split(coords, ",")
			if len(parts) == 2 {
				frameX, _ = strconv.ParseFloat(parts[0], 64)
				frameY, _ = strconv.ParseFloat(parts[1], 64)
			}
		} else if strings.HasPrefix(line, "Geometry:") {
			dims := strings.TrimSpace(strings.TrimPrefix(line, "Geometry:"))
			parts := strings.Split(dims, "x")
			if len(parts) == 2 {
				frameW, _ = strconv.ParseFloat(parts[0], 64)
				frameH, _ = strconv.ParseFloat(parts[1], 64)
			}
		}
	}

	if frameW == 0 || frameH == 0 {
		return 0, 0, 0, 0, fmt.Errorf("failed to parse geometry from kdotool output")
	}

	// Adjust frame geometry to client geometry (strip title bar / borders).
	// Find the matching X11 window via wmctrl to get _NET_FRAME_EXTENTS,
	// then scale from X11 pixels to logical using the known frame dimensions.
	name := kdotoolGetName(id)
	if name != "" {
		if windows, werr := wmctrlList(); werr == nil {
			for _, win := range windows {
				if win.title == name && win.w > 0 && win.h > 0 {
					fl, fr, ft, fb := xpropFrameExtents(win.id)
					if fl+fr+ft+fb > 0 {
						// Scale factor: logical (kdotool) / X11 (wmctrl)
						sx := frameW / float64(win.w)
						sy := frameH / float64(win.h)
						frameX += float64(fl) * sx
						frameY += float64(ft) * sy
						frameW -= float64(fl+fr) * sx
						frameH -= float64(ft+fb) * sy
					}
					break
				}
			}
		}
	}

	return frameX, frameY, frameW, frameH, nil
}

func firstOutputName() string {
	outputs, err := xrandrOutputs()
	if err != nil || len(outputs) == 0 {
		return ""
	}
	return outputs[0].name
}
