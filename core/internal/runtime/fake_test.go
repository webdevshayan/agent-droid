package runtime

import (
	"bytes"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/webdevshayan/agent-droid/core/internal/protocol"
	"github.com/webdevshayan/agent-droid/core/internal/uitree"
)

func fakeLaunched(t *testing.T) *FakeBackend {
	t.Helper()
	f := NewFakeBackend("")
	resp := f.Launch(makeApk(t))
	if !resp.OK {
		t.Fatalf("launch: %s", resp.Error)
	}
	t.Cleanup(f.Stop)
	return f
}

func TestFakeLaunchRequiresApk(t *testing.T) {
	f := NewFakeBackend("")
	if resp := f.Launch(""); resp.OK {
		t.Fatal("launch without apk must fail")
	}
	if resp := f.Launch("/definitely/missing/app.apk"); resp.OK {
		t.Fatal("launch with unreadable apk must fail")
	}
}

func TestFakeSnapshotTree(t *testing.T) {
	f := fakeLaunched(t)
	resp := f.Snapshot()
	if !resp.OK || resp.Tree == nil {
		t.Fatalf("snapshot: %+v", resp)
	}
	els := uitree.Flatten(resp.Tree)
	if len(els) != 5 {
		t.Fatalf("expected 5 elements (title, email, password, status, button), got %d", len(els))
	}
	if els[0].Node.Text != "AgentDroid Demo" || els[4].Node.Text != "Sign In" {
		t.Fatalf("unexpected order: %+v", els)
	}
	// The email field is exposed with its id, desc and focusability.
	email := els[1].Node
	if email.ResourceID != "demo:id/email" || email.ContentDesc != "Email" || !email.Focusable {
		t.Fatalf("email field: %+v", email)
	}
}

func TestFakeSnapshotBeforeLaunch(t *testing.T) {
	f := NewFakeBackend("")
	if resp := f.Snapshot(); resp.OK {
		t.Fatal("snapshot before launch must fail")
	}
}

func TestFakeSignInFlow(t *testing.T) {
	f := fakeLaunched(t)

	// Sign in with nothing filled → email validation message.
	if resp := f.Tap(center(buttonBounds)); !resp.OK {
		t.Fatalf("tap button: %s", resp.Error)
	}
	if got := statusText(t, f); got != "Enter an email first." {
		t.Fatalf("status after empty sign-in: %q", got)
	}

	// Fill email only → password validation message.
	ex, ey := center(emailBounds)
	if resp := f.Type(ex, ey, "shayan@example.com"); !resp.OK {
		t.Fatalf("type email: %s", resp.Error)
	}
	if resp := f.Tap(center(buttonBounds)); !resp.OK {
		t.Fatalf("tap button: %s", resp.Error)
	}
	if got := statusText(t, f); got != "Enter a password first." {
		t.Fatalf("status after email-only sign-in: %q", got)
	}

	// Fill password and sign in.
	px, py := center(passwordBounds)
	if resp := f.Type(px, py, "hunter2"); !resp.OK {
		t.Fatalf("type password: %s", resp.Error)
	}
	if resp := f.Tap(center(buttonBounds)); !resp.OK {
		t.Fatalf("tap button: %s", resp.Error)
	}
	if got := statusText(t, f); got != "Signed in as shayan@example.com" {
		t.Fatalf("status after sign-in: %q", got)
	}

	// The button turned into Sign Out, and signing out resets the form.
	els := uitree.Flatten(f.Snapshot().Tree)
	if els[4].Node.Text != "Sign Out" {
		t.Fatalf("button label after sign-in: %q", els[4].Node.Text)
	}
	if resp := f.Tap(center(buttonBounds)); !resp.OK {
		t.Fatalf("tap sign out: %s", resp.Error)
	}
	if got := statusText(t, f); got != "Signed out." {
		t.Fatalf("status after sign-out: %q", got)
	}
	if got := f.email; got != "" {
		t.Fatalf("email after sign-out: %q", got)
	}
}

func TestFakeTypeOutsideFieldFails(t *testing.T) {
	f := fakeLaunched(t)
	resp := f.Type(500, 900, "hello")
	if resp.OK || resp.Error == "" {
		t.Fatalf("typing into empty space must fail: %+v", resp)
	}
}

func TestFakeTapOutsideFieldsClearsFocus(t *testing.T) {
	f := fakeLaunched(t)
	if resp := f.Tap(center(emailBounds)); !resp.OK {
		t.Fatalf("tap email: %s", resp.Error)
	}
	if f.focused != "email" {
		t.Fatalf("focus: %q", f.focused)
	}
	if resp := f.Tap(500, 900); !resp.OK {
		t.Fatalf("tap nowhere: %s", resp.Error)
	}
	if f.focused != "" {
		t.Fatalf("focus after tap outside: %q", f.focused)
	}
}

