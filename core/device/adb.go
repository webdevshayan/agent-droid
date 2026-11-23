// Package device wraps adb interactions.
//
// Every function validates its input before spawning a process, degrades to
// clear errors when adb is missing or no device is attached, and caps how
// much output it will read into memory. The Runner interface keeps adb out
// of the tests entirely.
package device

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	// MaxInputLen matches cli.MaxTextLen: oversized `type` payloads must be
	// rejected before they reach adb's device-side shell.
	MaxInputLen = 4096
	// MaxCoord bounds every coordinate sent to `input tap`/`input swipe`.
	MaxCoord = 100000
	// MaxOutputBytes caps how much stdout we buffer from one adb call.
	MaxOutputBytes = 64 << 20
	// MaxStderrBytes caps the error text we surface from one adb call.
	MaxStderrBytes = 4096

	adbTimeout = 60 * time.Second
)

// Runner spawns the adb binary and returns its stdout.
type Runner interface {
	Output(args ...string) ([]byte, error)
}

// ExecRunner runs a real adb binary. Timeout defaults to 60s when zero.
type ExecRunner struct {
	Bin     string
	Timeout time.Duration
}

// Detect finds adb on PATH, with an install hint when it is missing.
func Detect() (Runner, error) {
	bin, err := exec.LookPath("adb")
	if err != nil {
		return nil, errors.New("adb not found in PATH; install Android platform-tools (https://developer.android.com/tools/releases/platform-tools)")
	}
	return ExecRunner{Bin: bin}, nil
}

// Output runs one adb command with a timeout and bounded output buffers.
func (r ExecRunner) Output(args ...string) ([]byte, error) {
	timeout := r.Timeout
	if timeout == 0 {
		timeout = adbTimeout
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, r.Bin, args...)
	var stdout, stderr bytes.Buffer
	w := &limitedWriter{w: &stdout, remaining: MaxOutputBytes}
	cmd.Stdout = w
	cmd.Stderr = &stderr

	err := cmd.Run()
	if ctx.Err() == context.DeadlineExceeded {
		return nil, fmt.Errorf("adb timed out after %s", timeout)
	}
	if w.overflowed {
		return nil, fmt.Errorf("adb %s produced more than %d bytes of output; refusing to buffer", args[0], MaxOutputBytes)
	}
	if err != nil {
		msg := stderr.String()
		if len(msg) > MaxStderrBytes {
			// Truncate on a rune boundary: cutting mid-rune garbles the
			// tail of an otherwise UTF-8 device message.
			msg = strings.ToValidUTF8(msg[:MaxStderrBytes], "") + "…[truncated]"
		}
		msg = strings.TrimSpace(msg)
		if msg == "" {
			msg = err.Error()
		}
		return nil, fmt.Errorf("adb %s: %s", args[0], msg)
	}
	return stdout.Bytes(), nil
}

type limitedWriter struct {
	w          *bytes.Buffer
	remaining  int
	overflowed bool
}

func (l *limitedWriter) Write(p []byte) (int, error) {
	if l.remaining <= 0 {
		l.overflowed = true
		return len(p), nil // swallow the rest, never store it
	}
	if len(p) > l.remaining {
		l.w.Write(p[:l.remaining])
		l.remaining = 0
		l.overflowed = true
		return len(p), nil
	}
	l.w.Write(p)
	l.remaining -= len(p)
	return len(p), nil
}

// Client is one device-scoped handle over a Runner.
type Client struct {
	Serial string
	Adb    Runner
}

// New builds a Client. When adb is nil it is detected on PATH.
func New(serial string, adb Runner) (*Client, error) {
	if adb == nil {
		var err error
		adb, err = Detect()
		if err != nil {
			return nil, err
		}
	}
	return &Client{Serial: serial, Adb: adb}, nil
}

func (c *Client) args(cmd ...string) []string {
	if c.Serial == "" {
		return cmd
	}
	return append([]string{"-s", c.Serial}, cmd...)
}

