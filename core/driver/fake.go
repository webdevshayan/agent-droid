// Package driver — fake runtime: an in-process simulated app that makes
// the whole agent loop runnable with no device, no adb and no emulator.
//
// The fake app is a small three-screen state machine ("main", "welcome",
// "about"). State persists across CLI invocations via a JSON file, because
// each agentdroid command is its own process: launch writes the session,
// every later command loads it, close removes it.
package driver

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

const (
	keyBack    = 4
	keyHome    = 3
	keyEnter   = 66
	keyMoveEnd = 123 // KEYCODE_MOVE_END
	keyDel     = 67  // KEYCODE_DEL (backspace: deletes the rune before the cursor)

	screenMain    = 0
	screenWelcome = 1
	screenAbout   = 2
)

// fakeState is what persists between invocations. Cursor is the insertion
// position inside the focused field, in runes, so DEL after MOVE_END deletes
// the typed text exactly like a real IME would.
type fakeState struct {
	Running  bool   `json:"running"`
	App      string `json:"app"`
	Screen   int    `json:"screen"`
	Username string `json:"username"`
	Focused  string `json:"focused"`
	Cursor   int    `json:"cursor"`
}

// Fake is the simulated-app driver.
type Fake struct {
	state fakeState
	lock  *stateLock
}

// fakeStatePath comes from AGENTDROID_FAKE_STATE (tests and CI use it) and
// defaults to a file under the user's cache directory. The old default — a
// fixed, world-known name in the OS temp dir — was attacker-chosen real
// estate on multi-user machines (another user could pre-create it as a
// symlink); the cache dir is user-scoped and 0700.
func fakeStatePath() string {
	if p := os.Getenv("AGENTDROID_FAKE_STATE"); p != "" {
		return p
	}
	dir, err := os.UserCacheDir()
	if err != nil || dir == "" {
		dir = os.TempDir() // headless containers without a HOME
	}
	return filepath.Join(dir, "agentdroid", "fake-state.json")
}

// ensureStateDir creates the state file's parent directory when missing.
func ensureStateDir(p string) error {
	dir := filepath.Dir(p)
	if dir == "" || dir == "." {
		return nil
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	return nil
}

// stateLock guards read-modify-write on the fake state file across
// processes: concurrent agentdroid invocations used to last-write-win, so
// typed text could silently vanish when an agent parallelized commands.
// A sidecar .lock file — a stable inode, unlike the state file that Close
// removes — is flock-ed exclusively for the Fake's lifetime; the kernel
// releases it when the process exits. Locks are reference-counted per
// process so several Fakes on one path (tests) do not deadlock.
type stateLock struct {
	path string
	f    *os.File
	refs int
}

var stateLocks = struct {
	mu sync.Mutex
	m  map[string]*stateLock
}{m: map[string]*stateLock{}}

// acquireStateLock flocks path+".lock" exclusively. Locking is best-effort:
// on failure the fake proceeds unlocked rather than breaking the command —
// the honest error would otherwise block single-user machines for nothing.
func acquireStateLock(statePath string) *stateLock {
	p := statePath + ".lock"
	if err := ensureStateDir(p); err != nil {
		return nil
	}
	stateLocks.mu.Lock()
	defer stateLocks.mu.Unlock()
	if l, ok := stateLocks.m[p]; ok {
		l.refs++
		return l
	}
	f, err := os.OpenFile(p, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil
	}
	if err := flockExclusive(f); err != nil {
		f.Close()
		return nil
	}
	l := &stateLock{path: p, f: f, refs: 1}
	stateLocks.m[p] = l
	return l
}

func (l *stateLock) release() {
	if l == nil {
		return
	}
	stateLocks.mu.Lock()
	defer stateLocks.mu.Unlock()
	l.refs--
	if l.refs > 0 {
		return
	}
	funlock(l.f)
	l.f.Close()
	delete(stateLocks.m, l.path)
}

// OpenFake loads the persisted fake session (or starts blank) and takes the
// state lock for this process's lifetime.
func OpenFake() *Fake {
	p := fakeStatePath()
	var f Fake
	f.lock = acquireStateLock(p)
	if data, err := os.ReadFile(p); err == nil {
		_ = json.Unmarshal(data, &f.state)
	}
	return &f
}

// save persists the session. A write failure is surfaced, never swallowed:
// the command that triggered it must fail (exit 1) so an agent learns the
// state did not persist, instead of every later command failing with
// "no fake app running".
func (f *Fake) save() error {
	data, err := json.Marshal(f.state)
	if err != nil {
		return fmt.Errorf("fake state encode: %w", err)
	}
	p := fakeStatePath()
	if err := ensureStateDir(p); err != nil {
		return fmt.Errorf("fake state write failed (%s): %w", p, err)
	}
	if err := os.WriteFile(p, data, 0o600); err != nil {
		return fmt.Errorf("fake state write failed (%s): %w", p, err)
	}
	return nil
}

// Launch starts the simulated app. The path must exist and end in .apk —
// the same accessibility rule the session runtimes enforce (AGD-07).
func (f *Fake) Launch(apk string) error {
	if !strings.EqualFold(filepath.Ext(apk), ".apk") {
		return fmt.Errorf("launch: %q does not look like an APK (.apk)", apk)
	}
	if _, err := os.Stat(apk); err != nil {
		return fmt.Errorf("launch: apk not accessible: %w", err)
	}
	prev := f.state
	f.state = fakeState{Running: true, App: apk, Screen: screenMain, Focused: ""}
	if err := f.save(); err != nil {
		f.state = prev // nothing persisted, nothing changed
		return fmt.Errorf("launch: %w", err)
	}
	return nil
}

// Close terminates the simulated app, clears the session file and releases
// the state lock.
func (f *Fake) Close() error {
	if err := f.running(); err != nil {
		return err
	}
	f.state.Running = false
	if err := f.save(); err != nil {
		f.state.Running = true
		return fmt.Errorf("close: %w", err)
	}
	if err := os.Remove(fakeStatePath()); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("close: %w", err)
	}
	f.lock.release()
	f.lock = nil
	return nil
}

