// Package runtime is the server side of the orchestrator: adapters implement
// Backend to drive a real or simulated Android runtime, and Serve/ServeMain
// expose them to the CLI over an abstract Unix socket (Linux-only), so a
// session leaves no trace on the filesystem.
package runtime

import (
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/webdevshayan/agent-droid/core/internal/protocol"
)

// Runtime choices and session naming.
const (
	RuntimeATL       = "atl"
	RuntimeWineDroid = "winedroid"
	RuntimeFake      = "fake"
	DefaultSession   = "default"
)

// SocketPrefix names the abstract Unix domain sockets sessions listen on:
// "@agentdroid-<session>". Abstract sockets exist only in the kernel, which
// is what keeps sessions zero-storage on disk.
const SocketPrefix = "agentdroid-"

// Backend is the contract a runtime adapter implements for the
// orchestrator. Methods return protocol responses directly; failures are
// OK=false responses. Implementations must be safe for concurrent use.
type Backend interface {
	// Launch starts the app for this session.
	Launch(apk string) *protocol.Response
	// Snapshot returns the current view tree.
	Snapshot() *protocol.Response
	// Tap taps the given point in frame coordinates.
	Tap(x, y int) *protocol.Response
	// Type taps the given point to focus it, then types text.
	Type(x, y int, text string) *protocol.Response
	// Screenshot returns a PNG of the current frame.
	Screenshot() *protocol.Response
	// Close ends the session from the client side.
	Close() *protocol.Response
	// Stop kills any runtime process the backend spawned. It must be safe
	// to call repeatedly and from any state.
	Stop()
}

// NewBackend builds the backend named by name (fake, atl or winedroid).
func NewBackend(name, apk, runtimeBin string) (Backend, error) {
	switch name {
	case RuntimeFake:
		return NewFakeBackend(apk), nil
	case RuntimeATL:
		return NewATLBackend(apk, runtimeBin), nil
	case RuntimeWineDroid:
		return NewWineDroidBackend(apk, runtimeBin), nil
	default:
		return nil, fmt.Errorf("unknown backend %q (want %s, %s or %s)", name, RuntimeFake, RuntimeATL, RuntimeWineDroid)
	}
}

// errSessionClosed is returned by ServeConn when a client completed a close
// command, telling the host to shut the session down.
var errSessionClosed = errors.New("session closed")

// ServeConn serves requests from a single connection until EOF, a write
// failure, or a successful close command.
func ServeConn(r io.Reader, w io.Writer, b Backend) error {
	br := protocol.NewReader(r)
	for {
		var req protocol.Request
		if err := br.Next(&req); err != nil {
			return err
		}
		resp := dispatch(&req, b)
		if err := protocol.WriteMessage(w, resp); err != nil {
			return err
		}
		if req.Cmd == protocol.CmdClose && resp.OK {
			return errSessionClosed
		}
	}
}

// dispatch runs one request against the backend. A panicking adapter must
// degrade to an error response, never kill the session process.
func dispatch(req *protocol.Request, b Backend) (resp *protocol.Response) {
	resp = &protocol.Response{ID: req.ID}
	defer func() {
		if p := recover(); p != nil {
			resp = &protocol.Response{ID: req.ID, Error: fmt.Sprintf("adapter panic: %v", p)}
		}
	}()
	switch req.Cmd {
	case protocol.CmdLaunch:
		resp = b.Launch(req.APK)
	case protocol.CmdSnapshot:
		resp = b.Snapshot()
	case protocol.CmdTap:
		if req.X < 0 || req.Y < 0 {
			resp = &protocol.Response{Error: fmt.Sprintf("tap: negative coordinates (%d,%d)", req.X, req.Y)}
		} else {
			resp = b.Tap(req.X, req.Y)
		}
	case protocol.CmdType:
		if req.Text == "" {
			resp = &protocol.Response{Error: "type: empty text"}
		} else {
			resp = b.Type(req.X, req.Y, req.Text)
		}
	case protocol.CmdScreenshot:
		resp = b.Screenshot()
	case protocol.CmdClose:
		resp = b.Close()
	default:
		resp = &protocol.Response{Error: fmt.Sprintf("unknown command %q", req.Cmd)}
	}
	resp.ID = req.ID
	return resp
}

// Serve accepts connections on ln and serves each concurrently. It returns
// once ln is closed. onClosed runs when a client completed a close command,
// so hosts can shut the session down.
func Serve(ln net.Listener, b Backend, onClosed func()) error {
	for {
		conn, err := ln.Accept()
		if err != nil {
			return err
		}
		go func() {
			defer conn.Close()
			if err := ServeConn(conn, conn, b); err == errSessionClosed && onClosed != nil {
				onClosed()
			}
		}()
	}
}

// ServeOptions configures ServeMain, the entry point of `agentdroid serve`.
type ServeOptions struct {
	Backend    string
	Session    string
	APK        string
	RuntimeBin string
	Stdio      bool
	Stdout     io.Writer // used in stdio mode
	Stderr     io.Writer
}

// ServeMain builds the requested backend and serves it either on the
// session's abstract socket or (with Stdio) on stdin/stdout. It blocks until
// a client closes the session, the listener dies, or the process is
// signalled; the backend's runtime process is always stopped on return.
func ServeMain(o ServeOptions) error {
	b, err := NewBackend(o.Backend, o.APK, o.RuntimeBin)
	if err != nil {
		return err
	}
	defer b.Stop()

	if o.Stdio {
		err := ServeConn(os.Stdin, o.Stdout, b)
		if errors.Is(err, errSessionClosed) || errors.Is(err, io.EOF) {
			return nil
		}
		return err
	}

	if o.Session == "" {
		return errors.New("serve: --session is required for socket mode")
	}
	addr := "@" + SocketPrefix + o.Session
	ln, err := net.Listen("unix", addr)
	if err != nil {
		return fmt.Errorf("serve: bind %s: %w", addr, err)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = Serve(ln, b, func() { ln.Close() })
	}()

	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	select {
	case <-done:
	case <-sigs:
		ln.Close()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
		}
	}
	return nil
}
