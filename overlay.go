package overlay

import (
	"context"
	"log"
	"sync/atomic"
	"time"
)

// Scene is implemented by the user to provide update and draw logic.
// Update runs on a background goroutine at the configured rate.
// Draw runs on the Wayland frame callback (vsync).
// Thread safety between Update and Draw is the user's responsibility
// (e.g. using atomic.Pointer for snapshot passing).
type Scene interface {
	Update(ctx context.Context)
	Draw(canvas *Canvas)
}

// Options configures the overlay.
type Options struct {
	// WindowMatcher finds the target window for overlay positioning.
	// If nil, the overlay covers the default output with no window tracking.
	WindowMatcher WindowMatcher

	// UpdateRate is how often Scene.Update is called. Default: 50ms (20Hz).
	// Set to 0 to disable the update loop (used internally by RunFunc).
	UpdateRate time.Duration

	// Projection sets the coordinate transform for world-space drawing.
	// Default: OrthogonalProjection.
	Projection Projection

	// GlyphAtlas provides the font for text rendering.
	// Default: basicfont.Face7x13 via DefaultGlyphAtlas().
	GlyphAtlas *GlyphAtlas

	// Namespace is the wlr-layer-shell namespace. Default: "overlay".
	Namespace string

	// AdaptiveQuality enables automatic quality degradation when frames
	// exceed the time budget. Nil disables adaptive quality.
	AdaptiveQuality *AdaptiveQualityOptions
}

func applyDefaults(opts *Options) {
	if opts.UpdateRate == 0 {
		opts.UpdateRate = 50 * time.Millisecond
	}
	if opts.Projection == nil {
		opts.Projection = OrthogonalProjection{}
	}
	if opts.GlyphAtlas == nil {
		opts.GlyphAtlas = DefaultGlyphAtlas()
	}
	if opts.Namespace == "" {
		opts.Namespace = "overlay"
	}
}

type overlayManager struct {
	ctx    context.Context
	cancel context.CancelFunc
	opts   Options
	scene  Scene
	canvas *Canvas

	windowRect     atomic.Pointer[WindowRect]
	windowHidden   atomic.Bool
	qualityTracker *qualityTracker
}

// Run displays a transparent click-through overlay and runs the Scene's
// update and draw loops until the context is cancelled.
func Run(ctx context.Context, opts Options, scene Scene) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	applyDefaults(&opts)

	mgr := &overlayManager{
		ctx:    ctx,
		cancel: cancel,
		opts:   opts,
		scene:  scene,
		canvas: &Canvas{
			atlas:      opts.GlyphAtlas,
			projection: opts.Projection,
		},
		qualityTracker: newQualityTracker(opts.AdaptiveQuality),
	}

	if opts.UpdateRate > 0 {
		go mgr.updateLoop()
	}

	switchOutput := make(chan string, 1)

	if opts.WindowMatcher != nil {
		go mgr.windowDetectionLoop(switchOutput)
	}

	return mgr.runWayland(switchOutput)
}

// RunFunc is a simplified entry point that calls draw on every frame
// without a separate update goroutine.
func RunFunc(ctx context.Context, opts Options, draw func(*Canvas)) error {
	opts.UpdateRate = 0
	return Run(ctx, opts, &funcScene{draw: draw})
}

type funcScene struct {
	draw func(*Canvas)
}

func (f *funcScene) Update(ctx context.Context) {}
func (f *funcScene) Draw(canvas *Canvas)        { f.draw(canvas) }

func (m *overlayManager) updateLoop() {
	ticker := time.NewTicker(m.opts.UpdateRate)
	defer ticker.Stop()
	for {
		select {
		case <-m.ctx.Done():
			return
		case <-ticker.C:
			m.scene.Update(m.ctx)
		}
	}
}

func (m *overlayManager) windowDetectionLoop(switchOutput chan<- string) {
	matcher := m.opts.WindowMatcher
	currentOutput := ""

	// Initial check
	if info, ok := matcher.FindWindow(); ok {
		wr := info.Rect
		m.windowRect.Store(&wr)
		m.windowHidden.Store(!info.Visible)
		if info.Output != "" {
			currentOutput = info.Output
			select {
			case switchOutput <- info.Output:
			default:
			}
		}
	}

	// Event-driven if supported
	var watchCh <-chan struct{}
	if watcher, ok := matcher.(WindowWatcher); ok {
		watchCh = watcher.WatchEvents(m.ctx)
	}

	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	refresh := func() {
		info, ok := matcher.FindWindow()
		if !ok {
			m.windowHidden.Store(true)
			return
		}
		wr := info.Rect
		m.windowRect.Store(&wr)
		m.windowHidden.Store(!info.Visible)
		if info.Output != "" && info.Output != currentOutput {
			log.Printf("window moved to output %q (was %q)", info.Output, currentOutput)
			currentOutput = info.Output
			select {
			case switchOutput <- info.Output:
			default:
			}
		}
	}

	for {
		select {
		case <-m.ctx.Done():
			return
		case _, ok := <-watchCh:
			if !ok {
				watchCh = nil
				ticker.Reset(2 * time.Second)
				continue
			}
			refresh()
		case <-ticker.C:
			refresh()
		}
	}
}

func (m *overlayManager) runWayland(switchOutput <-chan string) error {
	targetOutput := ""

	if m.opts.WindowMatcher != nil {
		select {
		case <-m.ctx.Done():
			return m.ctx.Err()
		case target := <-switchOutput:
			targetOutput = target
		case <-time.After(3 * time.Second):
			log.Printf("no target window detected, starting overlay on default output")
		}
	}

	for {
		select {
		case <-m.ctx.Done():
			return m.ctx.Err()
		default:
		}

		s := &waylandState{
			mgr: m,
		}

		err := s.init(targetOutput)
		if err != nil {
			log.Printf("overlay init: %v", err)
			time.Sleep(time.Second)
			continue
		}

		newTarget := s.run(switchOutput)
		s.client.Close()

		if newTarget == "" {
			return nil
		}

		targetOutput = newTarget
		log.Printf("reinitializing overlay on output %q", targetOutput)
	}
}