// Devices is meaningless under the fake runtime.
func (f *Fake) Devices() (string, error) {
	return "", errors.New("devices applies to the adb runtime only")
}

// running reports a clean error when there is no session — never a panic.
func (f *Fake) running() error {
	if !f.state.Running {
		return errors.New("no fake app running; run `agentdroid --runtime fake launch demo.apk` first")
	}
	return nil
}

// element is one fake UI node with fixed bounds for hit-testing.
type element struct {
	class     string
	text      string
	id        string
	rect      [4]int
	focusable bool
}

func (f *Fake) elements() []element {
	switch f.state.Screen {
	case screenWelcome:
		return []element{
			{class: "android.widget.TextView", text: "Welcome, " + f.state.Username, id: "demo:id/welcome", rect: [4]int{0, 0, 1080, 140}},
			{class: "android.widget.Button", text: "Back", id: "demo:id/back", rect: [4]int{340, 400, 740, 520}},
		}
	case screenAbout:
		return []element{
			{class: "android.widget.TextView", text: "AgentDroid demo v0.1.0", id: "demo:id/about_text", rect: [4]int{0, 0, 1080, 140}},
			{class: "android.widget.Button", text: "Back", id: "demo:id/back", rect: [4]int{340, 400, 740, 520}},
		}
	default: // main
		return []element{
			{class: "android.widget.TextView", text: "Demo App", id: "demo:id/title", rect: [4]int{0, 0, 1080, 140}},
			{class: "android.widget.EditText", text: f.state.Username, id: "demo:id/username", rect: [4]int{54, 200, 1026, 320}, focusable: true},
			{class: "android.widget.Button", text: "Sign in", id: "demo:id/signin", rect: [4]int{340, 400, 740, 520}},
			{class: "android.widget.Button", text: "About", id: "demo:id/about", rect: [4]int{340, 560, 740, 680}},
		}
	}
}

// Dump renders the current screen as uiautomator-style XML — the same
// format the snapshot parser consumes, so the agent loop is identical for
// both drivers.
func (f *Fake) Dump() ([]byte, error) {
	if err := f.running(); err != nil {
		return nil, err
	}
	var b strings.Builder
	b.WriteString("<?xml version='1.0' encoding='UTF-8' ?>\n<hierarchy rotation=\"0\">\n")
	for _, el := range f.elements() {
		clickable := "false"
		if el.class == "android.widget.Button" || el.focusable {
			clickable = "true"
		}
		fmt.Fprintf(&b,
			"  <node text=\"%s\" resource-id=%q class=%q content-desc=\"\" clickable=%q enabled=\"true\" scrollable=\"false\" bounds=\"[%d,%d][%d,%d]\"/>\n",
			xmlEscape(el.text), el.id, el.class, clickable,
			el.rect[0], el.rect[1], el.rect[2], el.rect[3])
	}
	b.WriteString("</hierarchy>")
	return []byte(b.String()), nil
}

// xmlEscape escapes the five XML specials and renders control characters as
// numeric character references. The numeric form matters: a literal newline
// or tab inside an attribute value would be normalized to a space by the XML
// parser, but character references survive parsing intact — so typed text
// round-trips byte-for-byte, control characters included.
func xmlEscape(s string) string {
	var b bytes.Buffer
	for _, r := range s {
		switch r {
		case '&':
			b.WriteString("&amp;")
		case '<':
			b.WriteString("&lt;")
		case '>':
			b.WriteString("&gt;")
		case '"':
			b.WriteString("&quot;")
		case '\'':
			b.WriteString("&apos;")
		default:
			if r < 0x20 || r == 0x7f {
				fmt.Fprintf(&b, "&#%d;", r)
			} else {
				b.WriteRune(r)
			}
		}
	}
	return b.String()
}

