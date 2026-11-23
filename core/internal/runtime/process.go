package runtime

import (
	"errors"
	"fmt"
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
	done     chan struct{} // closed by the reaper once the launcher exits
	exited   bool
	exitErr  error
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
		select {
		case <-b.done:
			// The launcher died after spawning: report nothing false here.
			// The exited error surfaces below on demand; a fresh launch is
			// exactly what the operator wants now.
			b.cmd = nil
			b.exited = false
			b.exitErr = nil
		default:
			return errResp("launch: %s runtime already running (pid %d)", b.name, b.cmd.Process.Pid)
		}
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
	cmd.Stdout = os.Stderr // runtime logs go to the daemon's stderr (see app.sessionLaunch)
	cmd.Stderr = os.Stderr
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		return errResp("launch: start %s runtime: %v", b.name, err)
	}
	b.cmd = cmd
	b.done = make(chan struct{})
	b.exited = false
	b.exitErr = nil
	// Reap the child the moment it exits, whatever the reason. Without a
	// reaper a launcher crash is invisible: Launch answers "already
	// running (pid N)" for a corpse and Snapshot answers the hook error as
	// if the runtime were alive. done closes BEFORE the state write —
	// anyone waiting on done must not depend on this mutex, or the wait
	// and the reaper deadlock each other.
	go func() {
		err := cmd.Wait()
		close(b.done)
		b.mu.Lock()
		if b.cmd == cmd { // still the live process (not killed/replaced)
			b.exited = true
			b.exitErr = err
		}
		b.mu.Unlock()
	}()
	return okResp(cmd.Process.Pid)
}

// aliveErrLocked names the honest state of the runtime process: "" when it
// is running, the error text when it is not.
func (b *processBackend) aliveErrLocked() string {
	if b.cmd == nil {
		return fmt.Sprintf("no %s runtime running; launch first", b.name)
	}
	if b.exited {
		return fmt.Sprintf("%s runtime exited (pid %d): %v — launch again", b.name, b.cmd.Process.Pid, b.exitErr)
	}
	return ""
}

func (b *processBackend) Snapshot() *protocol.Response {
	b.mu.Lock()
	defer b.mu.Unlock()
	if msg := b.aliveErrLocked(); msg != "" {
		return errResp("%s", msg)
	}
	return errResp("snapshot: %v", errRuntimeHook)
}

func (b *processBackend) Tap(x, y int) *protocol.Response {
	b.mu.Lock()
	defer b.mu.Unlock()
	if msg := b.aliveErrLocked(); msg != "" {
		return errResp("%s", msg)
	}
	return errResp("tap: %v", errRuntimeHook)
}

func (b *processBackend) Type(x, y int, text string) *protocol.Response {
	b.mu.Lock()
	defer b.mu.Unlock()
	if msg := b.aliveErrLocked(); msg != "" {
		return errResp("%s", msg)
	}
	return errResp("type: %v", errRuntimeHook)
}

func (b *processBackend) Screenshot() *protocol.Response {
	b.mu.Lock()
	defer b.mu.Unlock()
	if msg := b.aliveErrLocked(); msg != "" {
		return errResp("%s", msg)
	}
	return errResp("screenshot: %v", errRuntimeHook)
}

func (b *processBackend) Close() *protocol.Response {
	b.kill()
	return &protocol.Response{OK: true}
}

func (b *processBackend) Stop() {
	b.kill()
}

// kill terminates the runtime process group and waits for the reaper to
// confirm the exit. The wait happens WITHOUT the mutex — the reaper must be
// free to run, so holding the lock across the wait would deadlock it.
func (b *processBackend) kill() {
	b.mu.Lock()
	cmd, done := b.cmd, b.done
	b.cmd = nil
	if cmd != nil {
		pgid := -cmd.Process.Pid
		if err := syscall.Kill(pgid, syscall.SIGTERM); err != nil {
			_ = cmd.Process.Kill()
		}
	}
	b.mu.Unlock()

	if done == nil {
		return
	}
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		<-done
	}
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
