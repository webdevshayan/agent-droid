package driver

// RT over the fake backend, wired through the real session protocol on
// net.Pipe: the exact path `agentdroid serve` + `--session` commands take,
// minus the kernel socket. This is the conformance test for putting
// ATL/WineDroid-class runtimes behind the driver.Driver contract.

import (
	"bytes"
	"image/png"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/webdevshayan/agent-droid/core/internal/runtime"
	"github.com/webdevshayan/agent-droid/core/snapshot"
)

// newRTSession starts a fake backend served by the real protocol loop on a
// pipe and returns an RT driver attached to it.
func newRTSession(t *testing.T) (*RT, *runtime.FakeBackend) {
	t.Helper()
	apk := filepath.Join(t.TempDir(), "demo.apk")
	if err := os.WriteFile(apk, []byte("PK\x03\x04 rt test"), 0o644); err != nil {
		t.Fatal(err)
	}
	b := runtime.NewFakeBackend(apk)
	return dialRT(t, b), b
}

func dialRT(t *testing.T, b *runtime.FakeBackend) *RT {
	t.Helper()
	cc, sc := net.Pipe()
	t.Cleanup(func() { cc.Close(); sc.Close() })
	go func() { _ = runtime.ServeConn(sc, sc, b) }()
	return NewRT(runtime.NewClient(cc))
}

// dumpElements dumps through RT and parses with the shared snapshot
// pipeline — the one ref semantics every driver must satisfy.
func dumpElements(t *testing.T, d *RT) []snapshot.Element {
	t.Helper()
	raw, err := d.Dump()
	if err != nil {
		t.Fatalf("Dump: %v", err)
	}
	els, err := snapshot.Parse(raw)
	if err != nil {
		t.Fatalf("snapshot.Parse rejected an RT dump: %v\n%s", err, raw)
	}
	return els
}

func TestRTDriverContract(t *testing.T) {
	d, b := newRTSession(t)

	// Devices is adb-only; Swipe and Key degrade to honest errors.
	if _, err := d.Devices(); err == nil || !strings.Contains(err.Error(), "adb runtime only") {
		t.Fatalf("Devices = %v, want adb-only error", err)
	}
	if err := d.Swipe(0, 0, 10, 10, 100); err == nil {
		t.Fatal("Swipe must be unsupported over sessions")
	}
	if err := d.Key(4); err == nil {
		t.Fatal("Key must be unsupported over sessions")
	}

	// Coordinate gates fire before any protocol traffic.
	if err := d.Tap(-1, 0); err == nil {
		t.Fatal("negative coordinate must be rejected")
	}
	if err := d.Tap(0, 100001); err == nil {
		t.Fatal("oversized coordinate must be rejected")
	}
	if err := d.Type(""); err == nil {
		t.Fatal("empty text must be rejected")
	}

	// Snapshot before launch fails cleanly, never panics.
	if _, err := d.Dump(); err == nil || !strings.Contains(err.Error(), "snapshot") {
		t.Fatalf("Dump before launch = %v", err)
	}
	// Screenshot before launch fails cleanly too.
	if _, err := d.Screenshot(); err == nil || !strings.Contains(err.Error(), "screenshot") {
		t.Fatalf("Screenshot before launch = %v", err)
	}

	// Launch validation happens backend-side; a missing APK is a clean error.
	if err := d.Launch("/no/such/demo.apk"); err == nil || !strings.Contains(err.Error(), "launch") {
		t.Fatalf("Launch validation = %v", err)
	}
	if err := d.Launch(""); err != nil {
		t.Fatalf("Launch with the session APK: %v", err)
	}

	// The fake login screen: 6 bounded nodes, refs @e1..@e6 in document
	// order, identical to what an adb dump would produce.
	els := dumpElements(t, d)
	if len(els) != 6 {
		t.Fatalf("got %d elements, want 6", len(els))
	}
	if els[2].ResourceID != "demo:id/email" || els[5].ResourceID != "demo:id/sign_in" {
		t.Fatalf("ref order broken: %q then %q", els[2].ResourceID, els[5].ResourceID)
	}

	// Type targets the last tap (the protocol's typed coordinates).
	if err := d.Tap(270, 178); err != nil { // email field
		t.Fatalf("Tap email: %v", err)
	}
	if err := d.Type("shayan@example.com"); err != nil {
		t.Fatalf("Type: %v", err)
	}

	// A second driver instance over the same session has no last tap: its
	// Type must land in the field the daemon has focused.
	d2 := dialRT(t, b)
	if err := d2.Type(" and more"); err != nil {
		t.Fatalf("coordinate-less Type into daemon focus: %v", err)
	}

	// The typed text is in the tree, exactly as typed.
	els = dumpElements(t, d)
	if !strings.Contains(els[2].Text, "shayan@example.com and more") {
		t.Fatalf("typed text lost: %q", els[2].Text)
	}

	// Password, then sign in.
	if err := d.Tap(270, 248); err != nil {
		t.Fatalf("Tap password: %v", err)
	}
	if err := d.Type("hunter2"); err != nil {
		t.Fatalf("Type password: %v", err)
	}
	if err := d.Tap(270, 448); err != nil { // Sign In button
		t.Fatalf("Tap sign-in: %v", err)
	}
	els = dumpElements(t, d)
	if !strings.Contains(els[4].Text, "Signed in as shayan@example.com and more") {
		t.Fatalf("signed-in status missing: %q", els[4].Text)
	}

	// Screenshot is a real PNG of the simulated frame.
	shot, err := d.Screenshot()
	if err != nil {
		t.Fatalf("Screenshot: %v", err)
	}
	img, err := png.Decode(bytes.NewReader(shot))
	if err != nil {
		t.Fatalf("screenshot is not a PNG: %v", err)
	}
	if img.Bounds().Dx() != 540 || img.Bounds().Dy() != 960 {
		t.Fatalf("screenshot size = %v, want 540x960", img.Bounds())
	}

	// Close ends the session cleanly.
	if err := d.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}

func TestRTUnknownRefSurfacesListing(t *testing.T) {
	// The ref error comes from the shared snapshot pipeline; RT surfaces it
	// verbatim so agents get the same message for adb and sessions.
	d, _ := newRTSession(t)
	if err := d.Launch(""); err != nil {
		t.Fatal(err)
	}
	raw, err := d.Dump()
	if err != nil {
		t.Fatal(err)
	}
	els, err := snapshot.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := snapshot.Resolve(els, "@e42"); err == nil || !strings.Contains(err.Error(), "6 tappable element") {
		t.Fatalf("unknown ref error = %v", err)
	}
}
