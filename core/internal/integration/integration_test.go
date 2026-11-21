// Package integration runs the real CLI end to end: every command is a fresh
// agentdroid process (the test binary re-entering main via TestMain), the
// adapter daemon is a separate process serving the fake runtime over its
// abstract unix socket. No files, no device, no emulator — exactly the
// contract the orchestrator must keep with a real ATL/WineDroid adapter.
package integration

import (
	"bytes"
	"image/png"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/webdevshayan/agent-droid/core/internal/cli"
	"github.com/webdevshayan/agent-droid/core/internal/runtime"
)

// TestMain doubles as the agentdroid entry point: child processes re-exec
// this binary with AGENTDROID_TEST_SERVE=1 and argv passed straight to the
// CLI dispatcher, which is byte-for-byte the code path `go install` builds.
func TestMain(m *testing.M) {
	if os.Getenv("AGENTDROID_TEST_SERVE") == "1" {
		os.Exit(cli.Execute(os.Args[1:], os.Stdout, os.Stderr))
	}
	os.Exit(m.Run())
}

func helperBinary(t *testing.T) string {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatalf("resolve test binary: %v", err)
	}
	return exe
}

// runCLI executes one agentdroid command as its own process.
func runCLI(t *testing.T, args ...string) (string, string, int) {
	t.Helper()
	cmd := exec.Command(helperBinary(t), args...)
	cmd.Env = append(os.Environ(), "AGENTDROID_TEST_SERVE=1")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	code := 0
	if err != nil {
		code = 1
		if ee, ok := err.(*exec.ExitError); ok {
			code = ee.ExitCode()
		}
	}
	return stdout.String(), stderr.String(), code
}

func newSession(t *testing.T) string {
	return "it-" + t.Name() + "-" + time.Now().Format("150405.000000000")
}

func makeApk(t *testing.T) string {
	t.Helper()
	p := t.TempDir() + "/demo.apk"
	if err := os.WriteFile(p, []byte("PK\x03\x04 fake apk payload"), 0o644); err != nil {
		t.Fatalf("write apk: %v", err)
	}
	return p
}

func socketAlive(session string) bool {
	c, err := runtime.DialSession(session, 100*time.Millisecond)
	if err != nil {
		return false
	}
	c.Close()
	return true
}

func TestLaunchSnapshotTapTypeScreenshotClose(t *testing.T) {
	session := newSession(t)
	apk := makeApk(t)

	// launch: spawns the adapter daemon, waits for the abstract socket,
	// performs the handshake.
	stdout, stderr, code := runCLI(t, "launch", apk, "--runtime", "fake", "--session", session, "--timeout", "20s")
	if code != 0 {
		t.Fatalf("launch exit %d\nstdout: %s\nstderr: %s", code, stdout, stderr)
	}
	if !strings.Contains(stdout, "launched demo.apk") || !strings.Contains(stdout, session) {
		t.Fatalf("launch output: %q", stdout)
	}
	if !socketAlive(session) {
		t.Fatal("session socket not alive after launch")
	}

	// A second launch on the live session must be refused.
	_, stderr, code = runCLI(t, "launch", apk, "--runtime", "fake", "--session", session)
	if code != 1 || !strings.Contains(stderr, "already live") {
		t.Fatalf("duplicate launch: code=%d stderr=%q", code, stderr)
	}

	// snapshot: five elements of the fake login app.
	stdout, _, code = runCLI(t, "snapshot", "--session", session)
	if code != 0 {
		t.Fatalf("snapshot exit %d\nstdout: %s", code, stdout)
	}
	for _, want := range []string{"@e1", "@e5", `text="Sign In"`, "Ready.", "# 5 elements"} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("snapshot missing %q:\n%s", want, stdout)
		}
	}
	assertRefOfLine(t, stdout, `text="Sign In"`, "@e5")

	// tree and json views.
	stdout, _, code = runCLI(t, "snapshot", "--session", session, "--tree")
	if code != 0 || !strings.Contains(stdout, "FrameLayout") {
		t.Fatalf("tree view: code=%d\n%s", code, stdout)
	}
	stdout, _, code = runCLI(t, "snapshot", "--session", session, "--json")
	if code != 0 || !strings.Contains(stdout, `"demo:id/sign_in"`) {
		t.Fatalf("json view: code=%d\n%s", code, stdout)
	}

	// type: the CLI resolves @e2 against the live screen and sends the tap
	// coordinates plus text.
	stdout, stderr, code = runCLI(t, "type", "@e2", "shayan@example.com", "--session", session)
	if code != 0 {
		t.Fatalf("type exit %d\nstdout: %s\nstderr: %s", code, stdout, stderr)
	}
	if !strings.Contains(stdout, "typed \"shayan@example.com\" into @e2") {
		t.Fatalf("type output: %q", stdout)
	}
	stdout, _, _ = runCLI(t, "snapshot", "--session", session)
	if !strings.Contains(stdout, `text="shayan@example.com"`) {
		t.Fatalf("typed text not in snapshot:\n%s", stdout)
	}

	// tap: sign in with a password typed first.
	stdout, _, code = runCLI(t, "tap", "@e3", "--session", session)
	if code != 0 || !strings.Contains(stdout, "tapped @e3 at (270,248)") {
		t.Fatalf("tap @e3: code=%d out=%q", code, stdout)
	}
	stdout, _, _ = runCLI(t, "type", "@e3", "hunter2", "--session", session)
	if _, _, code := runCLI(t, "tap", "@e5", "--session", session); code != 0 {
		t.Fatalf("tap sign-in exit %d", code)
	}
	stdout, _, _ = runCLI(t, "snapshot", "--session", session)
	for _, want := range []string{"Signed in as shayan@example.com", "Sign Out"} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("signed-in state missing %q:\n%s", want, stdout)
		}
	}

	// screenshot: a real PNG comes back on stdout.
	var pngBuf bytes.Buffer
	cmd := exec.Command(helperBinary(t), "screenshot", "--session", session)
	cmd.Env = append(os.Environ(), "AGENTDROID_TEST_SERVE=1")
	cmd.Stdout = &pngBuf
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("screenshot: %v", err)
	}
	img, err := png.Decode(bytes.NewReader(pngBuf.Bytes()))
	if err != nil {
		t.Fatalf("screenshot is not a PNG: %v (first bytes: %x)", err, pngBuf.Bytes()[:min(8, pngBuf.Len())])
	}
	if got := img.Bounds(); got.Dx() != 540 || got.Dy() != 960 {
		t.Fatalf("screenshot size: %v", got)
	}

	// close: the daemon must go away, socket included.
	stdout, _, code = runCLI(t, "close", "--session", session)
	if code != 0 || !strings.Contains(stdout, "closed") {
		t.Fatalf("close: code=%d out=%q", code, stdout)
	}
	deadline := time.Now().Add(5 * time.Second)
	for socketAlive(session) {
		if time.Now().After(deadline) {
			t.Fatal("session socket still alive after close")
		}
		time.Sleep(50 * time.Millisecond)
	}

	// After close, commands fail with exit 1 and a hint to launch first.
	_, stderr, code = runCLI(t, "snapshot", "--session", session)
	if code != 1 || !strings.Contains(stderr, "no live session") {
		t.Fatalf("post-close snapshot: code=%d stderr=%q", code, stderr)
	}
}