// Devices lists attached devices. It is an error when none is attached.
func (c *Client) Devices() (string, error) {
	out, err := c.Adb.Output(c.args("devices", "-l")...)
	if err != nil {
		return "", err
	}
	listing := strings.TrimSpace(string(out))
	if !strings.Contains(listing, "\tdevice") {
		return "", fmt.Errorf("no devices attached:\n%s", listing)
	}
	return listing, nil
}

// Snapshot dumps the current screen as uiautomator XML. It prefers the
// one-shot `exec-out uiautomator dump /dev/tty` form and falls back to
// dump-to-file + cat for older devices. The fallback file is named per
// invocation (pid + timestamp) and removed after it is read: a fixed name
// on shared storage made concurrent agentdroid processes race on one file
// and left the dump behind forever.
func (c *Client) Snapshot() ([]byte, error) {
	var firstErr error
	out, err := c.Adb.Output(c.args("exec-out", "uiautomator", "dump", "/dev/tty")...)
	if err == nil && looksLikeXML(out) {
		return out, nil
	}
	if err != nil {
		firstErr = err
	}

	path := dumpPath()
	if _, err := c.Adb.Output(c.args("shell", "uiautomator", "dump", path)...); err != nil {
		if firstErr != nil {
			return nil, fmt.Errorf("uiautomator dump failed: %v; fallback dump also failed: %v", firstErr, err)
		}
		return nil, fmt.Errorf("uiautomator dump failed: %w", err)
	}
	out, err = c.Adb.Output(c.args("exec-out", "cat", path)...)
	// Best-effort cleanup: the device-side dump is scratch data, not state.
	// A failed rm must not mask a successful read.
	if _, rmErr := c.Adb.Output(c.args("shell", "rm", "-f", path)...); rmErr != nil && err == nil {
		err = fmt.Errorf("dump file %s could not be removed: %v", path, rmErr)
	}
	if err != nil {
		return nil, fmt.Errorf("reading UI dump failed: %w", err)
	}
	if !looksLikeXML(out) {
		return nil, errors.New("uiautomator produced no parsable XML (screen may be native chrome or the device is locked)")
	}
	return out, nil
}

// dumpPath is a per-invocation scratch path on shared storage: unique
// across concurrent agentdroid processes, removed after it is read.
func dumpPath() string {
	return fmt.Sprintf("/sdcard/agentdroid_dump_%d_%d.xml", os.Getpid(), time.Now().UnixNano())
}

func looksLikeXML(out []byte) bool {
	t := bytes.TrimSpace(out)
	return bytes.HasPrefix(t, []byte("<?xml")) || bytes.HasPrefix(t, []byte("<hierarchy"))
}

// Tap taps screen coordinates.
func (c *Client) Tap(x, y int) error {
	if err := checkCoord(x); err != nil {
		return err
	}
	if err := checkCoord(y); err != nil {
		return err
	}
	_, err := c.Adb.Output(c.args("shell", "input", "tap", strconv.Itoa(x), strconv.Itoa(y))...)
	return err
}

// Swipe drags from (x1,y1) to (x2,y2) over durationMs.
func (c *Client) Swipe(x1, y1, x2, y2, durationMs int) error {
	for _, n := range []int{x1, y1, x2, y2} {
		if err := checkCoord(n); err != nil {
			return err
		}
	}
	_, err := c.Adb.Output(c.args("shell", "input", "swipe",
		strconv.Itoa(x1), strconv.Itoa(y1), strconv.Itoa(x2), strconv.Itoa(y2),
		strconv.Itoa(durationMs))...)
	return err
}

// Type sends text to the focused element after ValidateText.
func (c *Client) Type(text string) error {
	escaped, err := ValidateText(text)
	if err != nil {
		return err
	}
	_, err = c.Adb.Output(c.args("shell", "input", "text", escaped)...)
	return err
}

