package overlay

import (
	"fmt"
	"log"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
)

// WmctrlMatcher finds windows using wmctrl and xrandr.
// Works on any compositor/WM with X11 or Xwayland support.
//
// On Wayland with fractional scaling, X11 coordinates (from wmctrl) differ
// from Wayland logical coordinates. Set Scale to the fractional scale factor
// (e.g. 1.35) or call DetectScale to auto-detect it.
type WmctrlMatcher struct {
	// Match returns true for window titles that should be tracked.
	Match func(windowName string) bool

	// Scale is the display scale factor. X11 coordinates are divided by this
	// to produce Wayland logical coordinates. Default 0 means auto-detect
	// on first FindWindow call.
	Scale float64

	trackedID string // hex window ID for stable tracking
}

// NewWmctrlMatcher creates a WindowMatcher that uses wmctrl to find windows
// and xrandr to map coordinates to output names. Scale is auto-detected.
func NewWmctrlMatcher(match func(string) bool) *WmctrlMatcher {
	return &WmctrlMatcher{Match: match}
}

type wmctrlWindow struct {
	id    string // hex ID like "0x06a00003"
	x, y  int
	w, h  int
	title string
	// Frame extents: left, right, top, bottom (from _NET_FRAME_EXTENTS)
	frameL, frameR, frameT, frameB int
}

// FindWindow implements WindowMatcher.
func (wm *WmctrlMatcher) FindWindow() (WindowInfo, bool) {
	windows, err := wmctrlList()
	if err != nil {
		return WindowInfo{}, false
	}

	var candidates []wmctrlWindow
	for _, w := range windows {
		if wm.Match(w.title) {
			candidates = append(candidates, w)
		}
	}

	if len(candidates) == 0 {
		return WindowInfo{}, false
	}

	// Prefer previously tracked window.
	var win wmctrlWindow
	if wm.trackedID != "" {
		for _, c := range candidates {
			if c.id == wm.trackedID {
				win = c
				break
			}
		}
	}
	if win.id == "" {
		win = candidates[0]
		if wm.trackedID != win.id {
			log.Printf("wmctrl: locking onto window %q id=%s (%d candidates)",
				win.title, win.id, len(candidates))
		}
		wm.trackedID = win.id
	}

	// Read frame extents to get content area.
	win.frameL, win.frameR, win.frameT, win.frameB = xpropFrameExtents(win.id)

	// Adjust to content rect (inside decorations).
	contentX := win.x + win.frameL
	contentY := win.y + win.frameT
	contentW := win.w - win.frameL - win.frameR
	contentH := win.h - win.frameT - win.frameB

	// Map to output using xrandr.
	outputs, err := xrandrOutputs()
	if err != nil || len(outputs) == 0 {
		// Fallback: return content coordinates as-is with empty output.
		return WindowInfo{
			Rect:    WindowRect{X: contentX, Y: contentY, W: contentW, H: contentH},
			Visible: true,
		}, true
	}

	// Auto-detect scale on first call.
	if wm.Scale == 0 {
		wm.Scale = detectScale(outputs)
		if wm.Scale != 1.0 {
			log.Printf("wmctrl: detected display scale %.3f", wm.Scale)
		}
	}

	cx := contentX + contentW/2
	cy := contentY + contentH/2
	for _, o := range outputs {
		if cx >= o.x && cx < o.x+o.w && cy >= o.y && cy < o.y+o.h {
			return WindowInfo{
				Rect:    wm.scaleRect(contentX-o.x, contentY-o.y, contentW, contentH),
				Output:  o.name,
				Visible: true,
			}, true
		}
	}

	// Window center not on any output; use first output.
	o := outputs[0]
	return WindowInfo{
		Rect:    wm.scaleRect(contentX-o.x, contentY-o.y, contentW, contentH),
		Output:  o.name,
		Visible: true,
	}, true
}

func (wm *WmctrlMatcher) scaleRect(x, y, w, h int) WindowRect {
	if wm.Scale <= 1.0 {
		return WindowRect{X: x, Y: y, W: w, H: h}
	}
	return WindowRect{
		X: int(float64(x) / wm.Scale),
		Y: int(float64(y) / wm.Scale),
		W: int(float64(w) / wm.Scale),
		H: int(float64(h) / wm.Scale),
	}
}

