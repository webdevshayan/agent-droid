package tests_test

// End-to-end session flow (ported from the standalone CLI): every command
// is a fresh process — this test binary re-entering app.Run via TestMain —
// and the adapter daemon is a separate process serving the fake runtime
// over its abstract unix socket. No files, no device, no emulator: exactly
// the contract a real ATL/WineDroid adapter must satisfy.

import (
	"bytes"
	"image/png"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/webdevshayan/agent-droid/core/app"
)

// TestMain doubles as the agentdroid entry point: child processes re-exec
// this binary with AGENTDROID_TEST_APP=1 and argv passed straight to
// app.Run — byte-for-byte the code path `go install` builds.
func TestMain(m *testing.M) {
	if os.Getenv("AGENTDROID_TEST_APP") == "1" {
		os.Exit(app.Run(os.Args[1:], os.Stdout, os.Stderr, nil))
	}
	os.Exit(m.Run())
}

func runAppCLI(t *testing.T, args ...string) (string, string, int) {
	t.Helper()
	cmd := exec.Command(testBinary(t), args...)
	cmd.Env = append(os.Environ(), "AGENTDROID_TEST_APP=1")
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

func testBinary(t *testing.T) string {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatalf("resolve test binary: %v", err)
	}
	return exe
}

func newSessionName(t *testing.T) string {
	return "it-" + strings.ReplaceAll(t.Name(), "/", "-") + "-" + time.Now().Format("150405.000000000")
}

func makeSessionApk(t *testing.T) string {
	t.Helper()
	p := t.TempDir() + "/demo.apk"
	if err := os.WriteFile(p, []byte("PK\x03\x04 fake apk payload"), 0o644); err != nil {
		t.Fatalf("write apk: %v", err)
	}
	return p
}

