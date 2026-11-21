package tests_test

// The fake driver in isolation: the three-screen state machine, its
// persistence across processes, and the never-panic error contract.

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/webdevshayan/agent-droid/core/driver"
	"github.com/webdevshayan/agent-droid/core/snapshot"
)

func TestFakeDriverStateMachine(t *testing.T) {
	fakeEnv(t)
	f := driver.OpenFake()

	// Without a session, every method degrades to the same clean error.
	if _, err := f.Dump(); err == nil || !strings.Contains(err.Error(), "no fake app running") {
		t.Fatalf("Dump without session = %v", err)
	}
	if err := f.Tap(540, 260); err == nil {
		t.Fatal("Tap without session must fail")
	}
	if err := f.Type("x"); err == nil {
		t.Fatal("Type without session must fail")
	}
	if err := f.Key(4); err == nil {
		t.Fatal("Key without session must fail")
	}
	if err := f.Swipe(0, 0, 1, 1, 10); err == nil {
		t.Fatal("Swipe without session must fail")
	}
	if _, err := f.Screenshot(); err == nil {
		t.Fatal("Screenshot without session must fail")
	}
	if err := f.Close(); err == nil {
		t.Fatal("Close without session must fail")
	}

	if err := f.Launch("demo.exe"); err == nil || !strings.Contains(err.Error(), ".apk") {
		t.Fatalf("non-APK launch must fail, got %v", err)
	}
	if err := f.Launch("demo.apk"); err != nil {
		t.Fatalf("Launch: %v", err)
	}

	// Tap the username field (center of [54,200][1026,320]) and type.
	if err := f.Tap(540, 260); err != nil {
		t.Fatalf("Tap field: %v", err)
	}
	if err := f.Type("hi"); err != nil {
		t.Fatalf("Type: %v", err)
	}
	if err := f.Type(" there"); err != nil {
		t.Fatalf("Type append: %v", err)
	}

	// State persists across OpenFake — this is what makes the fake usable
	// from separate CLI invocations.
	f2 := driver.OpenFake()
	raw, err := f2.Dump()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "hi there") {
		t.Fatalf("persisted state lost: %s", raw)
	}

	// Sign in → welcome screen; Back button and keys return to main.
	if err := f.Tap(540, 460); err != nil { // Sign in
		t.Fatal(err)
	}
	raw, _ = f.Dump()
	if !strings.Contains(string(raw), "Welcome, hi there") {
		t.Fatalf("welcome screen wrong: %s", raw)
	}
	if err := f.Tap(540, 460); err != nil { // Back button
		t.Fatal(err)
	}
	if err := f.Tap(540, 620); err != nil { // About
		t.Fatal(err)
	}
	raw, _ = f.Dump()
	if !strings.Contains(string(raw), "AgentDroid demo v0.1.0") {
		t.Fatalf("about screen wrong: %s", raw)
	}
	if err := f.Key(3); err != nil { // HOME → main
		t.Fatal(err)
	}

	// ENTER on a focused field submits.
	if err := f.Tap(540, 260); err != nil {
		t.Fatal(err)
	}
	if err := f.Key(66); err != nil {
		t.Fatal(err)
	}
	raw, _ = f.Dump()
	if !strings.Contains(string(raw), "Welcome,") {
		t.Fatalf("ENTER must submit, got: %s", raw)
	}
	if err := f.Tap(540, 460); err != nil { // Back
		t.Fatal(err)
	}

	// Tap on empty screen area is a harmless no-op.
	if err := f.Tap(10, 2300); err != nil {
		t.Fatalf("tap on empty area must be a no-op, got %v", err)
	}

	// Out-of-range coordinates are rejected, never hit the state machine.
	if err := f.Tap(-1, 0); err == nil {
		t.Fatal("negative tap must fail")
	}
	if err := f.Swipe(0, 0, 100001, 0, 10); err == nil {
		t.Fatal("oversized swipe must fail")
	}

	// Screenshot is a real PNG.
	shot, err := f.Screenshot()
	if err != nil || len(shot) < 8 || shot[0] != 0x89 || shot[1] != 'P' {
		t.Fatalf("Screenshot = %v, %v", shot[:8], err)
	}

	// Close ends the session and clears the state file.
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := f.Dump(); err == nil {
		t.Fatal("Dump after Close must fail")
	}
}

func TestFakeDriverUnicodeType(t *testing.T) {
	// Unlike the adb driver, the fake accepts Unicode — it does not pass
	// through a device shell.
	fakeEnv(t)
	f := driver.OpenFake()
	if err := f.Launch("demo.apk"); err != nil {
		t.Fatal(err)
	}
	if err := f.Tap(540, 260); err != nil {
		t.Fatal(err)
	}
	if err := f.Type("héllo — 🤖"); err != nil {
		t.Fatalf("fake Type must accept Unicode: %v", err)
	}
	raw, err := f.Dump()
	if err != nil {
		t.Fatal(err)
	}
	els, err := snapshot.Parse(raw)
	if err != nil {
		t.Fatalf("unicode text must stay parseable: %v", err)
	}
	found := false
	for _, el := range els {
		if el.Text == "héllo — 🤖" {
			found = true
		}
	}
	if !found {
		t.Fatalf("typed unicode lost in the dump: %+v", els)
	}
}

func TestFakeDriverDevicesIsUnsupported(t *testing.T) {
	f := driver.OpenFake()
	if _, err := f.Devices(); err == nil || !strings.Contains(err.Error(), "adb runtime only") {
		t.Fatalf("Devices under fake = %v", err)
	}
}

func TestFakeDriverStateFileEnvOverride(t *testing.T) {
	// The AGENTDROID_FAKE_STATE override isolates sessions (used by CI and
	// tests); the default lives in the OS temp dir.
	fakeEnv(t)
	f := driver.OpenFake()
	if err := f.Launch("demo.apk"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.Dump(); err != nil {
		t.Fatalf("session under env path must load: %v", err)
	}
	// A fresh driver pointed at a different file sees no session.
	t.Setenv("AGENTDROID_FAKE_STATE", filepath.Join(t.TempDir(), "other.json"))
	fresh := driver.OpenFake()
	if _, err := fresh.Dump(); err == nil {
		t.Fatal("fresh state path must not inherit the other session")
	}
}

func TestFakeDriverXMLEscaping(t *testing.T) {
	// Typed text is untrusted: XML metacharacters must be escaped in the
	// dump and survive a parse round-trip verbatim.
	fakeEnv(t)
	f := driver.OpenFake()
	if err := f.Launch("demo.apk"); err != nil {
		t.Fatal(err)
	}
	if err := f.Tap(540, 260); err != nil {
		t.Fatal(err)
	}
	payload := `<b> & "it's" >`
	if err := f.Type(payload); err != nil {
		t.Fatal(err)
	}
	raw, err := f.Dump()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "&lt;b&gt; &amp; &quot;it&apos;s&quot; &gt;") {
		t.Fatalf("metacharacters must be XML-escaped in the dump: %s", raw)
	}
	els, err := snapshot.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, el := range els {
		if el.Text == payload {
			found = true
		}
	}
	if !found {
		t.Fatal("escaped text must round-trip through the parser verbatim")
	}
}

func TestFakeDriverTypeLengthCap(t *testing.T) {
	fakeEnv(t)
	f := driver.OpenFake()
	if err := f.Launch("demo.apk"); err != nil {
		t.Fatal(err)
	}
	if err := f.Tap(540, 260); err != nil {
		t.Fatal(err)
	}
	if err := f.Type(string(make([]byte, 4097))); err == nil {
		t.Fatal("fake Type must honor the 4096-byte cap")
	}
}
