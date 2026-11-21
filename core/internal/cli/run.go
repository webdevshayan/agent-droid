package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"

	"github.com/webdevshayan/agent-droid/core/internal/protocol"
	"github.com/webdevshayan/agent-droid/core/internal/runtime"
	"github.com/webdevshayan/agent-droid/core/internal/uitree"
)

// Execute parses args and runs the command, returning the process exit code:
// 0 success, 1 failed operation, 2 usage error.
func Execute(args []string, stdout, stderr io.Writer) int {
	cmd, err := Parse(args)
	if err != nil {
		fmt.Fprintf(stderr, "agentdroid: %v\n\n%s", err, Usage())
		return 2
	}
	switch cmd.Name {
	case "help":
		fmt.Fprint(stdout, Usage())
		return 0
	case "version":
		fmt.Fprintf(stdout, "agentdroid %s\n", Version)
		return 0
	case "serve":
		if err := runtime.ServeMain(runtime.ServeOptions{
			Backend:    cmd.Backend,
			Session:    cmd.Session,
			APK:        cmd.APK,
			RuntimeBin: cmd.RuntimeBin,
			Stdio:      cmd.Stdio,
			Stdout:     stdout,
			Stderr:     stderr,
		}); err != nil {
			fmt.Fprintf(stderr, "agentdroid: serve: %v\n", err)
			return 1
		}
		return 0
	case "launch":
		if err := launch(cmd, stdout); err != nil {
			fmt.Fprintf(stderr, "agentdroid: %v\n", err)
			return 1
		}
		return 0
	default:
		if err := runSessionCommand(cmd, stdout); err != nil {
			fmt.Fprintf(stderr, "agentdroid: %v\n", err)
			return 1
		}
		return 0
	}
}

// runSessionCommand connects to a live session and performs one command.
// Tap and type always resolve their ref against the live screen first, so
// refs never go stale between separate CLI invocations — and no element map
// ever needs to be stored on disk.
func runSessionCommand(cmd *Command, stdout io.Writer) error {
	client, err := runtime.DialSession(cmd.Session, cmd.Timeout)
	if err != nil {
		return err
	}
	defer client.Close()

	switch cmd.Name {
	case "snapshot":
		resp, err := client.Call(protocol.Request{Cmd: protocol.CmdSnapshot}, cmd.Timeout)
		if err != nil {
			return err
		}
		if !resp.OK {
			return fmt.Errorf("snapshot failed: %s", resp.Error)
		}
		return renderSnapshot(resp, cmd, stdout)

	case "tap":
		req, err := resolveInteraction(client, cmd, protocol.CmdTap)
		if err != nil {
			return err
		}
		resp, err := client.Call(*req, cmd.Timeout)
		if err != nil {
			return err
		}
		if !resp.OK {
			return fmt.Errorf("tap failed: %s", resp.Error)
		}
		if cmd.JSON {
			return printJSON(resp, stdout)
		}
		fmt.Fprintf(stdout, "tapped %s at (%d,%d)\n", cmd.Ref, req.X, req.Y)
		return nil

	case "type":
		req, err := resolveInteraction(client, cmd, protocol.CmdType)
		if err != nil {
			return err
		}
		req.Text = cmd.Text
		resp, err := client.Call(*req, cmd.Timeout)
		if err != nil {
			return err
		}
		if !resp.OK {
			return fmt.Errorf("type failed: %s", resp.Error)
		}
		if cmd.JSON {
			return printJSON(resp, stdout)
		}
		fmt.Fprintf(stdout, "typed %q into %s at (%d,%d)\n", cmd.Text, cmd.Ref, req.X, req.Y)
		return nil

	case "screenshot":
		resp, err := client.Call(protocol.Request{Cmd: protocol.CmdScreenshot}, cmd.Timeout)
		if err != nil {
			return err
		}
		if !resp.OK {
			return fmt.Errorf("screenshot failed: %s", resp.Error)
		}
		if cmd.JSON {
			return printJSON(resp, stdout)
		}
		if len(resp.PNG) == 0 {
			return errors.New("screenshot returned no image data")
		}
		_, err = stdout.Write(resp.PNG)
		return err

	case "close":
		resp, err := client.Call(protocol.Request{Cmd: protocol.CmdClose}, cmd.Timeout)
		if err != nil {
			return err
		}
		if !resp.OK {
			return fmt.Errorf("close failed: %s", resp.Error)
		}
		if cmd.JSON {
			return printJSON(resp, stdout)
		}
		fmt.Fprintf(stdout, "session %q closed\n", cmd.Session)
		return nil
	}
	return fmt.Errorf("unhandled command %q", cmd.Name)
}

