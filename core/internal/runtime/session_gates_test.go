package runtime

// Gate tests for the parts coverage flagged: the ATL/WineDroid process
// backend's hook degradation, Stop idempotence, and the client/protocol
// failure paths.

import (
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/webdevshayan/agent-droid/core/internal/protocol"
)

func TestProcessBackendLifecycleAndHooks(t *testing.T) {
	apk := filepath.Join(t.TempDir(), "demo.apk")
	if err := os.WriteFile(apk, []byte("PK\x03\x04 process test"), 0o644); err != nil {
		t.Fatal(err)
	}
	b := NewATLBackend(apk, "/bin/sleep")

	// Nothing is running yet: hooks report the missing runtime, not a panic.
	if resp := b.Tap(1, 2); resp.OK || !strings.Contains(resp.Error, "no atl runtime running") {
		t.Fatalf("Tap before launch = %+v", resp)
	}
	if resp := b.Snapshot(); resp.OK || !strings.Contains(resp.Error, "no atl runtime running") {
		t.Fatalf("Snapshot before launch = %+v", resp)
	}
	if resp := b.Type(1, 2, "x"); resp.OK || !strings.Contains(resp.Error, "no atl runtime running") {
		t.Fatalf("Type before launch = %+v", resp)
	}
	if resp := b.Screenshot(); resp.OK || !strings.Contains(resp.Error, "no atl runtime running") {
		t.Fatalf("Screenshot before launch = %+v", resp)
	}

	if resp := b.Launch(apk); !resp.OK {
		t.Fatalf("Launch: %s", resp.Error)
	}
	// Double launch is refused while the session lives.
	if resp := b.Launch(apk); resp.OK || !strings.Contains(resp.Error, "already running") {
		t.Fatalf("double Launch = %+v", resp)
	}

	// While the runtime runs, the view/framebuffer hooks degrade to the
	// documented upstream-accessor error (docs/ADAPTERS.md).
	if resp := b.Snapshot(); resp.OK || !strings.Contains(resp.Error, "upstream accessor") {
		t.Fatalf("Snapshot = %+v", resp)
	}
	if resp := b.Tap(3, 4); resp.OK || !strings.Contains(resp.Error, "upstream accessor") {
		t.Fatalf("Tap = %+v", resp)
	}
	if resp := b.Type(3, 4, "hi"); resp.OK || !strings.Contains(resp.Error, "upstream accessor") {
		t.Fatalf("Type = %+v", resp)
	}
	if resp := b.Screenshot(); resp.OK || !strings.Contains(resp.Error, "upstream accessor") {
		t.Fatalf("Screenshot = %+v", resp)
	}

	// Close terminates the spawned process group; Stop is safe from any
	// state, any number of times.
	if resp := b.Close(); !resp.OK {
		t.Fatalf("Close = %+v", resp)
	}
	b.Stop()
	b.Stop()
}

func TestWriteMessageBrokenPipe(t *testing.T) {
	c1, c2 := net.Pipe()
	c2.Close()
	defer c1.Close()
	if err := protocol.WriteMessage(c1, &protocol.Response{OK: true}); err == nil {
		t.Fatal("write to a closed peer must fail")
	}
}

func TestCallAfterCloseFails(t *testing.T) {
	cc, sc := net.Pipe()
	defer sc.Close()
	go func() { _ = ServeConn(sc, sc, NewFakeBackend("")) }()
	c := NewClient(cc)
	if err := c.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if _, err := c.Call(protocol.Request{Cmd: protocol.CmdSnapshot}, time.Second); err == nil {
		t.Fatal("Call on a closed client must fail")
	}
}
