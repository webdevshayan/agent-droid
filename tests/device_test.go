package tests_test

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/webdevshayan/agent-droid/core/device"
)

// fakeRunner records every adb invocation and replies from a script.
// Tests assert both the returned errors and exactly which adb commands a
// code path produced.
type fakeRunner struct {
	calls   [][]string
	respond func(args []string) ([]byte, error)
}

func (f *fakeRunner) Output(args ...string) ([]byte, error) {
	cp := make([]string, len(args))
	copy(cp, args)
	f.calls = append(f.calls, cp)
	if f.respond == nil {
		return []byte("ok"), nil
	}
	return f.respond(args)
}

func (f *fakeRunner) assertNoCalls(t *testing.T) {
	t.Helper()
	if len(f.calls) != 0 {
		t.Fatalf("expected no adb calls, got %v", f.calls)
	}
}

func TestValidateText(t *testing.T) {
	ok := map[string]string{
		"hello":                                 "hello",
		"hello world":                           "hello%sworld",
		"email@example.com":                     "email@example.com",
		"a-b_c.d,e!f?g@h:i#j~k^l%m*n+o=p/":      "a-b_c.d,e!f?g@h:i#j~k^l%m*n+o=p/",
		strings.Repeat("a", device.MaxInputLen): strings.Repeat("a", device.MaxInputLen),
	}
	for in, want := range ok {
		got, err := device.ValidateText(in)
		if err != nil || got != want {
			t.Fatalf("ValidateText(%q) = %q, %v; want %q, nil", in, got, err, want)
		}
	}
	for _, in := range []string{
		"", "   \t", "\n", "\r", "\t", "\x00", "a;b", "a|b", "a&b", "a$b",
		"a`b", "a\"b", "a'b", "a\\b", "a<b", "a>b", "a(b", "a)b", "héllo",
		"emoji 🤖", strings.Repeat("a", device.MaxInputLen+1),
	} {
		if got, err := device.ValidateText(in); err == nil {
			t.Fatalf("ValidateText(%q) = %q, must be rejected", in, got)
		}
	}
	// The rejection message must quote only the offending character —
	// never echo a huge payload back.
	_, err := device.ValidateText(strings.Repeat("x", 4000) + "\n")
	if err == nil || strings.Contains(err.Error(), "xxxx") {
		t.Fatalf("rejection must not echo the payload, got: %v", err)
	}
}

func TestTapCoordBounds(t *testing.T) {
	f := &fakeRunner{}
	c := &device.Client{Serial: "", Adb: f}
	for _, xy := range [][2]int{{-1, 0}, {0, -1}, {device.MaxCoord + 1, 0}, {0, device.MaxCoord + 1}} {
		if err := c.Tap(xy[0], xy[1]); err == nil {
			t.Fatalf("Tap(%d,%d) must fail validation", xy[0], xy[1])
		}
	}
	f.assertNoCalls(t)

	if err := c.Tap(5, 10); err != nil {
		t.Fatalf("Tap(5,10) failed: %v", err)
	}
	want := []string{"shell", "input", "tap", "5", "10"}
	if !reflect.DeepEqual(f.calls[0], want) {
		t.Fatalf("adb call = %v, want %v", f.calls[0], want)
	}
}

func TestSwipeArgs(t *testing.T) {
	f := &fakeRunner{}
	c := &device.Client{Adb: f}
	if err := c.Swipe(10, 20, 30, 40, 300); err != nil {
		t.Fatalf("Swipe failed: %v", err)
	}
	want := []string{"shell", "input", "swipe", "10", "20", "30", "40", "300"}
	if !reflect.DeepEqual(f.calls[0], want) {
		t.Fatalf("adb call = %v, want %v", f.calls[0], want)
	}
	if err := c.Swipe(-1, 0, 1, 1, 300); err == nil {
		t.Fatal("Swipe with negative coordinate must fail")
	}
	if len(f.calls) != 1 {
		t.Fatalf("rejected Swipe must not reach adb, got %d calls: %v", len(f.calls), f.calls)
	}
}

