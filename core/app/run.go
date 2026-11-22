// Package app dispatches parsed Commands against a driver and defines the
// process exit codes: 0 success, 1 runtime error, 2 usage error.
package app

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	"github.com/webdevshayan/agent-droid/core/cli"
	"github.com/webdevshayan/agent-droid/core/device"
	"github.com/webdevshayan/agent-droid/core/driver"
	"github.com/webdevshayan/agent-droid/core/internal/protocol"
	"github.com/webdevshayan/agent-droid/core/internal/runtime"
	"github.com/webdevshayan/agent-droid/core/snapshot"
)

// Version is stamped at build time via -ldflags.
var Version = "0.1.0"

// clearDelKeys bounds the DEL sweep used by `clear` — never unbounded.
const clearDelKeys = 40

// Run executes one invocation. drv may be injected (tests) or nil (built
// from the --runtime flag). It returns the process exit code.
func Run(argv []string, stdout, stderr io.Writer, drv driver.Driver) int {
	cmd, err := cli.Parse(argv)
	if err != nil {
		fmt.Fprintf(stderr, "agentdroid: %v\n", err)
		return 2
	}

	switch cmd.Verb {
	case cli.VerbHelp:
		fmt.Fprint(stdout, Usage)
		return 0
	case cli.VerbVersion:
		fmt.Fprintf(stdout, "agentdroid %s\n", Version)
		return 0
	case cli.VerbServe:
		if err := runtime.ServeMain(runtime.ServeOptions{
			Backend:    cmd.Backend,
			Session:    cmd.Session,
			APK:        cmd.Path,
			RuntimeBin: cmd.RuntimeBin,
			Stdio:      cmd.Stdio,
			Stdout:     stdout,
			Stderr:     stderr,
		}); err != nil {
			fmt.Fprintf(stderr, "agentdroid: serve: %v\n", err)
			return 1
		}
		return 0
	}

	// Session mode: launch spawns the zero-storage daemon; every other
	// command attaches to the live session and drives it through the same
	// Driver contract as adb (driver.RT over the session client). An
	// injected drv (tests) only applies to the in-process path.
	if cmd.Session != "" {
		return runSession(cmd, stdout, stderr)
	}

	if drv == nil {
		if drv, err = openDriver(cmd); err != nil {
			fmt.Fprintf(stderr, "agentdroid: %v\n", err)
			return 1
		}
	}

	if err := dispatch(drv, cmd, stdout); err != nil {
		fmt.Fprintf(stderr, "agentdroid: %v\n", err)
		return 1
	}
	return 0
}

// openDriver builds the driver the command asks for.
func openDriver(cmd cli.Command) (driver.Driver, error) {
	if cmd.Runtime == cli.RuntimeFake {
		return driver.OpenFake(), nil
	}
	c, err := device.New(cmd.Serial, nil)
	if err != nil {
		return nil, err
	}
	return driver.NewADB(c), nil
}