// allowedPunctuation is the ASCII subset that survives adb's device-side
// shell untouched. Shell metacharacters (& ; | < > $ ` " ' \ and whitespace
// variants) are rejected instead of escaped — an allowlist is the only way
// to guarantee no injection into the device shell.
const allowedPunctuation = "-_.:,!?@#~^%*+=/"

// ValidateText checks a `type` payload and returns the adb-escaped form
// (spaces become %s). It rejects before allocating anything but the result.
func ValidateText(text string) (string, error) {
	if len(text) == 0 {
		return "", errors.New("type: text is empty")
	}
	if len(text) > MaxInputLen {
		return "", fmt.Errorf("type: text is %d bytes, over the %d-byte adb limit", len(text), MaxInputLen)
	}
	var b strings.Builder
	b.Grow(len(text) + 8)
	for _, r := range text {
		switch {
		case r == ' ':
			b.WriteString("%s")
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9',
			strings.ContainsRune(allowedPunctuation, r):
			b.WriteRune(r)
		case r < 0x20 || r == 0x7f:
			return "", fmt.Errorf("type: control character U+%04X is not supported by adb input text", r)
		default:
			return "", fmt.Errorf("type: unsupported character %q: adb input text cannot carry shell metacharacters or non-ASCII; allowed: [a-zA-Z0-9%s]", r, allowedPunctuation)
		}
	}
	return b.String(), nil
}

// Install pushes and installs an APK, validating the path before adb runs.
func (c *Client) Install(path string) error {
	if !strings.EqualFold(filepath.Ext(path), ".apk") {
		return fmt.Errorf("install: %q does not look like an APK (.apk)", path)
	}
	st, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("install: APK not found: %s", path)
		}
		return fmt.Errorf("install: %w", err)
	}
	if st.IsDir() {
		return fmt.Errorf("install: %s is a directory, want an .apk file", path)
	}
	if st.Size() == 0 {
		return errors.New("install: APK file is empty (0 bytes)")
	}
	if err := checkAPKMagic(path); err != nil {
		return err
	}
	_, err = c.Adb.Output(c.args("install", "-r", path)...)
	return err
}

// checkAPKMagic verifies the ZIP "PK" signature locally. APKs are ZIP
// archives and start with PK\x03\x04 (or PK\x05\x06 for an empty archive):
// a text file named .apk used to ship to adb and fail only device-side,
// far from the actual mistake.
func checkAPKMagic(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("install: %w", err)
	}
	defer f.Close()
	head := make([]byte, 4)
	if _, err := io.ReadFull(f, head); err != nil {
		return fmt.Errorf("install: cannot read %s: %w", path, err)
	}
	if !bytes.HasPrefix(head, []byte("PK\x03\x04")) && !bytes.HasPrefix(head, []byte("PK\x05\x06")) {
		return fmt.Errorf("install: %s is not a ZIP archive (missing the PK header) — not an installable APK", path)
	}
	return nil
}

// ScreenBytes captures the current screen as raw PNG bytes.
func (c *Client) ScreenBytes() ([]byte, error) {
	out, err := c.Adb.Output(c.args("exec-out", "screencap", "-p")...)
	if err != nil {
		return nil, err
	}
	if len(out) == 0 {
		return nil, errors.New("screenshot: screencap returned no data")
	}
	return out, nil
}

// KeyBack / KeyHome send the system navigation keyevents.
func (c *Client) KeyBack() error { return c.keyevent(4) }

func (c *Client) KeyHome() error { return c.keyevent(3) }

// Key sends an arbitrary keycode.
func (c *Client) Key(code int) error { return c.keyevent(code) }

func (c *Client) keyevent(code int) error {
	_, err := c.Adb.Output(c.args("shell", "input", "keyevent", strconv.Itoa(code))...)
	return err
}

func checkCoord(n int) error {
	if n < 0 || n > MaxCoord {
		return fmt.Errorf("coordinate %d out of range 0..%d", n, MaxCoord)
	}
	return nil
}