func TestUnknownRefFailsWithListing(t *testing.T) {
	session := newSession(t)
	apk := makeApk(t)
	if _, _, code := runCLI(t, "launch", apk, "--runtime", "fake", "--session", session); code != 0 {
		t.Fatal("launch failed")
	}
	defer func() {
		runCLI(t, "close", "--session", session)
	}()
	_, stderr, code := runCLI(t, "tap", "@e42", "--session", session)
	if code != 1 || !strings.Contains(stderr, "current screen has 5 elements") {
		t.Fatalf("unknown ref: code=%d stderr=%q", code, stderr)
	}
}

func TestServeStdioMode(t *testing.T) {
	// serve --stdio speaks the protocol on stdin/stdout directly: this is
	// how external harnesses embed an adapter without any socket.
	in := strings.NewReader(
		`{"id":1,"cmd":"launch","apk":"` + makeApk(t) + `"}` + "\n" +
			`{"id":2,"cmd":"snapshot"}` + "\n" +
			`{"id":3,"cmd":"close"}` + "\n")
	var out bytes.Buffer
	cmd := exec.Command(helperBinary(t), "serve", "--backend", "fake", "--stdio")
	cmd.Env = append(os.Environ(), "AGENTDROID_TEST_SERVE=1")
	cmd.Stdin = in
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		t.Fatalf("serve stdio: %v\nout: %s", err, out.String())
	}
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 3 {
		t.Fatalf("expected 3 responses, got %d:\n%s", len(lines), out.String())
	}
	if !strings.Contains(lines[1], `"id":2`) || !strings.Contains(lines[1], "Sign In") {
		t.Fatalf("snapshot response over stdio: %s", lines[1])
	}
}

// assertRefOfLine checks that the snapshot line carrying marker starts with
// the expected ref — the reproducibility guarantee refs make to agents.
func assertRefOfLine(t *testing.T, snapshot, marker, ref string) {
	t.Helper()
	for _, line := range strings.Split(snapshot, "\n") {
		if strings.Contains(line, marker) {
			if !strings.HasPrefix(line, ref+"  ") {
				t.Fatalf("line for %s starts with %q, want ref %s:\n%s", marker, line[:min(30, len(line))], ref, snapshot)
			}
			return
		}
	}
	t.Fatalf("no line carries %s\n%s", marker, snapshot)
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
