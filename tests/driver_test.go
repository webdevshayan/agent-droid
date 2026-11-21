package tests_test

// The ADB adapter: verifies driver.Driver wiring onto a real
// device.Client — call shapes, charset gate pass-through, error paths.

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/webdevshayan/agent-droid/core/device"
	"github.com/webdevshayan/agent-droid/core/driver"
)

func TestADBAdapterWiring(t *testing.T) {
	dir := t.TempDir()
	apk := filepath.Join(dir, "app.apk")
	if err := os.WriteFile(apk, []byte("PK\x03\x04fake"), 0o644); err != nil {
		t.Fatal(err)
	}

	f := &fakeRunner{respond: func(args []string) ([]byte, error) {
		return []byte("List of devices attached\nEMU1\tdevice\n"), nil
	}}
	d := driver.NewADB(&device.Client{Adb: f})

	listing, err := d.Devices()
	if err != nil || !strings.Contains(listing, "EMU1") {
		t.Fatalf("Devices = %q, %v", listing, err)
	}
	// Reset the scripted responses to the plain recorder for the rest.
	f.respond = nil

	if err := d.Launch(apk); err != nil {
		t.Fatalf("Launch: %v", err)
	}
	if !reflect.DeepEqual(f.calls[1], []string{"install", "-r", apk}) {
		t.Fatalf("Launch adb call = %v", f.calls[0])
	}

	if err := d.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if !reflect.DeepEqual(f.calls[len(f.calls)-1], []string{"shell", "input", "keyevent", "3"}) {
		t.Fatalf("Close must send HOME, got %v", f.calls[len(f.calls)-1])
	}

	if err := d.Key(66); err != nil {
		t.Fatalf("Key: %v", err)
	}
	if !reflect.DeepEqual(f.calls[len(f.calls)-1], []string{"shell", "input", "keyevent", "66"}) {
		t.Fatalf("Key adb call = %v", f.calls[len(f.calls)-1])
	}

	if err := d.Tap(3, 4); err != nil {
		t.Fatalf("Tap: %v", err)
	}
	if !reflect.DeepEqual(f.calls[len(f.calls)-1], []string{"shell", "input", "tap", "3", "4"}) {
		t.Fatalf("Tap adb call = %v", f.calls[len(f.calls)-1])
	}

	if err := d.Type("a b"); err != nil {
		t.Fatalf("Type: %v", err)
	}
	if !reflect.DeepEqual(f.calls[len(f.calls)-1], []string{"shell", "input", "text", "a%sb"}) {
		t.Fatalf("Type adb call = %v", f.calls[len(f.calls)-1])
	}

	// The charset gate must fire at this layer too — before any adb call.
	before := len(f.calls)
	if err := d.Type("a;b"); err == nil {
		t.Fatal("Type with metacharacters must be rejected")
	}
	if len(f.calls) != before {
		t.Fatal("rejected Type must not reach adb")
	}

	// Dump over a junk dump → clean error after the full fallback chain.
	if _, err := d.Dump(); err == nil || !strings.Contains(err.Error(), "no parsable XML") {
		t.Fatalf("Dump over junk = %v", err)
	}

	// Screenshot passes raw bytes through.
	shot, err := d.Screenshot()
	if err != nil || string(shot) != "ok" {
		t.Fatalf("Screenshot = %q, %v", shot, err)
	}
}

func TestADBAdapterDumpXML(t *testing.T) {
	f := &fakeRunner{respond: func(args []string) ([]byte, error) {
		for i := 0; i+1 < len(args); i++ {
			if args[i] == "exec-out" && args[i+1] == "uiautomator" {
				return []byte(screenXML), nil
			}
		}
		return []byte("ok"), nil
	}}
	d := driver.NewADB(&device.Client{Adb: f})
	raw, err := d.Dump()
	if err != nil || !strings.Contains(string(raw), "Settings") {
		t.Fatalf("Dump = %q, %v", raw, err)
	}
}

func TestADBAdapterInstallValidation(t *testing.T) {
	f := &fakeRunner{}
	d := driver.NewADB(&device.Client{Adb: f})
	if err := d.Launch("/no/such/app.apk"); err == nil || !strings.Contains(err.Error(), "APK not found") {
		t.Fatalf("missing APK must be caught before adb, got %v", err)
	}
	f.assertNoCalls(t)
	if err := d.Launch("demo.txt"); err == nil || !strings.Contains(err.Error(), ".apk") {
		t.Fatalf("non-APK extension must be rejected, got %v", err)
	}
	f.assertNoCalls(t)
}

func TestADBAdapterTapCoordGate(t *testing.T) {
	f := &fakeRunner{}
	d := driver.NewADB(&device.Client{Adb: f})
	if err := d.Tap(-1, 0); err == nil {
		t.Fatal("negative coordinate must be rejected")
	}
	if err := d.Swipe(0, 0, 200000, 0, 300); err == nil {
		t.Fatal("oversized coordinate must be rejected")
	}
	f.assertNoCalls(t)
}
