package app

// In-module gates for the session launcher's failure paths: a dying
// adapter surfaces a clear error and stopDaemon really terminates.

import (
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/webdevshayan/agent-droid/core/cli"
	"github.com/webdevshayan/agent-droid/core/driver"
)

func TestStopDaemonTerminates(t *testing.T) {
	cmd := exec.Command("sleep", "30")
	if err := cmd.Start(); err != nil {
		t.Skipf("no sleep binary: %v", err)
	}
	stopDaemon(cmd)
	done := make(chan struct{})
	go func() { _ = cmd.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("stopDaemon did not terminate the process group")
	}
}

func TestSessionLaunchAdapterDies(t *testing.T) {
	apk := filepath.Join(t.TempDir(), "demo.apk")
	if err := os.WriteFile(apk, []byte("PK\x03\x04 dies"), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := cli.Command{
		Verb:       cli.VerbLaunch,
		Path:       apk,
		Runtime:    cli.RuntimeFakeLogin,
		Session:    "dies-" + time.Now().Format("150405.000000000"),
		RuntimeBin: "/bin/false", // the adapter exits before serving
	}
	var out strings.Builder
	err := sessionLaunch(cmd, &out)
	if err == nil || !strings.Contains(err.Error(), "exited before accepting connections") {
		t.Fatalf("sessionLaunch with a dying adapter = %v", err)
	}
}

// TestDispatchUnknownVerbIsAnError pins the defensive default case: a verb
// the parser accepts but dispatch does not implement must fail loudly, never
// return exit 0 with no output (AGD-21). Unreachable through the real CLI —
// the parser rejects unknown verbs first — which is exactly why it must be
// tested directly.
func TestDispatchUnknownVerbIsAnError(t *testing.T) {
	err := dispatch(&driver.Fake{}, cli.Command{Verb: cli.Verb("frobnicate")}, io.Discard, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "unimplemented command") {
		t.Fatalf("unknown verb must error, got %v", err)
	}
}
