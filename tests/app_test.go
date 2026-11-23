package tests_test

// End-to-end tests through app.Run — the same dispatch a real invocation
// takes. Two layers are covered here:
//
//  1. a scripted driver (records adb-level calls, asserts dispatch wiring
//     and the graceful-failure contract: bad refs, missing files and bad
//     text produce clear errors and documented exit codes, never panics)
//  2. the REAL fake runtime — the full agent loop with no device at all:
//     launch → snapshot → tap → type → snapshot → close.

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/webdevshayan/agent-droid/core/app"
	"github.com/webdevshayan/agent-droid/core/driver"
	"github.com/webdevshayan/agent-droid/core/snapshot"
)

const screenXML = `<?xml version='1.0' encoding='UTF-8'?>
<hierarchy rotation="0">
  <node class="android.widget.FrameLayout" bounds="[0,0][1080,2400]">
    <node class="android.widget.TextView" text="Settings" resource-id="com.example:id/title" bounds="[54,100][1026,180]"/>
    <node class="android.widget.Button" text="OK" clickable="true" bounds="[340,1200][740,1400]"/>
  </node>
</hierarchy>`

// scriptedDriver implements driver.Driver with canned responses and full
// call recording. errOn/errMsg make a specific method kind fail.
type scriptedDriver struct {
	dump       []byte
	dumpErr    error
	screenshot []byte
	shotErr    error
	listing    string
	errOn      string
	errMsg     string
	calls      [][]string
}

func (s *scriptedDriver) record(kind string, args ...string) error {
	s.calls = append(s.calls, append([]string{kind}, args...))
	if s.errOn == kind {
		return errors.New(s.errMsg)
	}
	return nil
}

func (s *scriptedDriver) Launch(apk string) error  { return s.record("launch", apk) }
func (s *scriptedDriver) Close() error             { return s.record("close") }
func (s *scriptedDriver) Devices() (string, error) { return s.listing, s.record("devices") }
func (s *scriptedDriver) Tap(x, y int) error {
	return s.record("tap", strconv.Itoa(x), strconv.Itoa(y))
}
func (s *scriptedDriver) Type(text string) error { return s.record("type", text) }
func (s *scriptedDriver) Key(code int) error     { return s.record("key", strconv.Itoa(code)) }
func (s *scriptedDriver) Swipe(x1, y1, x2, y2, ms int) error {
	return s.record("swipe", strconv.Itoa(x1), strconv.Itoa(y1), strconv.Itoa(x2), strconv.Itoa(y2), strconv.Itoa(ms))
}
func (s *scriptedDriver) Dump() ([]byte, error) {
	s.calls = append(s.calls, []string{"dump"})
	if s.dumpErr != nil {
		return nil, s.dumpErr
	}
	return s.dump, nil
}
func (s *scriptedDriver) Screenshot() ([]byte, error) {
	s.calls = append(s.calls, []string{"screenshot"})
	if s.shotErr != nil {
		return nil, s.shotErr
	}
	return s.screenshot, nil
}

func scripted() *scriptedDriver {
	return &scriptedDriver{
		dump:       []byte(screenXML),
		screenshot: []byte{0x89, 'P', 'N', 'G', 1, 2, 3},
		listing:    "List of devices attached\nEMU1\tdevice product:sdk\n",
	}
}

func runApp(t *testing.T, argv []string, drv driver.Driver) (int, string, string) {
	t.Helper()
	var out, errOut bytes.Buffer
	code := app.Run(argv, &out, &errOut, drv)
	return code, out.String(), errOut.String()
}

func TestAppSnapshotFormatsRefs(t *testing.T) {
	f := scripted()
	code, out, errOut := runApp(t, []string{"snapshot"}, f)
	if code != 0 {
		t.Fatalf("exit=%d stderr=%q", code, errOut)
	}
	for _, want := range []string{"@e1", "@e2", "@e3", `text="Settings"`, "clickable", "center=(540,1300)"} {
		if !strings.Contains(out, want) {
			t.Fatalf("snapshot output missing %q:\n%s", want, out)
		}
	}
}