func TestInstallValidation(t *testing.T) {
	dir := t.TempDir()
	f := &fakeRunner{}
	c := &device.Client{Adb: f}

	if err := c.Install("/nonexistent/path/app.apk"); err == nil || !strings.Contains(err.Error(), "APK not found") {
		t.Fatalf("missing APK must report not-found, got %v", err)
	}
	f.assertNoCalls(t)

	if err := c.Install(filepath.Join(dir, "app.txt")); err == nil || !strings.Contains(err.Error(), ".apk") {
		t.Fatalf("non-.apk path must be rejected, got %v", err)
	}
	f.assertNoCalls(t)

	sub := filepath.Join(dir, "sub.apk")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := c.Install(sub); err == nil || !strings.Contains(err.Error(), "is a directory") {
		t.Fatalf("directory path must be rejected, got %v", err)
	}
	f.assertNoCalls(t)

	empty := filepath.Join(dir, "empty.apk")
	if err := os.WriteFile(empty, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := c.Install(empty); err == nil || !strings.Contains(err.Error(), "empty (0 bytes)") {
		t.Fatalf("empty APK must be rejected, got %v", err)
	}
	f.assertNoCalls(t)

	real := filepath.Join(dir, "real.apk")
	if err := os.WriteFile(real, []byte("PK\x03\x04fakezip"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := c.Install(real); err != nil {
		t.Fatalf("valid path must reach adb, got %v", err)
	}
	want := []string{"install", "-r", real}
	if !reflect.DeepEqual(f.calls[0], want) {
		t.Fatalf("adb call = %v, want %v", f.calls[0], want)
	}
}

func TestInstallRejectsNonZipAPK(t *testing.T) {
	// AGD-20: a text file named .apk must be rejected locally with the PK
	// header error — never shipped to adb to fail device-side.
	dir := t.TempDir()
	text := filepath.Join(dir, "notzip.apk")
	if err := os.WriteFile(text, []byte("definitely not a zip archive"), 0o644); err != nil {
		t.Fatal(err)
	}
	f := &fakeRunner{}
	c := &device.Client{Adb: f}
	err := c.Install(text)
	if err == nil || !strings.Contains(err.Error(), "missing the PK header") {
		t.Fatalf("non-ZIP .apk must be rejected locally, got %v", err)
	}
	f.assertNoCalls(t)
}

func TestSnapshotFallbackChain(t *testing.T) {
	xml := []byte(`<?xml version="1.0"?><hierarchy/>`)
	var step int
	f := &fakeRunner{respond: func(args []string) ([]byte, error) {
		step++
		switch step {
		case 1: // exec-out uiautomator dump /dev/tty → junk output
			return []byte("UI hierchary dumped to: /dev/tty\n"), nil
		case 2: // fallback dump
			return []byte("UI hierchary dumped to: /sdcard/agentdroid_dump.xml\n"), nil
		case 3: // fallback cat
			return xml, nil
		case 4: // best-effort rm of the per-invocation dump file
			if !strings.HasPrefix(args[len(args)-1], "/sdcard/agentdroid_dump_") || args[len(args)-2] != "-f" {
				return nil, errors.New("cleanup must rm -f the per-invocation dump path")
			}
			return []byte(""), nil
		}
		return nil, errors.New("unexpected extra call")
	}}
	c := &device.Client{Adb: f}
	got, err := c.Snapshot()
	if err != nil {
		t.Fatalf("Snapshot fallback failed: %v", err)
	}
	if string(got) != string(xml) {
		t.Fatalf("Snapshot returned %q, want the XML", got)
	}
	if len(f.calls) != 4 {
		t.Fatalf("fallback should be dump + cat + cleanup (4 adb calls), got %d: %v", len(f.calls), f.calls)
	}
	dumped := f.calls[1][len(f.calls[1])-1]
	if !strings.HasPrefix(dumped, "/sdcard/agentdroid_dump_") || !strings.HasSuffix(dumped, ".xml") {
		t.Fatalf("dump path must be per-invocation, got %q", dumped)
	}
	if dumped == "/sdcard/agentdroid_dump.xml" {
		t.Fatal("the fixed shared dump path must be gone (AGD-15)")
	}
}

func TestSnapshotBothPathsFail(t *testing.T) {
	f := &fakeRunner{respond: func(args []string) ([]byte, error) {
		return nil, errors.New("device offline")
	}}
	c := &device.Client{Adb: f}
	_, err := c.Snapshot()
	if err == nil || !strings.Contains(err.Error(), "dump failed") {
		t.Fatalf("both-fail path must surface a dump error, got %v", err)
	}
}

func TestDevicesListing(t *testing.T) {
	f := &fakeRunner{respond: func(args []string) ([]byte, error) {
		return []byte("List of devices attached\nEMU123\tdevice product:sdk model:x\n"), nil
	}}
	c := &device.Client{Adb: f}
	out, err := c.Devices()
	if err != nil || !strings.Contains(out, "EMU123") {
		t.Fatalf("Devices() = %q, %v", out, err)
	}

	f2 := &fakeRunner{respond: func(args []string) ([]byte, error) {
		return []byte("List of devices attached\n"), nil
	}}
	c2 := &device.Client{Adb: f2}
	if _, err := c2.Devices(); err == nil || !strings.Contains(err.Error(), "no devices attached") {
		t.Fatalf("empty listing must error, got %v", err)
	}
}

func TestKeyBackHome(t *testing.T) {
	f := &fakeRunner{}
	c := &device.Client{Adb: f}
	if err := c.KeyBack(); err != nil {
		t.Fatalf("KeyBack: %v", err)
	}
	if err := c.KeyHome(); err != nil {
		t.Fatalf("KeyHome: %v", err)
	}
	want := [][]string{{"shell", "input", "keyevent", "4"}, {"shell", "input", "keyevent", "3"}}
	if !reflect.DeepEqual(f.calls, want) {
		t.Fatalf("calls = %v, want %v", f.calls, want)
	}
}

func TestSerialIsPassedThrough(t *testing.T) {
	f := &fakeRunner{}
	c := &device.Client{Serial: "EMU9", Adb: f}
	if err := c.Tap(1, 2); err != nil {
		t.Fatalf("Tap: %v", err)
	}
	if !reflect.DeepEqual(f.calls[0], []string{"-s", "EMU9", "shell", "input", "tap", "1", "2"}) {
		t.Fatalf("serial missing from adb call: %v", f.calls[0])
	}
}

func TestExecRunnerSurfacesStderr(t *testing.T) {
	// Use /bin/false as a stand-in adb: exits 1 with empty stderr.
	// The error must come from the wrapped failure, gracefully worded.
	r := device.ExecRunner{Bin: "/bin/false"}
	_, err := r.Output("shell", "input", "tap", "1", "2")
	if err == nil {
		t.Fatal("failing binary must produce an error")
	}
	if !strings.Contains(err.Error(), "adb shell") {
		t.Fatalf("error should name the adb subcommand, got: %v", err)
	}

	// A nonexistent binary must fail cleanly too.
	r2 := device.ExecRunner{Bin: "/nonexistent/adb-binary"}
	if _, err := r2.Output("devices"); err == nil {
		t.Fatal("missing binary must produce an error")
	}
}

func TestScreenBytes(t *testing.T) {
	png := []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}
	f := &fakeRunner{respond: func(args []string) ([]byte, error) {
		return png, nil
	}}
	c := &device.Client{Adb: f}
	got, err := c.ScreenBytes()
	if err != nil || string(got) != string(png) {
		t.Fatalf("ScreenBytes = %v, %v", got, err)
	}
	if !reflect.DeepEqual(f.calls[0], []string{"exec-out", "screencap", "-p"}) {
		t.Fatalf("adb call = %v", f.calls[0])
	}
	f2 := &fakeRunner{respond: func(args []string) ([]byte, error) {
		return nil, nil
	}}
	c2 := &device.Client{Adb: f2}
	if _, err := c2.ScreenBytes(); err == nil || !strings.Contains(err.Error(), "no data") {
		t.Fatalf("empty screencap must error, got %v", err)
	}
}

func TestKeyAnyCode(t *testing.T) {
	f := &fakeRunner{}
	c := &device.Client{Adb: f}
	if err := c.Key(66); err != nil {
		t.Fatalf("Key(66): %v", err)
	}
	if !reflect.DeepEqual(f.calls[0], []string{"shell", "input", "keyevent", "66"}) {
		t.Fatalf("adb call = %v", f.calls[0])
	}
}