func TestFakeScreenshot(t *testing.T) {
	f := fakeLaunched(t)

	if resp := f.Tap(center(emailBounds)); !resp.OK {
		t.Fatalf("tap email: %s", resp.Error)
	}
	focused := f.Screenshot()
	if !focused.OK || len(focused.PNG) == 0 {
		t.Fatalf("screenshot: %+v", focused)
	}

	img, err := png.Decode(bytes.NewReader(focused.PNG))
	if err != nil {
		t.Fatalf("screenshot is not a PNG: %v", err)
	}
	if got := img.Bounds(); got.Dx() != 540 || got.Dy() != 960 {
		t.Fatalf("screenshot size: %v", got)
	}

	// The frame must be non-blank and show the amber focus border on the
	// email field's top edge.
	if !pixelIs(img, emailBounds[0]+20, emailBounds[1]+2, colorFocus) {
		t.Fatal("focus border not visible on email field")
	}
	if pixelIs(img, emailBounds[0]+20, emailBounds[1]+30, colorFocus) {
		t.Fatal("focus border rendered where it should not be")
	}

	// Focusing a different field moves the border.
	if resp := f.Tap(center(passwordBounds)); !resp.OK {
		t.Fatalf("tap password: %s", resp.Error)
	}
	moved := f.Screenshot()
	if !pixelIs(pngImage(t, moved), passwordBounds[0]+20, passwordBounds[1]+2, colorFocus) {
		t.Fatal("focus border not visible on password field")
	}
	if pixelIs(pngImage(t, moved), emailBounds[0]+20, emailBounds[1]+2, colorFocus) {
		t.Fatal("email border should be gone")
	}
}

func TestFakeCloseAndRestart(t *testing.T) {
	f := fakeLaunched(t)
	if resp := f.Close(); !resp.OK {
		t.Fatalf("close: %s", resp.Error)
	}
	if resp := f.Snapshot(); resp.OK {
		t.Fatal("snapshot after close must fail")
	}
	// A fresh launch reuses the same backend object (as the serve loop does).
	if resp := f.Launch(makeApk(t)); !resp.OK {
		t.Fatalf("relaunch: %s", resp.Error)
	}
	if resp := f.Snapshot(); !resp.OK {
		t.Fatalf("snapshot after relaunch: %s", resp.Error)
	}
}

func TestFakeConcurrentAccess(t *testing.T) {
	f := fakeLaunched(t)
	done := make(chan struct{})
	for i := 0; i < 4; i++ {
		go func(i int) {
			defer func() { done <- struct{}{} }()
			ex, ey := center(emailBounds)
			for j := 0; j < 50; j++ {
				switch (i + j) % 4 {
				case 0:
					f.Snapshot()
				case 1:
					f.Tap(ex, ey)
				case 2:
					f.Type(ex, ey, "a")
				case 3:
					f.Screenshot()
				}
			}
		}(i)
	}
	for i := 0; i < 4; i++ {
		<-done
	}
}

func statusText(t *testing.T, f *FakeBackend) string {
	t.Helper()
	resp := f.Snapshot()
	if !resp.OK || resp.Tree == nil {
		t.Fatalf("snapshot: %+v", resp)
	}
	els := uitree.Flatten(resp.Tree)
	for _, el := range els {
		if el.Node.ResourceID == "demo:id/status" {
			return el.Node.Text
		}
	}
	t.Fatal("status element not found")
	return ""
}

func pngImage(t *testing.T, resp *protocol.Response) image.Image {
	t.Helper()
	img, err := png.Decode(bytes.NewReader(resp.PNG))
	if err != nil {
		t.Fatalf("png: %v", err)
	}
	return img
}

func pixelIs(img image.Image, x, y int, want interface{ RGBA() (r, g, b, a uint32) }) bool {
	r, g, b, a := img.At(x, y).RGBA()
	wr, wg, wb, wa := want.RGBA()
	return r == wr && g == wg && b == wb && a == wa
}

func center(b uitree.Bounds) (int, int) {
	return b[0] + b[2]/2, b[1] + b[3]/2
}

func makeApk(t *testing.T) string {
	t.Helper()
	return makeApkAt(t, "demo.apk")
}

func makeApkAt(t *testing.T, name string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, []byte("PK\x03\x04 fake apk payload"), 0o644); err != nil {
		t.Fatalf("write apk: %v", err)
	}
	return p
}