func TestAppSnapshotXMLMode(t *testing.T) {
	f := scripted()
	code, out, _ := runApp(t, []string{"snapshot", "--xml"}, f)
	if code != 0 || !strings.Contains(out, "<hierarchy") || !strings.Contains(out, `[54,100][1026,180]`) {
		t.Fatalf("snapshot --xml must print the raw dump, exit=%d out=%q", code, out)
	}
}

func TestAppTapResolvesRef(t *testing.T) {
	f := scripted()
	code, out, _ := runApp(t, []string{"tap", "@e3"}, f)
	if code != 0 || !strings.Contains(out, "tapped @e3 at (540,1300)") {
		t.Fatalf("exit=%d out=%q", code, out)
	}
	want := []string{"tap", "540", "1300"}
	if !reflect.DeepEqual(f.calls[len(f.calls)-1], want) {
		t.Fatalf("last driver call = %v, want %v", f.calls[len(f.calls)-1], want)
	}
}

func TestAppTapUnknownRef(t *testing.T) {
	f := scripted()
	code, _, errOut := runApp(t, []string{"tap", "@e9"}, f)
	if code != 1 || !strings.Contains(errOut, "not found") || !strings.Contains(errOut, "3 tappable") {
		t.Fatalf("exit=%d stderr=%q — must say the ref was not found and how many refs exist", code, errOut)
	}
}

func TestAppTypeFlow(t *testing.T) {
	f := scripted()
	code, out, _ := runApp(t, []string{"type", "hi there"}, f)
	if code != 0 || !strings.Contains(out, "typed") {
		t.Fatalf("exit=%d out=%q", code, out)
	}
	want := []string{"type", "hi there"}
	if !reflect.DeepEqual(f.calls[len(f.calls)-1], want) {
		t.Fatalf("last driver call = %v, want %v", f.calls[len(f.calls)-1], want)
	}
}

func TestAppSwipeByCoordsAndRefs(t *testing.T) {
	f := scripted()
	if code, _, _ := runApp(t, []string{"swipe", "0", "0", "1080", "2400"}, f); code != 0 {
		t.Fatal("coordinate swipe must succeed")
	}
	if code, _, _ := runApp(t, []string{"swipe", "@e1", "@e3"}, f); code != 0 {
		t.Fatal("ref swipe must succeed")
	}
	want := []string{"swipe", "540", "1200", "540", "1300", "300"}
	if !reflect.DeepEqual(f.calls[len(f.calls)-1], want) {
		t.Fatalf("ref swipe driver call = %v, want %v", f.calls[len(f.calls)-1], want)
	}
}

func TestAppLaunchAndClose(t *testing.T) {
	f := scripted()
	if code, out, _ := runApp(t, []string{"launch", "demo.apk"}, f); code != 0 || !strings.Contains(out, "launched demo.apk") {
		t.Fatal("launch exit/out wrong")
	}
	if code, out, _ := runApp(t, []string{"close"}, f); code != 0 || !strings.Contains(out, "closed") {
		t.Fatal("close exit/out wrong")
	}
}

func TestAppScreenshotOutAndStdout(t *testing.T) {
	f := scripted()
	path := filepath.Join(t.TempDir(), "shot.png")
	code, out, _ := runApp(t, []string{"screenshot", "--out", path}, f)
	if code != 0 || !strings.Contains(out, "saved") {
		t.Fatalf("exit=%d out=%q", code, out)
	}
	st, err := os.Stat(path)
	if err != nil || st.Size() == 0 {
		t.Fatalf("screenshot file missing/empty: %v", err)
	}

	// No --out: raw PNG on stdout, so `agentdroid screenshot > screen.png` works.
	f2 := scripted()
	code, raw, _ := runApp(t, []string{"screenshot"}, f2)
	if code != 0 || len(raw) != len(f2.screenshot) {
		t.Fatalf("stdout screenshot exit=%d len=%d", code, len(raw))
	}

	// Empty driver payload must be a clean error.
	f3 := scripted()
	f3.screenshot = nil
	code, _, errOut := runApp(t, []string{"screenshot", "--out", filepath.Join(t.TempDir(), "x.png")}, f3)
	if code != 1 || !strings.Contains(errOut, "no data") {
		t.Fatalf("empty screenshot exit=%d stderr=%q", code, errOut)
	}
}

