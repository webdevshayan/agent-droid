package tests_test

// Branch-hardening tests: every error path in the adb executor, the
// dispatch layer and the bounds parser, exercised directly. These push
// coverage of the four internal packages over the 90% gate.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/webdevshayan/agent-droid/core/device"
	"github.com/webdevshayan/agent-droid/core/snapshot"
)

func TestExecRunnerCapsOversizedOutput(t *testing.T) {
	r := device.ExecRunner{Bin: "/bin/sh"}
	// 70 MB of stdout against the 64 MiB cap: the excess is swallowed,
	// never buffered, and surfaced as an error.
	_, err := r.Output("-c", "head -c 70000000 /dev/zero")
	if err == nil || !strings.Contains(err.Error(), "more than") {
		t.Fatalf("oversized output must be capped with an error, got %v", err)
	}
	out, err := r.Output("-c", "echo hello")
	if err != nil || !strings.Contains(string(out), "hello") {
		t.Fatalf("normal output must flow through, got %q, %v", out, err)
	}
}

func TestExecRunnerTimesOut(t *testing.T) {
	r := device.ExecRunner{Bin: "/bin/sh", Timeout: 100 * time.Millisecond}
	_, err := r.Output("-c", "sleep 2")
	if err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("slow command must hit the timeout, got %v", err)
	}
}

func TestDetectFindsFakeAdb(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "adb")
	body := "#!/bin/sh\nprintf 'List of devices attached\\nFAKE1\\tdevice\\n'\n"
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	r, err := device.Detect()
	if err != nil {
		t.Fatalf("Detect must find the fake adb, got %v", err)
	}
	out, err := r.Output("devices", "-l")
	if err != nil || !strings.Contains(string(out), "FAKE1") {
		t.Fatalf("fake adb output = %q, %v", out, err)
	}
	c, err := device.New("", nil)
	if err != nil {
		t.Fatalf("device.New must use the detected adb: %v", err)
	}
	listing, err := c.Devices()
	if err != nil || !strings.Contains(listing, "FAKE1") {
		t.Fatalf("Devices() = %q, %v", listing, err)
	}
}

func TestSnapshotNoParsableXML(t *testing.T) {
	f := &fakeRunner{respond: func(args []string) ([]byte, error) {
		if len(args) >= 2 && args[len(args)-2] == "exec-out" && args[len(args)-1] == "uiautomator" {
			return []byte("UI hierchary dumped to: /dev/tty\n"), nil
		}
		return []byte("nothing here"), nil
	}}
	c := &device.Client{Adb: f}
	_, err := c.Snapshot()
	if err == nil || !strings.Contains(err.Error(), "no parsable XML") {
		t.Fatalf("all-junk fallback chain must end in a clean error, got %v", err)
	}
	if len(f.calls) != 4 {
		// dump + cat + the best-effort cleanup rm (AGD-15).
		t.Fatalf("full fallback chain must run (4 calls), got %d", len(f.calls))
	}
}

func TestParseBoundsMalformedVariants(t *testing.T) {
	xml := "<hierarchy>" +
		`<node class="a" bounds="[1,2][3]"/>` +
		`<node class="b" bounds="[1,2,3][4,5,6]"/>` +
		`<node class="c" bounds="[a,b][c,d]"/>` +
		`<node class="d" bounds="[1,2][3,4"/>` +
		`<node class="e" bounds="[]"/>` +
		`<node class="f" bounds="x"/>` +
		`<node class="g" bounds="[0,0][-1,10]"/>` +
		`<node class="ok" bounds="[0,0][10,10]"/>` +
		"</hierarchy>"
	els, err := snapshot.Parse([]byte(xml))
	if err != nil {
		t.Fatalf("malformed bounds must be skipped, not fatal: %v", err)
	}
	if len(els) != 1 || els[0].Class != "ok" {
		t.Fatalf("only the well-formed node survives, got %+v", els)
	}
}

func TestFormatScrollableAndPkg(t *testing.T) {
	els, err := snapshot.Parse([]byte(
		`<hierarchy><node class="androidx.recyclerview" package="com.app" scrollable="true" bounds="[0,0][1080,100]"/></hierarchy>`))
	if err != nil {
		t.Fatal(err)
	}
	out := snapshot.Format(els)
	for _, want := range []string{"pkg=com.app", "scrollable", "center=(540,50)"} {
		if !strings.Contains(out, want) {
			t.Fatalf("Format missing %q in: %s", want, out)
		}
	}
}
