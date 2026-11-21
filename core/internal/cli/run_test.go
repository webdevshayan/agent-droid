package cli

import (
	"bytes"
	"image/png"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/webdevshayan/agent-droid/core/internal/runtime"
)

// startFakeSession binds the session's abstract socket with a launched fake
// backend, so the whole CLI execution path (client → socket → server →
// backend) runs for real, in-process.
func startFakeSession(t *testing.T, session string) {
	t.Helper()
	apk := filepath.Join(t.TempDir(), "unit.apk")
	if err := os.WriteFile(apk, []byte("PK\x03\x04 unit payload"), 0o644); err != nil {
		t.Fatalf("write apk: %v", err)
	}
	backend := runtime.NewFakeBackend("")
	if resp := backend.Launch(apk); !resp.OK {
		t.Fatalf("fake launch: %s", resp.Error)
	}
	ln, err := net.Listen("unix", "@"+runtime.SocketPrefix+session)
	if err != nil {
		t.Fatalf("bind session socket: %v", err)
	}
	done := make(chan struct{})
	t.Cleanup(func() {
		ln.Close()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
		}
	})
	go func() {
		defer close(done)
		_ = runtime.Serve(ln, backend, func() { ln.Close() })
	}()
	// Wait until the socket accepts.
	deadline := time.Now().Add(2 * time.Second)
	for {
		c, err := net.DialTimeout("unix", "@"+runtime.SocketPrefix+session, 100*time.Millisecond)
		if err == nil {
			c.Close()
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("session socket never came up: %v", err)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestExecuteVersionAndHelp(t *testing.T) {
	var out bytes.Buffer
	if code := Execute([]string{"version"}, &out, os.Stderr); code != 0 {
		t.Fatalf("version exit code %d", code)
	}
	if !strings.Contains(out.String(), "agentdroid "+Version) {
		t.Fatalf("version output: %q", out.String())
	}
	out.Reset()
	if code := Execute([]string{"help"}, &out, os.Stderr); code != 0 {
		t.Fatalf("help exit code %d", code)
	}
	if !strings.Contains(out.String(), "screenshot") {
		t.Fatalf("help output: %q", out.String())
	}
}

func TestExecuteUsageErrorExitCode(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := Execute([]string{"deploy"}, &out, &errOut); code != 2 {
		t.Fatalf("usage error exit code %d, want 2", code)
	}
	if !strings.Contains(errOut.String(), "unknown command") {
		t.Fatalf("usage error output: %q", errOut.String())
	}
}

func TestExecuteSessionFlow(t *testing.T) {
	if !isLinux() {
		t.Skip("abstract unix sockets are Linux-only")
	}
	session := "unit-" + time.Now().Format("150405.000000000")
	startFakeSession(t, session)

	run := func(args ...string) (int, string) {
		var out bytes.Buffer
		code := Execute(append(args, "--session", session), &out, os.Stderr)
		return code, out.String()
	}

	// snapshot: the fake login app lists five elements.
	code, out := run("snapshot")
	if code != 0 {
		t.Fatalf("snapshot exit %d", code)
	}
	for _, want := range []string{"@e1", "@e5", `text="Sign In"`, "Ready.", "# 5 elements"} {
		if !strings.Contains(out, want) {
			t.Fatalf("snapshot output missing %q:\n%s", want, out)
		}
	}

	// snapshot --tree renders the hierarchy.
	code, out = run("snapshot", "--tree")
	if code != 0 || !strings.Contains(out, "FrameLayout") || !strings.Contains(out, "@e4") {
		t.Fatalf("tree snapshot: code=%d out:\n%s", code, out)
	}

	// snapshot --json emits raw protocol JSON.
	code, out = run("snapshot", "--json")
	if code != 0 || !strings.Contains(out, `"tree"`) {
		t.Fatalf("json snapshot: code=%d out:\n%s", code, out)
	}

	// tap the email field (@e2) and type into it.
	code, out = run("tap", "@e2")
	if code != 0 || !strings.Contains(out, "tapped @e2 at (270,178)") {
		t.Fatalf("tap @e2: code=%d out=%q", code, out)
	}
	code, out = run("type", "@e2", "shayan@example.com")
	if code != 0 || !strings.Contains(out, "typed") {
		t.Fatalf("type @e2: code=%d out=%q", code, out)
	}
	_, out = run("snapshot")
	if !strings.Contains(out, `text="shayan@example.com"`) {
		t.Fatalf("typed text not visible in snapshot:\n%s", out)
	}

	// tap Sign In (@e5) without a password → validation message.
	code, _ = run("tap", "@e5")
	if code != 0 {
		t.Fatalf("tap @e5 exit %d", code)
	}
	_, out = run("snapshot")
	if !strings.Contains(out, "Enter a password first.") {
		t.Fatalf("validation status missing:\n%s", out)
	}

	// type the password, sign in, and see the state change.
	if code, _ := run("tap", "@e3"); code != 0 {
		t.Fatalf("tap @e3 exit %d", code)
	}
	if code, _ := run("type", "@e3", "hunter2"); code != 0 {
		t.Fatalf("type @e3 exit %d", code)
	}
	if code, _ := run("tap", "@e5"); code != 0 {
		t.Fatalf("tap @e5 exit %d", code)
	}
	_, out = run("snapshot")
	for _, want := range []string{"Signed in as shayan@example.com", "Sign Out"} {
		if !strings.Contains(out, want) {
			t.Fatalf("signed-in snapshot missing %q:\n%s", want, out)
		}
	}

	// screenshot writes a decodable PNG to stdout.
	var img bytes.Buffer
	code = Execute([]string{"screenshot", "--session", session}, &img, os.Stderr)
	if code != 0 {
		t.Fatalf("screenshot exit %d", code)
	}
	decoded, err := png.Decode(bytes.NewReader(img.Bytes()))
	if err != nil {
		t.Fatalf("screenshot is not a valid PNG: %v", err)
	}
	if got := decoded.Bounds(); got.Dx() != 540 || got.Dy() != 960 {
		t.Fatalf("screenshot size: %v", got)
	}

	// close ends the session; afterwards the socket is gone.
	code, out = run("close")
	if code != 0 || !strings.Contains(out, "closed") {
		t.Fatalf("close: code=%d out=%q", code, out)
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		c, err := net.DialTimeout("unix", "@"+runtime.SocketPrefix+session, 100*time.Millisecond)
		if err != nil {
			break
		}
		c.Close()
		if time.Now().After(deadline) {
			t.Fatal("session socket still alive after close")
		}
		time.Sleep(10 * time.Millisecond)
	}
	var errOut bytes.Buffer
	if code := Execute([]string{"snapshot", "--session", session}, &bytes.Buffer{}, &errOut); code != 1 {
		t.Fatalf("post-close snapshot exit %d, want 1", code)
	}
	if !strings.Contains(errOut.String(), "no live session") {
		t.Fatalf("post-close error: %q", errOut.String())
	}
}

func TestExecuteUnknownRefIsActionable(t *testing.T) {
	if !isLinux() {
		t.Skip("abstract unix sockets are Linux-only")
	}
	session := "unit-ref-" + time.Now().Format("150405.000000000")
	startFakeSession(t, session)
	defer func() {
		Execute([]string{"close", "--session", session}, &bytes.Buffer{}, &bytes.Buffer{})
	}()
	var errOut bytes.Buffer
	code := Execute([]string{"tap", "@e42", "--session", session}, &bytes.Buffer{}, &errOut)
	if code != 1 {
		t.Fatalf("exit %d, want 1", code)
	}
	if !strings.Contains(errOut.String(), "current screen has 5 elements") {
		t.Fatalf("unknown-ref error: %q", errOut.String())
	}
}

func TestExecuteLaunchRejectsMissingApk(t *testing.T) {
	if !isLinux() {
		t.Skip("abstract unix sockets are Linux-only")
	}
	session := "unit-launch-" + time.Now().Format("150405.000000000")
	var out, errOut bytes.Buffer
	code := Execute([]string{"launch", filepath.Join(t.TempDir(), "nope.apk"), "--runtime", "fake", "--session", session}, &out, &errOut)
	if code != 1 {
		t.Fatalf("exit %d, want 1", code)
	}
	if !strings.Contains(errOut.String(), "not accessible") {
		t.Fatalf("error: %q", errOut.String())
	}
}

// isLinux reports Linux the cheap way: abstract unix sockets need /proc
// backing and only exist on Linux, where /proc/self/exe is always present.
func isLinux() bool { return fileExists("/proc/self/exe") }

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}
