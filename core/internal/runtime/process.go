package runtime

import (
	"errors"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/webdevshayan/agent-droid/core/internal/protocol"
)

// errRuntimeHook describes precisely what Phase 1 cannot do for real
// runtimes: the orchestrator can manage the runtime process, but dumping the
// view tree or framebuffer needs an upstream accessor (docs/ADAPTERS.md
// spells out the hook adapters must expose).
var errRuntimeHook = errors.New(
	"view-hierarchy/framebuffer hook not available for this runtime yet: " +
		"the adapter manages the runtime process, but reading the widget tree or " +
		"framebuffer needs an upstream accessor — see core/docs/ADAPTERS.md")

// processBackend drives a real open-source runtime (ATL or WineDroid) as a
// background Linux process: spawn the launcher with the APK, keep it alive
// for the session, and shut it down cleanly on close. No emulator, no ADB,
// no container — the runtime runs the app natively.
type processBackend struct {
	mu       sync.Mutex
	name     string
	apk      string
	bin      string
	argsTmpl []string
	cmd      *exec.Cmd
}

// NewATLBackend drives the Android Translation Layer launcher. The binary is
// resolved from --runtime-bin, then AGENTDROID_ATL_BIN, then PATH.
func NewATLBackend(apk, runtimeBin string) Backend {
	return newProcessBackend("atl", apk, runtimeBin, "AGENTDROID_ATL_BIN", "AGENTDROID_ATL_ARGS", "android-launcher")
}

// NewWineDroidBackend drives the WineDroid launcher the same way, with its
// own environment overrides.
func NewWineDroidBackend(apk, runtimeBin string) Backend {
	return newProcessBackend("winedroid", apk, runtimeBin, "AGENTDROID_WINEDROID_BIN", "AGENTDROID_WINEDROID_ARGS", "winedroid")
}

func newProcessBackend(name, apk, runtimeBin, envBin, envArgs, defaultBin string) Backend {
	bin := runtimeBin
	if bin == "" {
		bin = os.Getenv(envBin)
	}
	if bin == "" {
		bin = defaultBin
	}
	args := strings.Fields(os.Getenv(envArgs))
	if len(args) == 0 {
		args = []string{"{apk}"}
	}
	return &processBackend{name: name, apk: apk, bin: bin, argsTmpl: args}
}

func (b *processBackend) Launch(apk string) *protocol.Response {
	if apk != "" {
		b.apk = apk
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.apk == "" {
		return errResp("launch: no apk given")
	}
	if b.cmd != nil {
		return errResp("launch: %s runtime already running (pid %d)", b.name, b.cmd.Process.Pid)
	}
	path, err := exec.LookPath(b.bin)
	if err != nil {
		return errResp("launch: %s launcher %q not found in PATH; install the runtime or point --runtime-bin / the AGENTDROID_*_BIN env var at it", b.name, b.bin)
	}
	if _, err := os.Stat(b.apk); err != nil {
		return errResp("launch: apk not readable: %v", err)
	}
	args := make([]string, len(b.argsTmpl))
	for i, a := range b.argsTmpl {
		args[i] = strings.ReplaceAll(a, "{apk}", b.apk)
	}
	cmd := exec.Command(path, args...)
	cmd.Stdout = os.Stderr // runtime logs go to the session stderr, never to files
	cmd.Stderr = os.Stderr
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		return errResp("launch: start %s runtime: %v", b.name, err)
	}
	b.cmd = cmd
	return okResp(cmd.Process.Pid)
}

func (b *processBackend) Snapshot() *protocol.Response {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.cmd == nil {
		return errResp("no %s runtime running; launch first", b.name)
	}
	return errResp("snapshot: %v", errRuntimeHook)
}

func (b *processBackend) Tap(x, y int) *protocol.Response {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.cmd == nil {
		return errResp("no %s runtime running; launch first", b.name)
	}
	return errResp("tap: %v", errRuntimeHook)
}

func (b *processBackend) Type(x, y int, text string) *protocol.Response {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.cmd == nil {
		return errResp("no %s runtime running; launch first", b.name)
	}
	return errResp("type: %v", errRuntimeHook)
}

func (b *processBackend) Screenshot() *protocol.Response {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.cmd == nil {
		return errResp("no %s runtime running; launch first", b.name)
	}
	return errResp("screenshot: %v", errRuntimeHook)
}

func (b *processBackend) Close() *protocol.Response {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.killLocked()
	return &protocol.Response{OK: true}
}

func (b *processBackend) Stop() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.killLocked()
}

func (b *processBackend) killLocked() {
	if b.cmd == nil {
		return
	}
	terminate(b.cmd)
	b.cmd = nil
}

// terminate stops a spawned runtime process group: SIGTERM first, then
// SIGKILL after a grace period. Runtimes are started with Setpgid so their
// whole process tree goes down with the session.
func terminate(cmd *exec.Cmd) {
	if cmd == nil || cmd.Process == nil {
		return
	}
	pgid := -cmd.Process.Pid
	if err := syscall.Kill(pgid, syscall.SIGTERM); err != nil {
		_ = cmd.Process.Kill()
	}
	done := make(chan struct{})
	go func() {
		_ = cmd.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		_ = syscall.Kill(pgid, syscall.SIGKILL)
		<-done
	}
}