func dispatch(d driver.Driver, cmd cli.Command, stdout io.Writer) error {
	switch cmd.Verb {
	case cli.VerbDevices:
		listing, err := d.Devices()
		if err != nil {
			return err
		}
		fmt.Fprintln(stdout, listing)

	case cli.VerbLaunch, cli.VerbInstall:
		if err := d.Launch(cmd.Path); err != nil {
			return err
		}
		fmt.Fprintf(stdout, "launched %s\n", cmd.Path)

	case cli.VerbClose:
		if err := d.Close(); err != nil {
			return err
		}
		fmt.Fprintln(stdout, "closed")

	case cli.VerbSnapshot:
		raw, err := d.Dump()
		if err != nil {
			return err
		}
		if cmd.XML {
			stdout.Write(raw)
			fmt.Fprintln(stdout)
			return nil
		}
		els, err := snapshot.Parse(raw)
		if err != nil {
			return err
		}
		if len(els) == 0 {
			fmt.Fprintln(stdout, "no tappable elements found on screen")
			return nil
		}
		fmt.Fprint(stdout, snapshot.Format(els))

	case cli.VerbTap:
		el, err := resolveCurrent(d, cmd.Ref)
		if err != nil {
			return err
		}
		x, y := el.Bounds.Center()
		if err := d.Tap(x, y); err != nil {
			return err
		}
		fmt.Fprintf(stdout, "tapped %s at (%d,%d)\n", cmd.Ref, x, y)

	case cli.VerbClear:
		el, err := resolveCurrent(d, cmd.Ref)
		if err != nil {
			return err
		}
		x, y := el.Bounds.Center()
		if err := d.Tap(x, y); err != nil {
			return err
		}
		if err := d.Key(123); err != nil { // KEYCODE_MOVE_END
			return err
		}
		for i := 0; i < clearDelKeys; i++ {
			if err := d.Key(67); err != nil { // KEYCODE_DEL
				return err
			}
		}
		fmt.Fprintf(stdout, "cleared %s\n", cmd.Ref)

	case cli.VerbType:
		if err := d.Type(cmd.Text); err != nil {
			return err
		}
		fmt.Fprintf(stdout, "typed %d bytes\n", len(cmd.Text))

	case cli.VerbSwipe:
		if cmd.Coords != nil {
			if err := d.Swipe(cmd.Coords[0], cmd.Coords[1], cmd.Coords[2], cmd.Coords[3], 300); err != nil {
				return err
			}
			fmt.Fprintf(stdout, "swiped (%d,%d) -> (%d,%d)\n", cmd.Coords[0], cmd.Coords[1], cmd.Coords[2], cmd.Coords[3])
			return nil
		}
		from, err := resolveCurrent(d, cmd.Ref)
		if err != nil {
			return err
		}
		to, err := resolveCurrent(d, cmd.Ref2)
		if err != nil {
			return err
		}
		x1, y1 := from.Bounds.Center()
		x2, y2 := to.Bounds.Center()
		if err := d.Swipe(x1, y1, x2, y2, 300); err != nil {
			return err
		}
		fmt.Fprintf(stdout, "swiped %s (%d,%d) -> %s (%d,%d)\n", cmd.Ref, x1, y1, cmd.Ref2, x2, y2)

	case cli.VerbScreenshot:
		data, err := d.Screenshot()
		if err != nil {
			return err
		}
		if len(data) == 0 {
			return errors.New("screenshot: driver returned no data")
		}
		if cmd.Out != "" {
			n, err := writePNG(cmd.Out, data)
			if err != nil {
				return err
			}
			fmt.Fprintf(stdout, "saved %s (%s)\n", cmd.Out, humanBytes(n))
			return nil
		}
		// No --out: raw PNG on stdout, so `agentdroid screenshot > screen.png`
		// works exactly as documented.
		stdout.Write(data)
		return nil

	case cli.VerbBack:
		if err := d.Key(4); err != nil {
			return err
		}
		fmt.Fprintln(stdout, "back")

	case cli.VerbHome:
		if err := d.Key(3); err != nil {
			return err
		}
		fmt.Fprintln(stdout, "home")
	}
	return nil
}

// sessionDialTimeout bounds attaching to a live session socket.
const sessionDialTimeout = 15 * time.Second

// launchDeadline bounds spawning the daemon and the launch handshake.
const launchDeadline = 30 * time.Second

// runSession executes one command against a runtime daemon session: launch
// spawns `agentdroid serve` detached (setsid) and performs the launch
// handshake; everything else attaches through driver.RT, so dispatch, refs
// and exit codes are identical to the in-process runtimes. No session
// state ever touches disk — the daemon keeps it in memory.
func runSession(cmd cli.Command, stdout, stderr io.Writer) int {
	if cmd.Verb == cli.VerbLaunch || cmd.Verb == cli.VerbInstall {
		if err := sessionLaunch(cmd, stdout); err != nil {
			fmt.Fprintf(stderr, "agentdroid: %v\n", err)
			return 1
		}
		return 0
	}
	client, err := runtime.DialSession(cmd.Session, sessionDialTimeout)
	if err != nil {
		fmt.Fprintf(stderr, "agentdroid: %v\n", err)
		return 1
	}
	if err := dispatch(driver.NewRT(client), cmd, stdout); err != nil {
		fmt.Fprintf(stderr, "agentdroid: %v\n", err)
		return 1
	}
	return 0
}

