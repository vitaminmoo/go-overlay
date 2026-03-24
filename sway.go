package overlay

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"os"
	"os/exec"
	"strings"
	"time"
)

// SwayMatcher finds windows using sway/i3 IPC.
type SwayMatcher struct {
	// Match returns true for window names that should be tracked.
	Match func(windowName string) bool

	ipc       *swayIPC
	trackedID int
}

// NewSwayMatcher creates a WindowMatcher that uses sway IPC to find windows.
// The match function is called with each window's name; return true for the
// target window.
func NewSwayMatcher(match func(string) bool) *SwayMatcher {
	return &SwayMatcher{
		Match: match,
		ipc:   newSwayIPC(),
	}
}

// FindWindow implements WindowMatcher.
func (sm *SwayMatcher) FindWindow() (WindowInfo, bool) {
	root, err := getSwayTree(sm.ipc)
	if err != nil {
		return WindowInfo{}, false
	}

	var candidates []*swayNode
	var findWindows func(node *swayNode)
	findWindows = func(node *swayNode) {
		if sm.Match(node.Name) {
			candidates = append(candidates, node)
		}
		for i := range node.Nodes {
			findWindows(&node.Nodes[i])
		}
		for i := range node.Floating {
			findWindows(&node.Floating[i])
		}
	}
	findWindows(root)

	if len(candidates) == 0 {
		return WindowInfo{}, false
	}

	var winNode *swayNode
	if sm.trackedID != 0 {
		for _, c := range candidates {
			if c.ID == sm.trackedID {
				winNode = c
				break
			}
		}
	}
	if winNode == nil {
		winNode = candidates[0]
		if sm.trackedID != winNode.ID {
			log.Printf("sway: locking onto window %q id=%d pid=%d (%d candidates)",
				winNode.Name, winNode.ID, winNode.PID, len(candidates))
		}
		sm.trackedID = winNode.ID
	}

	contentX := winNode.Rect.X + winNode.WindowRect.X
	contentY := winNode.Rect.Y + winNode.WindowRect.Y
	contentW := winNode.WindowRect.Width
	contentH := winNode.WindowRect.Height

	cx := contentX + contentW/2
	cy := contentY + contentH/2
	for _, output := range root.Nodes {
		if output.Type != "output" || strings.HasPrefix(output.Name, "__") {
			continue
		}
		oR := output.Rect
		if cx >= oR.X && cx < oR.X+oR.Width &&
			cy >= oR.Y && cy < oR.Y+oR.Height {
			relX := contentX - oR.X
			relY := contentY - oR.Y
			return WindowInfo{
				Rect: WindowRect{
					X: relX, Y: relY, W: contentW, H: contentH,
				},
				Output:  output.Name,
				Visible: winNode.Visible,
			}, true
		}
	}
	return WindowInfo{}, false
}

// WatchEvents implements WindowWatcher.
func (sm *SwayMatcher) WatchEvents(ctx context.Context) <-chan struct{} {
	if sm.ipc == nil {
		return nil
	}
	return sm.ipc.watchWindowEvents(ctx)
}

// --- sway IPC internals ---

type swayIPC struct {
	socketPath string
}

const (
	swayIPCMagic     = "i3-ipc"
	swayMsgSubscribe = 2
	swayMsgGetTree   = 4
	swayHeaderSize   = 14 // 6 magic + 4 length + 4 type
	swayEventWindow  = 0x80000003
)

func newSwayIPC() *swayIPC {
	path := os.Getenv("SWAYSOCK")
	if path == "" {
		return nil
	}
	return &swayIPC{socketPath: path}
}

func (s *swayIPC) getTree() ([]byte, error) {
	conn, err := net.Dial("unix", s.socketPath)
	if err != nil {
		return nil, fmt.Errorf("dial %s: %w", s.socketPath, err)
	}
	defer conn.Close()

	var header [swayHeaderSize]byte
	copy(header[:6], swayIPCMagic)
	binary.LittleEndian.PutUint32(header[6:10], 0)
	binary.LittleEndian.PutUint32(header[10:14], swayMsgGetTree)

	if _, err := conn.Write(header[:]); err != nil {
		return nil, fmt.Errorf("write request: %w", err)
	}

	var respHeader [swayHeaderSize]byte
	if _, err := ipcReadFull(conn, respHeader[:]); err != nil {
		return nil, fmt.Errorf("read response header: %w", err)
	}
	if string(respHeader[:6]) != swayIPCMagic {
		return nil, fmt.Errorf("invalid magic: %q", respHeader[:6])
	}

	payloadLen := binary.LittleEndian.Uint32(respHeader[6:10])
	if payloadLen > 16<<20 {
		return nil, fmt.Errorf("response too large: %d bytes", payloadLen)
	}

	payload := make([]byte, payloadLen)
	if _, err := ipcReadFull(conn, payload); err != nil {
		return nil, fmt.Errorf("read payload: %w", err)
	}
	return payload, nil
}