func TestAppScreenshotHumanSizes(t *testing.T) {
	for _, tc := range []struct {
		size int
		want string
	}{
		{2048, "2.0 KiB"},
		{(3 << 20) / 2, "1.5 MiB"},
		{7, "7 B"},
	} {
		f := scripted()
		f.screenshot = make([]byte, tc.size)
		code, out, _ := runApp(t, []string{"screenshot", "--out", filepath.Join(t.TempDir(), "x.png")}, f)
		if code != 0 || !strings.Contains(out, tc.want) {
			t.Fatalf("size %d: exit=%d out=%q want %q", tc.size, code, out, tc.want)
		}
	}
}

func TestAppScreenshotUnwritablePath(t *testing.T) {
	f := scripted()
	dir := t.TempDir()
	blocker := filepath.Join(dir, "blocker")
	if err := os.WriteFile(blocker, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	code, _, errOut := runApp(t, []string{"screenshot", "--out", filepath.Join(blocker, "x.png")}, f)
	if code != 1 || !strings.Contains(errOut, "screenshot:") {
		t.Fatalf("unwritable path exit=%d stderr=%q", code, errOut)
	}
}

func TestAppClearSweep(t *testing.T) {
	f := scripted()
	code, out, _ := runApp(t, []string{"clear", "@e1"}, f)
	if code != 0 || !strings.Contains(out, "cleared @e1") {
		t.Fatalf("exit=%d out=%q", code, out)
	}
	keys := 0
	for _, c := range f.calls {
		if c[0] == "key" {
			keys++
		}
	}
	if keys != 41 { // 1 MOVE_END + exactly the bounded DEL sweep
		t.Fatalf("clear made %d key calls, want 41", keys)
	}
	if f.calls[1][0] != "tap" {
		t.Fatalf("clear must tap the element first, calls: %v", f.calls[1])
	}
}

func TestAppClearSurfacesKeyError(t *testing.T) {
	f := scripted()
	f.errOn, f.errMsg = "key", "keyevent failed"
	code, _, errOut := runApp(t, []string{"clear", "@e1"}, f)
	if code != 1 || !strings.Contains(errOut, "keyevent failed") {
		t.Fatalf("clear exit=%d stderr=%q", code, errOut)
	}
}

func TestAppDriverFailurePaths(t *testing.T) {
	f := scripted()
	f.dumpErr = errors.New("device offline")
	code, _, errOut := runApp(t, []string{"snapshot"}, f)
	if code != 1 || !strings.Contains(errOut, "device offline") {
		t.Fatalf("snapshot exit=%d stderr=%q", code, errOut)
	}
	code, _, _ = runApp(t, []string{"tap", "@e1"}, f)
	if code != 1 {
		t.Fatalf("tap exit=%d", code)
	}
	f2 := scripted()
	f2.errOn, f2.errMsg = "devices", "device offline"
	code, _, errOut = runApp(t, []string{"devices"}, f2)
	if code != 1 || !strings.Contains(errOut, "device offline") {
		t.Fatalf("devices exit=%d stderr=%q", code, errOut)
	}
}

func TestAppSnapshotGarbageIsRuntimeError(t *testing.T) {
	f := scripted()
	f.dump = []byte("this is not xml at all")
	code, _, errOut := runApp(t, []string{"snapshot"}, f)
	if code != 1 || !strings.Contains(errOut, "not XML") {
		t.Fatalf("garbage dump exit=%d stderr=%q", code, errOut)
	}
	ft := scripted()
	ft.dump = []byte("<?xml version=\"1.0\"?><hierarchy><node bounds=\"[0,0][1,1]\"")
	code, _, errOut = runApp(t, []string{"snapshot"}, ft)
	if code != 1 || !strings.Contains(errOut, "invalid XML") {
		t.Fatalf("truncated dump exit=%d stderr=%q", code, errOut)
	}
}

func TestAppNoAdbGraceful(t *testing.T) {
	t.Setenv("PATH", "/nonexistent")
	for _, argv := range [][]string{
		{"tap", "@e1"}, {"snapshot"}, {"devices"}, {"type", "hi"}, {"back"}, {"close"},
	} {
		code, _, errOut := runApp(t, argv, nil)
		if code != 1 || !strings.Contains(errOut, "adb not found") {
			t.Fatalf("argv=%q exit=%d stderr=%q — missing adb must degrade to a clean error", argv, code, errOut)
		}
	}
}

func TestAppUsageErrorsAreExit2(t *testing.T) {
	f := scripted()
	for _, argv := range [][]string{
		{}, {"frobnicate"}, {"tap"}, {"tap", "@bogus"}, {"swipe", "1", "2"},
		{"type", ""}, {"install", "", "extra"}, {"--serial", "x", "help"},
		{"--runtime", "wine", "snapshot"},
	} {
		code, _, errOut := runApp(t, argv, f)
		if code != 2 {
			t.Fatalf("argv=%q exit=%d, want 2 (usage error)", argv, code)
		}
		if !strings.HasPrefix(errOut, "agentdroid: ") {
			t.Fatalf("usage error must be prefixed, got %q", errOut)
		}
	}
}

func TestAppRuntimeErrorsAreExit1(t *testing.T) {
	// Ref out of range at resolve time.
	f := scripted()
	for _, argv := range [][]string{{"tap", "@e9"}, {"swipe", "@e1", "@e9"}, {"clear", "@e9"}} {
		code, _, _ := runApp(t, argv, f)
		if code != 1 {
			t.Fatalf("argv=%q exit=%d, want 1 (runtime error)", argv, code)
		}
	}
	// Driver-level failures surface as exit 1 too.
	f2 := scripted()
	f2.errOn, f2.errMsg = "launch", "APK not found"
	if code, _, _ := runApp(t, []string{"launch", "x.apk"}, f2); code != 1 {
		t.Fatal("failing launch must exit 1")
	}
	f3 := scripted()
	f3.errOn, f3.errMsg = "key", "keyevent failed"
	if code, _, _ := runApp(t, []string{"back"}, f3); code != 1 {
		t.Fatal("failing back must exit 1")
	}
}

func TestAppHelpVersion(t *testing.T) {
	code, out, _ := runApp(t, []string{"--help"}, scripted())
	if code != 0 || !strings.Contains(out, "Agent loop") || !strings.Contains(out, "--runtime fake") {
		t.Fatalf("help exit=%d out=%q", code, out)
	}
	code, out, _ = runApp(t, []string{"--version"}, nil)
	if code != 0 || !strings.Contains(out, "agentdroid 0.1.0") {
		t.Fatalf("version exit=%d out=%q", code, out)
	}
	code, out, _ = runApp(t, []string{"help"}, nil)
	if code != 0 || !strings.Contains(out, "snapshot") {
		t.Fatalf("help verb exit=%d out=%q", code, out)
	}
}

func TestAppBackHome(t *testing.T) {
	f := scripted()
	if code, _, _ := runApp(t, []string{"back"}, f); code != 0 {
		t.Fatal("back must succeed")
	}
	if code, _, _ := runApp(t, []string{"home"}, f); code != 0 {
		t.Fatal("home must succeed")
	}
	if !reflect.DeepEqual(f.calls[len(f.calls)-1], []string{"key", "3"}) {
		t.Fatalf("last call = %v, want HOME key", f.calls[len(f.calls)-1])
	}
}

// --- The REAL fake runtime: the documented 30-second flow, no device. ---

func fakeEnv(t *testing.T) {
	t.Helper()
	t.Setenv("AGENTDROID_FAKE_STATE", filepath.Join(t.TempDir(), "state.json"))
}

// demoAPK writes a tiny placeholder APK. The fake runtime stats the path at
// launch (the same accessibility rule the session runtimes enforce), so the
// tests must hand it a file that really exists.
func demoAPK(t *testing.T) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "demo.apk")
	if err := os.WriteFile(p, []byte("PK\x03\x04 placeholder payload"), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestFakeRuntimeAgentLoop(t *testing.T) {
	fakeEnv(t)
	fa := []string{"--runtime", "fake"}

	// launch → snapshot shows the main screen
	apk := demoAPK(t)
	if code, out, errOut := runApp(t, append(fa, "launch", apk), nil); code != 0 || !strings.Contains(out, "launched") || !strings.Contains(out, "demo.apk") {
		t.Fatalf("launch exit=%d out=%q stderr=%q", code, out, errOut)
	}
	code, out, errOut := runApp(t, append(fa, "snapshot"), nil)
	if code != 0 {
		t.Fatalf("snapshot exit=%d stderr=%q", code, errOut)
	}
	for _, want := range []string{"@e1", "@e2", "@e3", "@e4", "Demo App", "Sign in", "About"} {
		if !strings.Contains(out, want) {
			t.Fatalf("main screen missing %q:\n%s", want, out)
		}
	}

	// tap the username field, type, verify the text round-trips
	if code, _, errOut := runApp(t, append(fa, "tap", "@e2"), nil); code != 0 {
		t.Fatalf("tap field exit=%d stderr=%q", code, errOut)
	}
	if code, _, errOut := runApp(t, append(fa, "type", "demo@gmail.com"), nil); code != 0 {
		t.Fatalf("type exit=%d stderr=%q", code, errOut)
	}
	if _, out, _ = runApp(t, append(fa, "snapshot"), nil); !strings.Contains(out, `text="demo@gmail.com"`) {
		t.Fatalf("typed text must appear in the dump:\n%s", out)
	}

	// Sign in (@e3) → welcome screen shows the typed value
	if code, _, errOut := runApp(t, append(fa, "tap", "@e3"), nil); code != 0 {
		t.Fatalf("tap sign-in exit=%d stderr=%q", code, errOut)
	}
	if _, out, _ = runApp(t, append(fa, "snapshot"), nil); !strings.Contains(out, "Welcome, demo@gmail.com") {
		t.Fatalf("welcome screen wrong:\n%s", out)
	}

	// back → main; close ends the session; later commands fail cleanly
	if code, _, errOut := runApp(t, append(fa, "back"), nil); code != 0 {
		t.Fatalf("back exit=%d stderr=%q", code, errOut)
	}
	if code, out, _ := runApp(t, append(fa, "close"), nil); code != 0 || !strings.Contains(out, "closed") {
		t.Fatal("close wrong")
	}
	code, _, errOut = runApp(t, append(fa, "snapshot"), nil)
	if code != 1 || !strings.Contains(errOut, "no fake app running") {
		t.Fatalf("snapshot after close exit=%d stderr=%q", code, errOut)
	}
}

func TestFakeRuntimeValidation(t *testing.T) {
	fakeEnv(t)
	fa := []string{"--runtime", "fake"}

	// launch a non-APK
	if code, _, errOut := runApp(t, append(fa, "launch", "demo.exe"), nil); code != 1 || !strings.Contains(errOut, "does not look like an APK") {
		t.Fatalf("launch non-apk exit=%d stderr=%q", code, errOut)
	}
	// commands without a running session fail cleanly
	for _, argv := range [][]string{
		{"--runtime", "fake", "type", "hi"},
		{"--runtime", "fake", "close"},
		{"--runtime", "fake", "tap", "@e1"},
		{"--runtime", "fake", "back"},
		{"--runtime", "fake", "screenshot"},
	} {
		if code, _, errOut := runApp(t, argv, nil); code != 1 || !strings.Contains(errOut, "no fake app running") {
			t.Fatalf("argv=%q exit=%d stderr=%q", argv, code, errOut)
		}
	}
	// devices is a usage error under fake (parser-level)
	if code, _, errOut := runApp(t, []string{"--runtime", "fake", "devices"}, nil); code != 2 {
		t.Fatalf("fake devices exit=%d stderr=%q", code, errOut)
	}
}

func TestFakeRuntimeTypeWithoutFocus(t *testing.T) {
	fakeEnv(t)
	fa := []string{"--runtime", "fake"}
	runApp(t, append(fa, "launch", demoAPK(t)), nil)
	if code, _, errOut := runApp(t, append(fa, "type", "hi"), nil); code != 1 || !strings.Contains(errOut, "no focused text field") {
		t.Fatalf("type without focus exit=%d stderr=%q", code, errOut)
	}
}

func TestFakeRuntimeScreenshotPNG(t *testing.T) {
	fakeEnv(t)
	fa := []string{"--runtime", "fake"}
	runApp(t, append(fa, "launch", demoAPK(t)), nil)
	code, raw, _ := runApp(t, append(fa, "screenshot"), nil)
	if code != 0 || len(raw) < 8 || raw[0] != 0x89 || raw[1] != 'P' || raw[2] != 'N' || raw[3] != 'G' {
		t.Fatalf("fake screenshot must be PNG on stdout, exit=%d len=%d", code, len(raw))
	}
	path := filepath.Join(t.TempDir(), "x.png")
	if code, _, _ = runApp(t, append(fa, "screenshot", "--out", path), nil); code != 0 {
		t.Fatal("fake screenshot --out failed")
	}
	st, err := os.Stat(path)
	if err != nil || st.Size() == 0 {
		t.Fatalf("fake screenshot file missing: %v", err)
	}
}

func TestFakeRuntimeDumpIsParseable(t *testing.T) {
	// The fake driver must emit exactly the XML dialect the snapshot
	// parser consumes — that is the whole point of the common protocol.
	fakeEnv(t)
	f := driver.OpenFake()
	if err := f.Launch(demoAPK(t)); err != nil {
		t.Fatal(err)
	}
	raw, err := f.Dump()
	if err != nil {
		t.Fatal(err)
	}
	els, err := snapshot.Parse(raw)
	if err != nil || len(els) != 4 {
		t.Fatalf("fake dump must parse into 4 elements, got %d, %v", len(els), err)
	}
	el, err := snapshot.Resolve(els, "@e3")
	if err != nil || el.Text != "Sign in" {
		t.Fatalf("@e3 = %+v, %v", el, err)
	}
}

func TestFakeRuntimeSwipeHonestyNote(t *testing.T) {
	// AGD-14: the fake ignores swipes; the CLI must say so on stderr
	// instead of leaving a bare success line that implies movement.
	fakeEnv(t)
	fa := []string{"--runtime", "fake"}
	runApp(t, append(fa, "launch", demoAPK(t)), nil)
	code, out, errOut := runApp(t, append(fa, "swipe", "@e1", "@e3"), nil)
	if code != 0 || !strings.Contains(out, "swiped @e1") {
		t.Fatalf("swipe exit=%d out=%q", code, out)
	}
	if !strings.Contains(errOut, "ignored by the fake runtime") {
		t.Fatalf("swipe must print the honesty note on stderr, got %q", errOut)
	}
}
