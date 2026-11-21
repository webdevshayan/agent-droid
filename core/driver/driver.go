// Package driver defines the runtime-adapter contract and ships the two
// built-in drivers: adb (real devices) and fake (an in-process simulated
// app that needs no device, no adb and no emulator).
//
// A driver is everything behind the CLI: launch/close an app, dump the
// view tree (uiautomator-style XML, parsed by the snapshot package), and
// input events. Third-party drivers only have to implement Driver — see
// docs/DRIVER_PROTOCOL.md.
package driver

import (
	"github.com/webdevshayan/agent-droid/core/device"
)

// Driver is the runtime-adapter contract. Implementations must be safe
// against malformed input (coordinate and text validation happen inside)
// and degrade to clear errors when their runtime is unavailable.
type Driver interface {
	// Launch installs/starts the APK (adb: `install -r`; fake: session start).
	Launch(apk string) error
	// Close ends the session (adb: HOME key — the app stays installed,
	// backgrounded; fake: terminates the simulated app).
	Close() error
	// Devices lists attached devices (adb only).
	Devices() (string, error)
	// Dump returns the current view tree as uiautomator-style XML.
	Dump() ([]byte, error)
	// Tap taps screen coordinates.
	Tap(x, y int) error
	// Type sends text to the focused element. The adb driver enforces the
	// shell-safe charset allowlist; the fake driver accepts Unicode.
	Type(text string) error
	// Swipe drags from (x1,y1) to (x2,y2) over durationMs.
	Swipe(x1, y1, x2, y2, durationMs int) error
	// Key sends a keycode (4 = BACK, 3 = HOME, 66 = ENTER, ...).
	Key(code int) error
	// Screenshot returns raw PNG bytes.
	Screenshot() ([]byte, error)
}

// ADB adapts a real adb device to the Driver contract.
type ADB struct {
	c *device.Client
}

// NewADB wraps a device client as a Driver.
func NewADB(c *device.Client) *ADB { return &ADB{c: c} }

func (a *ADB) Launch(apk string) error     { return a.c.Install(apk) }
func (a *ADB) Close() error                { return a.c.KeyHome() }
func (a *ADB) Devices() (string, error)    { return a.c.Devices() }
func (a *ADB) Dump() ([]byte, error)       { return a.c.Snapshot() }
func (a *ADB) Tap(x, y int) error          { return a.c.Tap(x, y) }
func (a *ADB) Type(text string) error      { return a.c.Type(text) }
func (a *ADB) Key(code int) error          { return a.c.Key(code) }
func (a *ADB) Screenshot() ([]byte, error) { return a.c.ScreenBytes() }

func (a *ADB) Swipe(x1, y1, x2, y2, durationMs int) error {
	return a.c.Swipe(x1, y1, x2, y2, durationMs)
}