func ipcSendMsg(conn net.Conn, msgType uint32, payload []byte) (uint32, []byte, error) {
	var header [swayHeaderSize]byte
	copy(header[:6], swayIPCMagic)
	binary.LittleEndian.PutUint32(header[6:10], uint32(len(payload)))
	binary.LittleEndian.PutUint32(header[10:14], msgType)

	if _, err := conn.Write(header[:]); err != nil {
		return 0, nil, fmt.Errorf("write: %w", err)
	}
	if len(payload) > 0 {
		if _, err := conn.Write(payload); err != nil {
			return 0, nil, fmt.Errorf("write payload: %w", err)
		}
	}

	var resp [swayHeaderSize]byte
	if _, err := ipcReadFull(conn, resp[:]); err != nil {
		return 0, nil, fmt.Errorf("read header: %w", err)
	}
	if string(resp[:6]) != swayIPCMagic {
		return 0, nil, fmt.Errorf("invalid magic: %q", resp[:6])
	}
	respLen := binary.LittleEndian.Uint32(resp[6:10])
	respType := binary.LittleEndian.Uint32(resp[10:14])
	if respLen > 16<<20 {
		return 0, nil, fmt.Errorf("response too large: %d", respLen)
	}
	body := make([]byte, respLen)
	if _, err := ipcReadFull(conn, body); err != nil {
		return 0, nil, fmt.Errorf("read body: %w", err)
	}
	return respType, body, nil
}

func (s *swayIPC) watchWindowEvents(ctx context.Context) <-chan struct{} {
	ch := make(chan struct{}, 1)
	go func() {
		defer close(ch)
		for {
			if ctx.Err() != nil {
				return
			}
			err := s.subscribeLoop(ctx, ch)
			if ctx.Err() != nil {
				return
			}
			if err != nil {
				log.Printf("sway event subscription error: %v; reconnecting in 2s", err)
			}
			select {
			case <-ctx.Done():
				return
			case <-time.After(2 * time.Second):
			}
		}
	}()
	return ch
}

func (s *swayIPC) subscribeLoop(ctx context.Context, ch chan<- struct{}) error {
	conn, err := net.Dial("unix", s.socketPath)
	if err != nil {
		return fmt.Errorf("dial: %w", err)
	}
	defer conn.Close()

	_, body, err := ipcSendMsg(conn, swayMsgSubscribe, []byte(`["window"]`))
	if err != nil {
		return fmt.Errorf("subscribe: %w", err)
	}
	var subReply struct{ Success bool }
	if err := json.Unmarshal(body, &subReply); err != nil || !subReply.Success {
		return fmt.Errorf("subscribe failed: %s", body)
	}
	log.Printf("sway: subscribed to window events")

	for {
		if ctx.Err() != nil {
			return nil
		}

		conn.SetReadDeadline(time.Now().Add(5 * time.Second))

		var header [swayHeaderSize]byte
		if _, err := ipcReadFull(conn, header[:]); err != nil {
			if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				continue
			}
			return fmt.Errorf("read event header: %w", err)
		}
		if string(header[:6]) != swayIPCMagic {
			return fmt.Errorf("invalid magic: %q", header[:6])
		}
		payloadLen := binary.LittleEndian.Uint32(header[6:10])
		if payloadLen > 16<<20 {
			return fmt.Errorf("event too large: %d", payloadLen)
		}

		payload := make([]byte, payloadLen)
		if _, err := ipcReadFull(conn, payload); err != nil {
			return fmt.Errorf("read event payload: %w", err)
		}

		evType := binary.LittleEndian.Uint32(header[10:14])
		if evType == swayEventWindow {
			select {
			case ch <- struct{}{}:
			default:
			}
		}
	}
}

func ipcReadFull(conn net.Conn, buf []byte) (int, error) {
	total := 0
	for total < len(buf) {
		n, err := conn.Read(buf[total:])
		total += n
		if err != nil {
			return total, err
		}
	}
	return total, nil
}

// --- sway tree types ---

type swayNode struct {
	ID         int        `json:"id"`
	PID        int        `json:"pid"`
	Name       string     `json:"name"`
	Type       string     `json:"type"`
	Visible    bool       `json:"visible"`
	Focused    bool       `json:"focused"`
	Rect       swayRect   `json:"rect"`
	WindowRect swayRect   `json:"window_rect"`
	Nodes      []swayNode `json:"nodes"`
	Floating   []swayNode `json:"floating_nodes"`
}

type swayRect struct {
	X      int `json:"x"`
	Y      int `json:"y"`
	Width  int `json:"width"`
	Height int `json:"height"`
}

func execSwayMsg() ([]byte, error) {
	return exec.Command("swaymsg", "-t", "get_tree").Output()
}

func getSwayTree(ipc *swayIPC) (*swayNode, error) {
	var data []byte
	var err error
	if ipc != nil {
		data, err = ipc.getTree()
	} else {
		data, err = execSwayMsg()
	}
	if err != nil {
		return nil, err
	}

	var root swayNode
	if err := json.Unmarshal(data, &root); err != nil {
		return nil, fmt.Errorf("unmarshal tree: %w", err)
	}
	return &root, nil
}
