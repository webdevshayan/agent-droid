package runtime

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"os"
	"sync"

	"github.com/webdevshayan/agent-droid/core/internal/protocol"
	"github.com/webdevshayan/agent-droid/core/internal/uitree"
)

// FakeBackend simulates a small Android login app so the full orchestrator
// flow — launch, snapshot, tap, type, screenshot, close — works with no
// device, no emulator and no real runtime installed. It renders a genuine
// view tree and a genuine PNG frame, which makes it useful for demos,
// development and the integration tests.
type FakeBackend struct {
	mu       sync.Mutex
	apk      string
	email    string
	password string
	status   string
	signedIn bool
	focused  string // "", "email" or "password"
	started  bool
}

// NewFakeBackend builds the simulated login app. The APK is accepted at
// construction and again at launch, mirroring the real backends.
func NewFakeBackend(apk string) *FakeBackend {
	return &FakeBackend{apk: apk, status: "Ready."}
}

// Layout of the simulated 540x960 login screen, shared by the view tree,
// hit-testing and the framebuffer renderer.
var (
	frameBounds    = uitree.Bounds{0, 0, 540, 960}
	titleBounds    = uitree.Bounds{20, 40, 500, 48}
	emailBounds    = uitree.Bounds{20, 150, 500, 56}
	passwordBounds = uitree.Bounds{20, 220, 500, 56}
	statusBounds   = uitree.Bounds{20, 340, 500, 40}
	buttonBounds   = uitree.Bounds{20, 420, 500, 56}
)

func inside(x, y int, b uitree.Bounds) bool {
	return x >= b[0] && x < b[0]+b[2] && y >= b[1] && y < b[1]+b[3]
}

func okResp(pid int) *protocol.Response {
	return &protocol.Response{OK: true, PID: pid}
}

func errResp(format string, a ...any) *protocol.Response {
	return &protocol.Response{Error: fmt.Sprintf(format, a...)}
}

