# go-overlay

A Go library for drawing transparent, click-through overlays on the screen under
Wayland, using the [`wlr-layer-shell`](https://wayland.app/protocols/wlr-layer-shell-unstable-v1)
protocol. It can optionally track a target window and keep the overlay aligned to
it, so you can draw annotations, HUDs, or debug graphics on top of another
application.

> [!WARNING]
> **Pre-alpha.** APIs will change, things are rough, and breakage is expected.
> This is a personal project shared in the hope it's useful, not a polished
> release. See [Status & limitations](#status--limitations) before relying on it.

## Status & limitations

- **Linux + Wayland only.** It depends on `wlr-layer-shell`, so it will not work
  on X11, macOS, or Windows.
- **Best supported on sway** (and other wlroots-based compositors). Window
  tracking uses sway's IPC, including event-driven updates.
- **KDE / KWin is partial and rough.** It works through `kdotool` with polling
  only, and is not reliable. Treat it as experimental.
- **X11 window matching exists** via `wmctrl`/`xrandr`/`xprop`, but the overlay
  surface itself still requires a Wayland layer-shell compositor.
- No stability guarantees on the API or behavior.

## Requirements

- Go (see `go.mod` for the version)
- A Wayland compositor that implements `wlr-layer-shell` (e.g. sway)
- For window tracking, depending on your compositor:
  - **sway:** `swaymsg` / sway IPC
  - **KDE/KWin:** [`kdotool`](https://github.com/jinliu/kdotool)
  - **X11:** `wmctrl`, `xrandr`, `xprop`, `xrdb`

## Usage

`go-overlay` is a library (package `overlay`). You implement a `Scene` — or just
pass a draw function — and call `Run`.

```go
package main

import (
	"context"
	"image/color"
	"strings"

	overlay "github.com/vitaminmoo/go-overlay"
)

func main() {
	opts := overlay.Options{
		// Track a window whose title contains "Firefox" (sway).
		WindowMatcher: overlay.NewSwayMatcher(func(title string) bool {
			return strings.Contains(title, "Firefox")
		}),
	}

	red := color.RGBA{R: 255, A: 255}

	// RunFunc draws each frame with the given callback.
	_ = overlay.RunFunc(context.Background(), opts, func(c *overlay.Canvas) {
		c.FillRect(10, 10, 200, 40, red)
		c.Text("hello overlay", 16, 24, red)
	})
}
```

For stateful overlays, implement the `Scene` interface instead:

```go
type Scene interface {
	Update(ctx context.Context) // runs on a background goroutine at Options.UpdateRate
	Draw(canvas *Canvas)        // runs on the Wayland frame callback (vsync)
}
```

and call `overlay.Run(ctx, opts, scene)`.

### Window matchers

Each matcher takes a predicate over the window title and reports the matched
window's geometry and output:

- `overlay.NewSwayMatcher(match)` — sway IPC, event-driven (recommended)
- `overlay.NewKWinMatcher(match)` — KDE/KWin via `kdotool`, polling, experimental
- `overlay.NewWmctrlMatcher(match)` — X11 via `wmctrl`/`xrandr`/`xprop`

If `WindowMatcher` is nil, the overlay simply covers the default output with no
window tracking.

### Drawing

The `Canvas` provides both screen-space and world-space primitives: pixels,
lines, filled/outlined rectangles, ellipses, polygons, and text, plus camera and
projection transforms, clipping, and optional adaptive quality that degrades
rendering when frames exceed their time budget.

## License

No license has been chosen yet. Until one is added, no usage rights are granted.