// resolveInteraction snapshots the live screen, maps the ref to the center
// of the element's bounds, and builds the request.
func resolveInteraction(client *runtime.Client, cmd *Command, action string) (*protocol.Request, error) {
	resp, err := client.Call(protocol.Request{Cmd: protocol.CmdSnapshot}, cmd.Timeout)
	if err != nil {
		return nil, err
	}
	if !resp.OK {
		return nil, fmt.Errorf("cannot resolve %s: %s", cmd.Ref, resp.Error)
	}
	el, err := elementFor(resp, cmd.Ref)
	if err != nil {
		return nil, err
	}
	x, y := el.Node.Center()
	return &protocol.Request{Cmd: action, X: x, Y: y}, nil
}

func elementFor(resp *protocol.Response, ref string) (*uitree.Element, error) {
	if resp.Tree == nil {
		return nil, errors.New("adapter returned no view tree")
	}
	if err := uitree.Validate(resp.Tree); err != nil {
		return nil, err
	}
	return uitree.Resolve(uitree.Flatten(resp.Tree), ref)
}

func renderSnapshot(resp *protocol.Response, cmd *Command, stdout io.Writer) error {
	if cmd.JSON {
		return printJSON(resp, stdout)
	}
	if resp.Tree == nil {
		return errors.New("adapter returned no view tree")
	}
	if err := uitree.Validate(resp.Tree); err != nil {
		return err
	}
	if cmd.Tree {
		_, err := fmt.Fprint(stdout, uitree.RenderTree(resp.Tree))
		return err
	}
	els := uitree.Flatten(resp.Tree)
	if _, err := fmt.Fprint(stdout, uitree.RenderList(els)); err != nil {
		return err
	}
	_, err := fmt.Fprintf(stdout, "# %d elements\n", len(els))
	return err
}

func printJSON(v any, stdout io.Writer) error {
	enc := json.NewEncoder(stdout)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

// launch spawns the adapter daemon (this binary in serve mode, or a custom
// --runtime-bin), waits for its abstract-socket session, and performs the
// launch handshake. The daemon detaches with setsid so it survives this
// short-lived CLI process, and the session lives entirely in the daemon's
// memory — nothing is written to disk.
func launch(cmd *Command, stdout io.Writer) error {
	if _, err := os.Stat(cmd.APK); err != nil {
		return fmt.Errorf("launch: apk not accessible: %w", err)
	}
	// A live session is never silently replaced; make the operator close it.
	if probe, err := runtime.DialSession(cmd.Session, 500*time.Millisecond); err == nil {
		_ = probe.Close()
		return fmt.Errorf("session %q is already live; run agentdroid close first", cmd.Session)
	}

	bin := cmd.RuntimeBin
	if bin == "" {
		self, err := os.Executable()
		if err != nil {
			return fmt.Errorf("launch: resolve adapter binary: %w", err)
		}
		bin = self
	}
	args := []string{
		"serve",
		"--backend", cmd.Runtime,
		"--session", cmd.Session,
	}
	daemon := exec.Command(bin, args...)
	// Detached daemons get /dev/null stdio: the CLI process is short-lived,
	// and an inherited pipe would keep whoever ran `launch` waiting for EOF
	// until the daemon exits. Bind failures surface through the exited
	// channel below instead of stderr.
	daemon.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := daemon.Start(); err != nil {
		return fmt.Errorf("launch: start adapter %s: %w", bin, err)
	}
	exited := make(chan error, 1)
	go func() { exited <- daemon.Wait() }()

	deadline := time.Now().Add(DefaultLaunchDeadline)
	var client *runtime.Client
	for {
		c, err := runtime.DialSession(cmd.Session, 250*time.Millisecond)
		if err == nil {
			client = c
			break
		}
		select {
		case e := <-exited:
			return fmt.Errorf("launch: adapter exited before accepting connections: %v", e)
		default:
		}
		if time.Now().After(deadline) {
			stopDaemon(daemon)
			return fmt.Errorf("launch: adapter did not accept a connection within %s", DefaultLaunchDeadline)
		}
		time.Sleep(25 * time.Millisecond)
	}

	resp, err := client.Call(protocol.Request{Cmd: protocol.CmdLaunch, APK: cmd.APK}, DefaultLaunchDeadline)
	_ = client.Close()
	if err != nil {
		stopDaemon(daemon)
		return fmt.Errorf("launch: handshake failed: %w", err)
	}
	if !resp.OK {
		stopDaemon(daemon)
		return fmt.Errorf("launch failed: %s", resp.Error)
	}

	base := filepath.Base(cmd.APK)
	if resp.PID > 0 {
		fmt.Fprintf(stdout, "launched %s — session %q (adapter pid %d)\n", base, cmd.Session, resp.PID)
	} else {
		fmt.Fprintf(stdout, "launched %s — session %q\n", base, cmd.Session)
	}
	return nil
}

// stopDaemon terminates the adapter's process group: it runs detached via
// setsid, so the plain process handle alone is not enough.
func stopDaemon(daemon *exec.Cmd) {
	if daemon == nil || daemon.Process == nil {
		return
	}
	_ = syscall.Kill(-daemon.Process.Pid, syscall.SIGTERM)
}
