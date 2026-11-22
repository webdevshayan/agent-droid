package driver

import (
	"errors"
	"fmt"
	"time"

	"github.com/webdevshayan/agent-droid/core/internal/protocol"
	"github.com/webdevshayan/agent-droid/core/internal/runtime"
	"github.com/webdevshayan/agent-droid/core/internal/uitree"
)

// RT is the Driver view of one live runtime session. It adapts the session
// protocol (the fake/ATL/WineDroid backends served by `agentdroid serve` on
// an abstract Unix socket) onto the same driver.Driver contract as adb, so
// dispatch, snapshot refs and the agent loop behave identically for every
// runtime.
//
// Zero storage by construction: the session lives in the daemon's memory
// and its kernel-only socket; RT itself holds nothing but the connection
// and the last tap target.
type RT struct {
	c       *runtime.Client
	timeout time.Duration
	lastX   int
	lastY   int
	hasLast bool
}

// NewRT wraps a connected session client as a Driver.
func NewRT(c *runtime.Client) *RT { return &RT{c: c, timeout: 15 * time.Second} }

// respErr maps a failed protocol response onto the Driver error convention.
func respErr(resp *protocol.Response, op string) error {
	if resp.Error != "" {
		return fmt.Errorf("%s: %s", op, resp.Error)
	}
	return nil
}

// Launch hands the APK to the session's backend (already spawned by the
// daemon, so this is start, not install).
func (r *RT) Launch(apk string) error {
	resp, err := r.c.Call(protocol.Request{Cmd: protocol.CmdLaunch, APK: apk}, r.timeout)
	if err != nil {
		return err
	}
	return respErr(resp, "launch")
}

// Close ends the session; the daemon shuts its runtime down and exits.
func (r *RT) Close() error {
	resp, err := r.c.Call(protocol.Request{Cmd: protocol.CmdClose}, r.timeout)
	cerr := r.c.Close()
	if err != nil {
		return err
	}
	if cerr != nil {
		return fmt.Errorf("close: connection: %w", cerr)
	}
	return respErr(resp, "close")
}

// Devices is an adb concept; runtime sessions have no device list.
func (r *RT) Devices() (string, error) {
	return "", errors.New("devices applies to the adb runtime only")
}

// Dump snapshots the session and renders the view tree as uiautomator-style
// XML — the byte shape the adb driver returns — so core/snapshot stays the
// single ref-assignment path for every runtime.
func (r *RT) Dump() ([]byte, error) {
	resp, err := r.c.Call(protocol.Request{Cmd: protocol.CmdSnapshot}, r.timeout)
	if err != nil {
		return nil, err
	}
	if resp.Error != "" {
		return nil, fmt.Errorf("snapshot: %s", resp.Error)
	}
	if resp.Tree == nil {
		return nil, errors.New("snapshot: adapter returned no view tree")
	}
	return uitree.XML(resp.Tree)
}

// Tap taps screen coordinates and remembers the point: within one process,
// the last tap decides where Type sends text (across processes the daemon
// keeps the focus, exactly like a real device would).
func (r *RT) Tap(x, y int) error {
	if err := checkCoord(x); err != nil {
		return err
	}
	if err := checkCoord(y); err != nil {
		return err
	}
	resp, err := r.c.Call(protocol.Request{Cmd: protocol.CmdTap, X: x, Y: y}, r.timeout)
	if err != nil {
		return err
	}
	if err := respErr(resp, "tap"); err != nil {
		return err
	}
	r.lastX, r.lastY, r.hasLast = x, y, true
	return nil
}

// Type sends text to the focused element. With no prior tap in this process
// it asks the daemon to type into whatever field the daemon has focused —
// the coordinate-less form of the session protocol.
func (r *RT) Type(text string) error {
	if text == "" {
		return errors.New("type: text is empty")
	}
	x, y := -1, -1
	if r.hasLast {
		x, y = r.lastX, r.lastY
	}
	resp, err := r.c.Call(protocol.Request{Cmd: protocol.CmdType, X: x, Y: y, Text: text}, r.timeout)
	if err != nil {
		return err
	}
	return respErr(resp, "type")
}

// Swipe is not part of the session protocol yet; it degrades to a clear
// error instead of pretending (the parser rejects it at exit code 2, this
// guards direct Driver users).
func (r *RT) Swipe(x1, y1, x2, y2, durationMs int) error {
	return errors.New("swipe is not supported by runtime sessions yet")
}

// Key is not part of the session protocol yet; see Swipe.
func (r *RT) Key(code int) error {
	return errors.New("key is not supported by runtime sessions yet")
}

// Screenshot returns the session's PNG frame.
func (r *RT) Screenshot() ([]byte, error) {
	resp, err := r.c.Call(protocol.Request{Cmd: protocol.CmdScreenshot}, r.timeout)
	if err != nil {
		return nil, err
	}
	if resp.Error != "" {
		return nil, fmt.Errorf("screenshot: %s", resp.Error)
	}
	if len(resp.PNG) == 0 {
		return nil, errors.New("screenshot: adapter returned no image data")
	}
	return resp.PNG, nil
}