// Tap hit-tests the point and applies the screen's action.
func (f *Fake) Tap(x, y int) error {
	if err := checkCoord(x); err != nil {
		return err
	}
	if err := checkCoord(y); err != nil {
		return err
	}
	if err := f.running(); err != nil {
		return err
	}
	for _, el := range f.elements() {
		if x < el.rect[0] || x > el.rect[2] || y < el.rect[1] || y > el.rect[3] {
			continue
		}
		switch el.id {
		case "demo:id/username":
			f.state.Focused = "username"
			f.state.Cursor = len([]rune(f.state.Username))
		case "demo:id/signin":
			f.state.Screen = screenWelcome
			f.state.Focused = ""
			f.state.Cursor = 0
		case "demo:id/about":
			f.state.Screen = screenAbout
			f.state.Focused = ""
			f.state.Cursor = 0
		case "demo:id/back":
			f.state.Screen = screenMain
			f.state.Focused = ""
			f.state.Cursor = 0
		}
		if err := f.save(); err != nil {
			return fmt.Errorf("tap: %w", err)
		}
		return nil
	}
	return nil // tap on empty screen area: a no-op, like a real tap
}

// Type appends text to the focused field.
func (f *Fake) Type(text string) error {
	if len(text) == 0 {
		return errors.New("type: text is empty")
	}
	if len(text) > 4096 {
		return fmt.Errorf("type: text is %d bytes, over the 4096-byte limit", len(text))
	}
	if err := f.running(); err != nil {
		return err
	}
	if f.state.Focused != "username" {
		return errors.New("type: no focused text field; tap the field first")
	}
	runes := []rune(f.state.Username)
	at := f.state.Cursor
	if at < 0 || at > len(runes) {
		at = len(runes) // tolerate state files written by older versions
	}
	f.state.Username = string(runes[:at]) + text + string(runes[at:])
	f.state.Cursor = at + len([]rune(text))
	if err := f.save(); err != nil {
		return fmt.Errorf("type: %w", err)
	}
	return nil
}

// Swipe is a no-op on the fake screens (they do not scroll). The CLI prints
// an explicit note when the fake ignores a swipe, so the agent sees the
// honest result instead of inferring one from a bare success line.
func (f *Fake) Swipe(x1, y1, x2, y2, durationMs int) error {
	for _, n := range []int{x1, y1, x2, y2} {
		if err := checkCoord(n); err != nil {
			return err
		}
	}
	if err := f.running(); err != nil {
		return err
	}
	return nil
}

// Key handles BACK, HOME, ENTER, MOVE_END and DEL like a real IME: DEL
// deletes the rune before the cursor, MOVE_END parks the cursor after the
// last rune — together they are what `clear` uses to empty a field. Unknown
// keycodes are accepted no-ops, exactly like a real device.
func (f *Fake) Key(code int) error {
	if err := f.running(); err != nil {
		return err
	}
	runes := []rune(f.state.Username)
	switch code {
	case keyBack, keyHome:
		f.state.Screen = screenMain
		f.state.Focused = ""
		f.state.Cursor = 0
	case keyEnter:
		if f.state.Screen == screenMain && f.state.Focused == "username" {
			f.state.Screen = screenWelcome
			f.state.Focused = ""
			f.state.Cursor = 0
		}
	case keyMoveEnd:
		if f.state.Focused == "username" {
			f.state.Cursor = len(runes)
		}
	case keyDel:
		if f.state.Focused == "username" && f.state.Cursor > 0 && f.state.Cursor <= len(runes) {
			f.state.Username = string(runes[:f.state.Cursor-1]) + string(runes[f.state.Cursor:])
			f.state.Cursor--
		}
	}
	if err := f.save(); err != nil {
		return fmt.Errorf("key %d: %w", code, err)
	}
	return nil
}

// Screenshot renders a small deterministic PNG (color-coded per screen).
func (f *Fake) Screenshot() ([]byte, error) {
	if err := f.running(); err != nil {
		return nil, err
	}
	var base color.RGBA
	switch f.state.Screen {
	case screenWelcome:
		base = color.RGBA{0x23, 0x86, 0x36, 0xff}
	case screenAbout:
		base = color.RGBA{0x82, 0x50, 0xdf, 0xff}
	default:
		base = color.RGBA{0x1f, 0x6f, 0xeb, 0xff}
	}
	img := image.NewRGBA(image.Rect(0, 0, 1080, 2400))
	// Uniform fills via image/draw — the per-pixel img.Set version did the
	// same work about an order of magnitude slower.
	draw.Draw(img, img.Bounds(), &image.Uniform{base}, image.Point{}, draw.Src)
	draw.Draw(img, image.Rect(0, 0, 1080, 140), &image.Uniform{color.RGBA{0xf0, 0xf6, 0xfc, 0xff}}, image.Point{}, draw.Src)
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, fmt.Errorf("screenshot: %w", err)
	}
	return buf.Bytes(), nil
}

func checkCoord(n int) error {
	if n < 0 || n > 100000 {
		return fmt.Errorf("coordinate %d out of range 0..100000", n)
	}
	return nil
}
