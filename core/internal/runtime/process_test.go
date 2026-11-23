package runtime

import (
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func setpgidAttr() *syscall.SysProcAttr { return &syscall.SysProcAttr{Setpgid: true} }

func TestProcessBackendSpawnAndKill(t *testing.T) {
	// Use `sleep` as a stand-in for a real runtime launcher: it is a plain
	// long-lived process, exactly the lifecycle the backend must manage.
	b := newProcessBackend("atl", makeApk(t), "", "AGENTDROID_ATL_BIN", "AGENTDROID_ATL_ARGS", "sleep")
	pb := b.(*processBackend)
	pb.bin = "sleep"
	pb.argsTmpl = []string{"30"}

	resp := pb.Launch("")
	if !resp.OK || resp.PID <= 0 {
		t.Fatalf("launch: %+v", resp)
	}
	pid := resp.PID
	if !processAlive(pid) {
		t.Fatalf("runtime process %d not alive after launch", pid)
	}

	// A second launch must be refused while one is running.
	if resp := pb.Launch(""); resp.OK {
		t.Fatal("second launch must fail")
	}

	// Close terminates the process group.
	if resp := pb.Close(); !resp.OK {
		t.Fatalf("close: %s", resp.Error)
	}
	deadline := time.Now().Add(3 * time.Second)
	for processAlive(pid) {
		if time.Now().After(deadline) {
			t.Fatalf("runtime process %d still alive after close", pid)
		}
		time.Sleep(20 * time.Millisecond)
	}
	// Stop is idempotent.
	pb.Stop()
}

func TestProcessBackendErrorsBeforeAndAfter(t *testing.T) {
	b := newProcessBackend("winedroid", makeApk(t), "", "AGENTDROID_WINEDROID_BIN", "AGENTDROID_WINEDROID_ARGS", "winedroid-missing-binary")
	// Snapshot before launch fails with a session hint.
	if resp := b.Snapshot(); resp.OK || !strings.Contains(resp.Error, "launch first") {
		t.Fatalf("snapshot before launch: %+v", resp)
	}
	// Launch fails when the launcher binary is not in PATH.
	if resp := b.Launch(""); resp.OK || !strings.Contains(resp.Error, "not found in PATH") {
		t.Fatalf("launch with missing binary: %+v", resp)
	}
	// With a valid binary but a broken args template, spawn still happens
	// exactly as configured — verify template substitution.
	b2 := newProcessBackend("atl", "", "", "", "", "sleep")
	pb := b2.(*processBackend)
	pb.argsTmpl = []string{"--apk", "{apk}", "30"}
	resp := pb.Launch(makeApk(t))
	if !resp.OK {
		t.Fatalf("templated launch: %s", resp.Error)
	}
	pb.Stop()
}

func processAlive(pid int) bool {
	proc := "/proc/" + strconv.Itoa(pid)
	if _, err := os.Stat(proc); err != nil {
		// /proc unavailable (non-Linux test host): fall back to signal 0.
		p, err := os.FindProcess(pid)
		if err != nil {
			return false
		}
		return p.Signal(nil) == nil
	}
	return true
}

func TestTerminateKillsProcessGroup(t *testing.T) {
	// `sh -c 'sleep 30 & wait'` forks a child, exercising the process-group
	// kill path.
	cmd := exec.Command("/bin/sh", "-c", "sleep 30 & wait")
	cmd.SysProcAttr = setpgidAttr()
	if err := cmd.Start(); err != nil {
		t.Skipf("cannot start sh: %v", err)
	}
	terminate(cmd)
	deadline := time.Now().Add(3 * time.Second)
	for processAlive(cmd.Process.Pid) {
		if time.Now().After(deadline) {
			t.Fatalf("process %d still alive after terminate", cmd.Process.Pid)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestProcessBackendDetectsCrash(t *testing.T) {
	// AGD-10: a launcher that exits right after spawn must be reaped and
	// reported honestly — Snapshot may not answer the hook error as if the
	// runtime were alive, and a relaunch must be possible.
	b := newProcessBackend("atl", makeApk(t), "", "", "", "true")
	pb := b.(*processBackend)
	pb.argsTmpl = []string{} // `true` takes no arguments and exits at once

	if resp := pb.Launch(""); !resp.OK {
		t.Fatalf("launch: %s", resp.Error)
	}
	deadline := time.Now().Add(3 * time.Second)
	for {
		pb.mu.Lock()
		exited := pb.exited
		pb.mu.Unlock()
		if exited {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the reaper never recorded the launcher exit")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if resp := pb.Snapshot(); resp.OK || !strings.Contains(resp.Error, "runtime exited") {
		t.Fatalf("snapshot after a launcher crash must report the exit, got %+v", resp)
	}
	if resp := pb.Tap(1, 1); resp.OK || !strings.Contains(resp.Error, "runtime exited") {
		t.Fatalf("tap after a launcher crash must report the exit, got %+v", resp)
	}
	// A fresh launch must be possible again — never "already running".
	if resp := pb.Launch(""); !resp.OK {
		t.Fatalf("relaunch after exit refused: %s", resp.Error)
	}
	pb.Stop()
}

func TestProcessBackendStillRefusesDoubleLaunch(t *testing.T) {
	// While the launcher is genuinely alive, a second launch is refused.
	b := newProcessBackend("atl", makeApk(t), "", "AGENTDROID_ATL_BIN", "AGENTDROID_ATL_ARGS", "sleep")
	pb := b.(*processBackend)
	pb.argsTmpl = []string{"30"}
	if resp := pb.Launch(""); !resp.OK {
		t.Fatalf("launch: %s", resp.Error)
	}
	if resp := pb.Launch(""); resp.OK || !strings.Contains(resp.Error, "already running") {
		t.Fatalf("second launch must be refused with \"already running\", got %+v", resp)
	}
	pb.Stop()
}