func TestSessionLaunchSnapshotTapTypeScreenshotClose(t *testing.T) {
	session := newSessionName(t)
	apk := makeSessionApk(t)

	// launch: spawns the detached daemon, waits for the abstract socket,
	// performs the launch handshake.
	stdout, stderr, code := runAppCLI(t, "launch", apk, "--runtime", "fake-login", "--session", session)
	if code != 0 {
		t.Fatalf("launch exit %d\nstdout: %s\nstderr: %s", code, stdout, stderr)
	}
	if !strings.Contains(stdout, "launched demo.apk") || !strings.Contains(stdout, session) {
		t.Fatalf("launch output: %q", stdout)
	}

	// A second launch on the live session must be refused.
	_, stderr, code = runAppCLI(t, "launch", apk, "--runtime", "fake-login", "--session", session)
	if code != 1 || !strings.Contains(stderr, "already live") {
		t.Fatalf("duplicate launch: code=%d stderr=%q", code, stderr)
	}

	// snapshot: six bounded nodes of the fake login screen, refs assigned
	// by the shared snapshot pipeline (@e1 root … @e6 sign-in button).
	stdout, _, code = runAppCLI(t, "snapshot", "--session", session)
	if code != 0 {
		t.Fatalf("snapshot exit %d\nstdout: %s", code, stdout)
	}
	for _, want := range []string{"@e1", "@e6", `text="AgentDroid Demo"`, "demo:id/sign_in"} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("snapshot missing %q:\n%s", want, stdout)
		}
	}
	if n := strings.Count(stdout, "\n"); n != 6 {
		t.Fatalf("snapshot lines = %d, want 6:\n%s", n, stdout)
	}

	// The raw XML view is available through the same attach path.
	stdout, _, code = runAppCLI(t, "snapshot", "--session", session, "--xml")
	if code != 0 || !strings.Contains(stdout, "<hierarchy") || !strings.Contains(stdout, "demo:id/email") {
		t.Fatalf("snapshot --xml: code=%d\n%s", code, stdout)
	}

	// type: focus the email field by tap, then type (coordinate-less — the
	// daemon keeps the focus, exactly like a real device).
	stdout, stderr, code = runAppCLI(t, "tap", "@e3", "--session", session)
	if code != 0 || !strings.Contains(stdout, "tapped @e3 at (270,178)") {
		t.Fatalf("tap email: code=%d out=%q stderr=%q", code, stdout, stderr)
	}
	stdout, stderr, code = runAppCLI(t, "type", "shayan@example.com", "--session", session)
	if code != 0 || !strings.Contains(stdout, "typed 18 bytes") {
		t.Fatalf("type email: code=%d out=%q stderr=%q", code, stdout, stderr)
	}
	stdout, _, _ = runAppCLI(t, "snapshot", "--session", session)
	if !strings.Contains(stdout, `text="shayan@example.com"`) {
		t.Fatalf("typed text not in snapshot:\n%s", stdout)
	}

	// Password, then sign in.
	if _, _, code = runAppCLI(t, "tap", "@e4", "--session", session); code != 0 {
		t.Fatalf("tap password exit %d", code)
	}
	if _, _, code = runAppCLI(t, "type", "hunter2", "--session", session); code != 0 {
		t.Fatalf("type password exit %d", code)
	}
	_, _, code = runAppCLI(t, "tap", "@e6", "--session", session)
	if code != 0 {
		t.Fatalf("tap sign-in exit %d", code)
	}
	stdout, _, _ = runAppCLI(t, "snapshot", "--session", session)
	for _, want := range []string{"Signed in as shayan@example.com", `text="Sign Out"`} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("signed-in state missing %q:\n%s", want, stdout)
		}
	}

	// screenshot: a real PNG comes back on stdout.
	stdout, stderr, code = runAppCLI(t, "screenshot", "--session", session)
	if code != 0 {
		t.Fatalf("screenshot exit %d\nstderr: %s", code, stderr)
	}
	img, err := png.Decode(strings.NewReader(stdout))
	if err != nil {
		t.Fatalf("screenshot is not a PNG: %v (first bytes: %x)", err, stdout[:minInt(8, len(stdout))])
	}
	if img.Bounds().Dx() != 540 || img.Bounds().Dy() != 960 {
		t.Fatalf("screenshot size: %v", img.Bounds())
	}

	// close: the daemon goes away, socket included.
	stdout, _, code = runAppCLI(t, "close", "--session", session)
	if code != 0 || !strings.Contains(stdout, "closed") {
		t.Fatalf("close: code=%d out=%q", code, stdout)
	}

	// After close, commands fail with exit 1 and a hint to launch first.
	_, stderr, code = runAppCLI(t, "snapshot", "--session", session)
	if code != 1 || !strings.Contains(stderr, "no live session") {
		t.Fatalf("post-close snapshot: code=%d stderr=%q", code, stderr)
	}
}

func TestSessionUnknownRefFailsWithListing(t *testing.T) {
	session := newSessionName(t)
	apk := makeSessionApk(t)
	if _, _, code := runAppCLI(t, "launch", apk, "--runtime", "fake-login", "--session", session); code != 0 {
		t.Fatal("launch failed")
	}
	defer func() { runAppCLI(t, "close", "--session", session) }()
	_, stderr, code := runAppCLI(t, "tap", "@e42", "--session", session)
	if code != 1 || !strings.Contains(stderr, "last snapshot has 6 tappable element") {
		t.Fatalf("unknown ref: code=%d stderr=%q", code, stderr)
	}
}

func TestServeStdioMode(t *testing.T) {
	// serve --stdio speaks the session protocol on stdin/stdout directly:
	// this is how external harnesses embed an adapter without any socket.
	in := strings.NewReader(
		`{"id":1,"cmd":"launch","apk":"` + makeSessionApk(t) + `"}` + "\n" +
			`{"id":2,"cmd":"snapshot"}` + "\n" +
			`{"id":3,"cmd":"close"}` + "\n")
	var out bytes.Buffer
	cmd := exec.Command(testBinary(t), "serve", "--backend", "fake-login", "--stdio")
	cmd.Env = append(os.Environ(), "AGENTDROID_TEST_APP=1")
	cmd.Stdin = in
	cmd.Stdout = &out
	cmd.Stderr = os.Stderr
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

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
