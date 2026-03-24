package overlay

import (
	"errors"
	"fmt"
	"log"
	"time"

	wl "deedles.dev/wl/client"
	"deedles.dev/ximage/format"

	"github.com/vitaminmoo/go-overlay/internal/layershell"
)

type waylandState struct {
	mgr *overlayManager

	client     *wl.Client
	display    *wl.Display
	registry   *wl.Registry
	shm        *wl.Shm
	compositor *wl.Compositor
	layerShell *layershell.LayerShellV1

	surface      *wl.Surface
	layerSurface *layershell.LayerSurfaceV1
	buffers      [2]*wl.ImageBuffer
	bufIdx       int

	outputs       map[string]*wl.Output
	currentOutput string

	width        int32
	height       int32
	framePending bool
}

func (s *waylandState) init(targetOutput string) error {
	client, err := wl.Dial()
	if err != nil {
		return fmt.Errorf("dial display: %w", err)
	}
	s.client = client
	s.outputs = make(map[string]*wl.Output)

	s.display = client.Display()
	s.display.Listener = (*wlDisplayListener)(s)

	s.registry = s.display.GetRegistry()
	s.registry.Listener = (*wlRegistryListener)(s)

	if err := s.client.RoundTrip(); err != nil {
		return fmt.Errorf("round trip: %w", err)
	}
	if err := s.client.RoundTrip(); err != nil {
		return fmt.Errorf("round trip 2: %w", err)
	}

	if s.compositor == nil {
		return errors.New("no compositor found")
	}
	if s.shm == nil {
		return errors.New("no shm found")
	}
	if s.layerShell == nil {
		return errors.New("no layer shell found")
	}

	var output *wl.Output
	if targetOutput != "" {
		if o, ok := s.outputs[targetOutput]; ok {
			output = o
			s.currentOutput = targetOutput
			log.Printf("overlay on output %q", targetOutput)
		}
	}

	s.surface = s.compositor.CreateSurface()

	s.layerSurface = s.layerShell.GetLayerSurface(
		s.surface,
		output,
		layershell.LayerShellV1LayerOverlay,
		s.mgr.opts.Namespace,
	)
	s.layerSurface.Listener = (*wlLayerSurfaceListener)(s)
	s.layerSurface.SetAnchor(
		layershell.LayerSurfaceV1AnchorTop |
			layershell.LayerSurfaceV1AnchorBottom |
			layershell.LayerSurfaceV1AnchorLeft |
			layershell.LayerSurfaceV1AnchorRight,
	)
	s.layerSurface.SetSize(0, 0)
	s.layerSurface.SetExclusiveZone(-1)
	s.layerSurface.SetKeyboardInteractivity(layershell.LayerSurfaceV1KeyboardInteractivityNone)

	// Empty input region for full click-through
	region := s.compositor.CreateRegion()
	s.surface.SetInputRegion(region)
	region.Destroy()

	s.surface.Commit()

	for i := range 2 {
		buf, err := wl.NewImageBuffer(s.shm, 1, 1)
		if err != nil {
			return fmt.Errorf("create buffer %d: %w", i, err)
		}
		s.buffers[i] = buf
	}

	return nil
}

func (s *waylandState) run(switchOutput <-chan string) string {
	if s.width > 0 && s.height > 0 {
		s.render()
	}

	for {
		select {
		case <-s.mgr.ctx.Done():
			return ""
		case target := <-switchOutput:
			return target
		case ev, ok := <-s.client.Events():
			if !ok {
				return ""
			}
			if err := ev(); err != nil {
				log.Printf("wayland event error: %v", err)
			}
		}
	}
}

func (s *waylandState) render() {
	if s.framePending {
		return
	}
	s.framePending = true

	back := s.buffers[s.bufIdx]
	back.Resize(s.width, s.height)
	img := back.Image()
	fimg := img.(*format.Image)
	clear(fimg.Pix)

	if !s.mgr.windowHidden.Load() || s.mgr.opts.WindowMatcher == nil {
		c := s.mgr.canvas
		c.reset(newPixBufFromImage(fimg), int(s.width), int(s.height))

		if wr := s.mgr.windowRect.Load(); wr != nil {
			c.windowRect = wr
		}

		if s.mgr.qualityTracker != nil {
			c.quality = s.mgr.qualityTracker.level
		}

		start := time.Now()
		s.mgr.scene.Draw(c)
		drawDur := time.Since(start)

		if s.mgr.qualityTracker != nil {
			s.mgr.qualityTracker.recordFrame(drawDur)
		}
	}

	s.surface.Attach(back.Buffer(), 0, 0)
	s.surface.DamageBuffer(0, 0, s.width, s.height)

	cb := s.surface.Frame()
	cb.Then(func(_ uint32) {
		s.framePending = false
		if s.width > 0 && s.height > 0 {
			s.render()
		}
	})

	s.surface.Commit()
	s.bufIdx ^= 1
}

// --- Wayland listener implementations ---

type wlDisplayListener waylandState

func (s *wlDisplayListener) Error(id, code uint32, msg string) {
	log.Fatalf("display error: id: %v, code: %v, msg: %q", id, code, msg)
}

func (s *wlDisplayListener) DeleteId(id uint32) {
	s.client.Delete(id)
}

type wlRegistryListener waylandState

func (s *wlRegistryListener) Global(name uint32, inter string, version uint32) {
	switch inter {
	case wl.CompositorInterface:
		s.compositor = wl.BindCompositor(s.client, s.registry, name, version)
	case wl.ShmInterface:
		s.shm = wl.BindShm(s.client, s.registry, name, version)
	case layershell.LayerShellV1Interface:
		s.layerShell = layershell.BindLayerShellV1(s.client, s.registry, name, version)
	case wl.OutputInterface:
		output := wl.BindOutput(s.client, s.registry, name, version)
		output.Listener = &wlOutputNameListener{state: (*waylandState)(s), output: output}
	}
}

func (s *wlRegistryListener) GlobalRemove(name uint32) {}

type wlOutputNameListener struct {
	state  *waylandState
	output *wl.Output
}

func (l *wlOutputNameListener) Geometry(x, y, physicalWidth, physicalHeight int32, subpixel wl.OutputSubpixel, make, model string, transform wl.OutputTransform) {
}
func (l *wlOutputNameListener) Mode(flags wl.OutputMode, width, height, refresh int32) {}
func (l *wlOutputNameListener) Done()                                                  {}
func (l *wlOutputNameListener) Scale(factor int32)                                     {}
func (l *wlOutputNameListener) Description(description string)                         {}

func (l *wlOutputNameListener) Name(name string) {
	l.state.outputs[name] = l.output
	log.Printf("wayland output registered: %q", name)
}

type wlLayerSurfaceListener waylandState

func (s *wlLayerSurfaceListener) Configure(serial uint32, width uint32, height uint32) {
	s.layerSurface.AckConfigure(serial)
	ws := (*waylandState)(s)
	ws.width = int32(width)
	ws.height = int32(height)
	ws.render()
}

func (s *wlLayerSurfaceListener) Closed() {
	s.mgr.cancel()
}