// sessionLaunch spawns this binary as a detached serve daemon for the
// session's backend, waits for its abstract socket, and hands it the APK.
// The daemon survives this short-lived process; the session lives entirely
// in the daemon's memory and its kernel-only socket.
func sessionLaunch(cmd cli.Command, stdout io.Writer) error {
	if _, err := os.Stat(cmd.Path); err != nil {
		return fmt.Errorf("launch: apk not accessible: %w", err)
	}
	// A live session is never silently replaced; make the operator close it.
	if probe, err := runtime.DialSession(cmd.Session, 500*time.Millisecond); err == nil {
		_ = probe.Close()
		return fmt.Errorf("session %q is already live; run agentdroid close --session %s first", cmd.Session, cmd.Session)
	}

	bin := cmd.RuntimeBin
	if bin == "" {
		self, err := os.Executable()
		if err != nil {
			return fmt.Errorf("launch: resolve adapter binary: %w", err)
		}
		bin = self
	}
	daemon := exec.Command(bin, "serve", "--backend", cmd.Runtime, "--session", cmd.Session)
	// Detached daemons get /dev/null stdio: an inherited pipe would keep
	// whoever ran `launch` waiting for EOF until the daemon exits. Bind
	// failures surface through the exited channel instead of stderr.
	daemon.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := daemon.Start(); err != nil {
		return fmt.Errorf("launch: start %s adapter: %w", cmd.Runtime, err)
	}
	exited := make(chan error, 1)
	go func() { exited <- daemon.Wait() }()

	client, err := awaitSession(cmd.Session, daemon, exited)
	if err != nil {
		return err
	}
	resp, err := client.Call(protocol.Request{Cmd: protocol.CmdLaunch, APK: cmd.Path}, launchDeadline)
	_ = client.Close()
	if err != nil {
		stopDaemon(daemon)
		return fmt.Errorf("launch: handshake failed: %w", err)
	}
	if resp.Error != "" {
		stopDaemon(daemon)
		return fmt.Errorf("launch failed: %s", resp.Error)
	}

	base := filepath.Base(cmd.Path)
	if resp.PID > 0 {
		fmt.Fprintf(stdout, "launched %s — session %q (adapter pid %d)\n", base, cmd.Session, resp.PID)
	} else {
		fmt.Fprintf(stdout, "launched %s — session %q\n", base, cmd.Session)
	}
	return nil
}

// awaitSession dials the session socket until the daemon accepts, the
// daemon dies, or the deadline passes.
func awaitSession(session string, daemon *exec.Cmd, exited <-chan error) (*runtime.Client, error) {
	deadline := time.Now().Add(launchDeadline)
	for {
		c, err := runtime.DialSession(session, 250*time.Millisecond)
		if err == nil {
			return c, nil
		}
		select {
		case e := <-exited:
			return nil, fmt.Errorf("launch: adapter exited before accepting connections: %v", e)
		default:
		}
		if time.Now().After(deadline) {
			stopDaemon(daemon)
			return nil, fmt.Errorf("launch: adapter did not accept a connection within %s", launchDeadline)
		}
		time.Sleep(25 * time.Millisecond)
	}
}

// stopDaemon terminates the daemon's whole process group (it runs detached
// via setsid, so the process handle alone is not enough). If the group kill
// fails — e.g. the process was started without its own group — fall back to
// the single process, so a daemon never outlives a failed launch.
func stopDaemon(daemon *exec.Cmd) {
	if daemon == nil || daemon.Process == nil {
		return
	}
	if err := syscall.Kill(-daemon.Process.Pid, syscall.SIGTERM); err != nil {
		_ = daemon.Process.Kill()
	}
}

// resolveCurrent dumps, parses and resolves one ref against the live view
// tree. Refs are always re-resolved against the current screen — stateless
// by design.
func resolveCurrent(d driver.Driver, ref string) (snapshot.Element, error) {
	raw, err := d.Dump()
	if err != nil {
		return snapshot.Element{}, err
	}
	els, err := snapshot.Parse(raw)
	if err != nil {
		return snapshot.Element{}, err
	}
	return snapshot.Resolve(els, ref)
}

func writePNG(path string, data []byte) (int, error) {
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return 0, fmt.Errorf("screenshot: %w", err)
		}
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return 0, fmt.Errorf("screenshot: %w", err)
	}
	return len(data), nil
}

func humanBytes(n int) string {
	switch {
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MiB", float64(n)/(1<<20))
	case n >= 1024:
		return fmt.Sprintf("%.1f KiB", float64(n)/1024)
	default:
		return strconv.Itoa(n) + " B"
	}
}