// detectScale attempts to determine the display scale factor by comparing
// the X11 DPI (from xrdb) against the standard 96 DPI baseline.
func detectScale(outputs []xrandrOutput) float64 {
	// Try xrdb Xft.dpi — most reliable on Wayland compositors.
	if dpi := xrdbDPI(); dpi > 0 {
		scale := float64(dpi) / 96.0
		if scale > 1.0 {
			return scale
		}
	}
	return 1.0
}

// xrdbDPI reads Xft.dpi from the X resource database.
func xrdbDPI() int {
	out, err := exec.Command("xrdb", "-query").Output()
	if err != nil {
		return 0
	}
	for _, line := range strings.Split(string(out), "\n") {
		if strings.HasPrefix(line, "Xft.dpi:") {
			val := strings.TrimSpace(strings.TrimPrefix(line, "Xft.dpi:"))
			if v, err := strconv.Atoi(val); err == nil {
				return v
			}
		}
	}
	return 0
}

// xpropFrameExtents reads _NET_FRAME_EXTENTS for a window ID.
// Returns left, right, top, bottom decoration sizes in X11 pixels.
func xpropFrameExtents(windowID string) (left, right, top, bottom int) {
	out, err := exec.Command("xprop", "-id", windowID, "_NET_FRAME_EXTENTS").Output()
	if err != nil {
		return 0, 0, 0, 0
	}
	// Format: _NET_FRAME_EXTENTS(CARDINAL) = 0, 0, 37, 0
	s := string(out)
	idx := strings.Index(s, "= ")
	if idx < 0 {
		return 0, 0, 0, 0
	}
	parts := strings.Split(s[idx+2:], ",")
	if len(parts) < 4 {
		return 0, 0, 0, 0
	}
	l, _ := strconv.Atoi(strings.TrimSpace(parts[0]))
	r, _ := strconv.Atoi(strings.TrimSpace(parts[1]))
	t, _ := strconv.Atoi(strings.TrimSpace(strings.TrimRight(parts[2], "\n")))
	b, _ := strconv.Atoi(strings.TrimSpace(strings.TrimRight(parts[3], "\n")))
	return l, r, t, b
}

// wmctrlList parses `wmctrl -l -G` output.
func wmctrlList() ([]wmctrlWindow, error) {
	out, err := exec.Command("wmctrl", "-l", "-G").Output()
	if err != nil {
		return nil, fmt.Errorf("wmctrl: %w", err)
	}

	var windows []wmctrlWindow
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line == "" {
			continue
		}
		// Format: ID DESKTOP X Y W H HOST TITLE...
		fields := strings.Fields(line)
		if len(fields) < 8 {
			continue
		}
		x, _ := strconv.Atoi(fields[2])
		y, _ := strconv.Atoi(fields[3])
		w, _ := strconv.Atoi(fields[4])
		h, _ := strconv.Atoi(fields[5])
		title := strings.Join(fields[7:], " ")
		windows = append(windows, wmctrlWindow{
			id:    fields[0],
			x:     x,
			y:     y,
			w:     w,
			h:     h,
			title: title,
		})
	}
	return windows, nil
}

type xrandrOutput struct {
	name    string
	x, y    int
	w, h    int
}

// xrandr output line: "eDP-1 connected primary 2256x1504+0+0 ..."
var xrandrRe = regexp.MustCompile(`^(\S+)\s+connected\s+(?:primary\s+)?(\d+)x(\d+)\+(\d+)\+(\d+)`)

func xrandrOutputs() ([]xrandrOutput, error) {
	out, err := exec.Command("xrandr", "--query").Output()
	if err != nil {
		return nil, fmt.Errorf("xrandr: %w", err)
	}

	var outputs []xrandrOutput
	for _, line := range strings.Split(string(out), "\n") {
		m := xrandrRe.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		w, _ := strconv.Atoi(m[2])
		h, _ := strconv.Atoi(m[3])
		x, _ := strconv.Atoi(m[4])
		y, _ := strconv.Atoi(m[5])
		outputs = append(outputs, xrandrOutput{
			name: m[1],
			x:    x, y: y,
			w:    w, h: h,
		})
	}
	return outputs, nil
}