func (f *FakeBackend) Launch(apk string) *protocol.Response {
	if apk == "" {
		apk = f.apk
	}
	if apk == "" {
		return errResp("launch: no apk given")
	}
	if _, err := os.Stat(apk); err != nil {
		return errResp("launch: apk not readable: %v", err)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.apk = apk
	f.started = true
	return okResp(os.Getpid())
}

func (f *FakeBackend) Snapshot() *protocol.Response {
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.started {
		return errResp("no app launched in this session")
	}
	return &protocol.Response{OK: true, Tree: f.tree()}
}

func (f *FakeBackend) Tap(x, y int) *protocol.Response {
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.started {
		return errResp("no app launched in this session")
	}
	f.tapLocked(x, y)
	return &protocol.Response{OK: true}
}

func (f *FakeBackend) Type(x, y int, text string) *protocol.Response {
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.started {
		return errResp("no app launched in this session")
	}
	// Typing taps the target first to focus it, mirroring how a real
	// keyboard works: the coordinates decide which field receives text.
	f.tapLocked(x, y)
	switch f.focused {
	case "email":
		f.email += text
	case "password":
		f.password += text
	default:
		return errResp("type: no editable field at (%d,%d) — tap the email or password field first", x, y)
	}
	return &protocol.Response{OK: true}
}

func (f *FakeBackend) tapLocked(x, y int) {
	switch {
	case !f.signedIn && inside(x, y, emailBounds):
		f.focused = "email"
	case !f.signedIn && inside(x, y, passwordBounds):
		f.focused = "password"
	case inside(x, y, buttonBounds):
		f.pressSignInLocked()
	default:
		f.focused = ""
	}
}

func (f *FakeBackend) pressSignInLocked() {
	switch {
	case f.signedIn:
		f.signedIn = false
		f.status = "Signed out."
		f.email, f.password = "", ""
		f.focused = ""
	case f.email == "":
		f.status = "Enter an email first."
	case f.password == "":
		f.status = "Enter a password first."
	default:
		f.signedIn = true
		f.status = "Signed in as " + f.email
	}
}

func (f *FakeBackend) Screenshot() *protocol.Response {
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.started {
		return errResp("no app launched in this session")
	}
	png, err := f.renderPNG()
	if err != nil {
		return errResp("screenshot: %v", err)
	}
	return &protocol.Response{OK: true, PNG: png}
}

func (f *FakeBackend) Close() *protocol.Response {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.started = false
	return &protocol.Response{OK: true}
}

func (f *FakeBackend) Stop() {}

func (f *FakeBackend) tree() *uitree.Node {
	label, resID := "Sign In", "demo:id/sign_in"
	if f.signedIn {
		label = "Sign Out"
	}
	return &uitree.Node{
		Class:  "android.widget.FrameLayout",
		Bounds: frameBounds,
		Children: []*uitree.Node{
			{
				Class:  "android.widget.TextView",
				Text:   "AgentDroid Demo",
				Bounds: titleBounds,
			},
			{
				Class:       "android.widget.EditText",
				Text:        f.email,
				ContentDesc: "Email",
				ResourceID:  "demo:id/email",
				Bounds:      emailBounds,
				Clickable:   true,
				Focusable:   true,
			},
			{
				Class:       "android.widget.EditText",
				Text:        f.password,
				ContentDesc: "Password",
				ResourceID:  "demo:id/password",
				Bounds:      passwordBounds,
				Clickable:   true,
				Focusable:   true,
			},
			{
				Class:      "android.widget.TextView",
				Text:       f.status,
				ResourceID: "demo:id/status",
				Bounds:     statusBounds,
			},
			{
				Class:      "android.widget.Button",
				Text:       label,
				ResourceID: resID,
				Bounds:     buttonBounds,
				Clickable:  true,
			},
		},
	}
}

// Palette of the simulated frame. A focused field gets an amber border so
// screenshots really differ between interaction states.
var (
	colorBg     = color.RGBA{R: 0xf2, G: 0xf3, B: 0xf5, A: 0xff}
	colorHeader = color.RGBA{R: 0x1e, G: 0x88, B: 0xe5, A: 0xff}
	colorField  = color.RGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff}
	colorButton = color.RGBA{R: 0x2e, G: 0x7d, B: 0x32, A: 0xff}
	colorStatus = color.RGBA{R: 0xe8, G: 0xea, B: 0xed, A: 0xff}
	colorFocus  = color.RGBA{R: 0xff, G: 0xb3, B: 0x00, A: 0xff}
)

// renderPNG draws the current UI state into a 540x960 PNG. The rendering is
// deterministic geometry, so it needs no fonts and no platform graphics.
func (f *FakeBackend) renderPNG() ([]byte, error) {
	img := image.NewRGBA(image.Rect(0, 0, frameBounds[2], frameBounds[3]))
	draw.Draw(img, img.Bounds(), &image.Uniform{colorBg}, image.Point{}, draw.Src)
	fill := func(b uitree.Bounds, c color.RGBA) {
		draw.Draw(img, image.Rect(b[0], b[1], b[0]+b[2], b[1]+b[3]), &image.Uniform{c}, image.Point{}, draw.Src)
	}
	fill(uitree.Bounds{0, 0, frameBounds[2], 120}, colorHeader)
	fill(emailBounds, colorField)
	fill(passwordBounds, colorField)
	fill(statusBounds, colorStatus)
	fill(buttonBounds, colorButton)
	switch f.focused {
	case "email":
		border(img, emailBounds, colorFocus, 4)
	case "password":
		border(img, passwordBounds, colorFocus, 4)
	}

	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func border(img *image.RGBA, b uitree.Bounds, c color.RGBA, t int) {
	draw.Draw(img, image.Rect(b[0], b[1], b[0]+b[2], b[1]+t), &image.Uniform{c}, image.Point{}, draw.Src)
	draw.Draw(img, image.Rect(b[0], b[1]+b[3]-t, b[0]+b[2], b[1]+b[3]), &image.Uniform{c}, image.Point{}, draw.Src)
	draw.Draw(img, image.Rect(b[0], b[1], b[0]+t, b[1]+b[3]), &image.Uniform{c}, image.Point{}, draw.Src)
	draw.Draw(img, image.Rect(b[0]+b[2]-t, b[1], b[0]+b[2], b[1]+b[3]), &image.Uniform{c}, image.Point{}, draw.Src)
}
