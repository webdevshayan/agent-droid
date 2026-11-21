package runtime

import (
	"bytes"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/webdevshayan/agent-droid/core/internal/protocol"
)

// recordingBackend captures the requests ServeConn passes through, so tests
// can assert exactly what the orchestrator put on the wire.
type recordingBackend struct {
	mu       sync.Mutex
	launches []string
	taps     [][2]int
	types    []protocol.Request
	snapTree *protocol.Response
}

func (r *recordingBackend) Launch(apk string) *protocol.Response {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.launches = append(r.launches, apk)
	return okResp(4242)
}

func (r *recordingBackend) Snapshot() *protocol.Response {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.snapTree != nil {
		return r.snapTree
	}
	return &protocol.Response{OK: true}
}

func (r *recordingBackend) Tap(x, y int) *protocol.Response {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.taps = append(r.taps, [2]int{x, y})
	return &protocol.Response{OK: true}
}

func (r *recordingBackend) Type(x, y int, text string) *protocol.Response {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.types = append(r.types, protocol.Request{X: x, Y: y, Text: text})
	return &protocol.Response{OK: true}
}

func (r *recordingBackend) Screenshot() *protocol.Response {
	return &protocol.Response{OK: true, PNG: []byte("pngbytes")}
}

func (r *recordingBackend) Close() *protocol.Response {
	return &protocol.Response{OK: true}
}

func (r *recordingBackend) Stop() {}

// driveConn wires a client to ServeConn over an in-memory pipe and returns
// the client plus the channel carrying ServeConn's exit status.
func driveConn(t *testing.T, b Backend) (*Client, chan error) {
	t.Helper()
	clientConn, serverConn := net.Pipe()
	t.Cleanup(func() {
		clientConn.Close()
		serverConn.Close()
	})
	done := make(chan error, 1)
	go func() { done <- ServeConn(serverConn, serverConn, b) }()
	t.Cleanup(func() {
		select {
		case <-done:
		case <-time.After(time.Second):
		}
	})
	return &Client{conn: clientConn, br: protocol.NewReader(clientConn)}, done
}

func TestServeConnDispatch(t *testing.T) {
	b := &recordingBackend{}
	c, done := driveConn(t, b)

	resp, err := c.Call(protocol.Request{Cmd: protocol.CmdLaunch, APK: "demo.apk"}, time.Second)
	if err != nil || !resp.OK || resp.PID != 4242 {
		t.Fatalf("launch via wire: %v %+v", err, resp)
	}
	if got := b.launches; len(got) != 1 || got[0] != "demo.apk" {
		t.Fatalf("launches recorded: %v", got)
	}

	resp, err = c.Call(protocol.Request{Cmd: protocol.CmdTap, X: 30, Y: 40}, time.Second)
	if err != nil || !resp.OK {
		t.Fatalf("tap via wire: %v %+v", err, resp)
	}
	if got := b.taps; len(got) != 1 || got[0] != [2]int{30, 40} {
		t.Fatalf("taps recorded: %v", got)
	}

	resp, err = c.Call(protocol.Request{Cmd: protocol.CmdType, X: 1, Y: 2, Text: "hi"}, time.Second)
	if err != nil || !resp.OK {
		t.Fatalf("type via wire: %v %+v", err, resp)
	}
	if got := b.types; len(got) != 1 || got[0].Text != "hi" {
		t.Fatalf("types recorded: %+v", got)
	}

	// Unknown commands degrade to an error response, not a crash.
	resp, err = c.Call(protocol.Request{Cmd: "deploy"}, time.Second)
	if err != nil || resp.OK || !strings.Contains(resp.Error, "unknown command") {
		t.Fatalf("unknown command: %v %+v", err, resp)
	}

	// Close completes the session: ServeConn returns errSessionClosed.
	if _, err := c.Call(protocol.Request{Cmd: protocol.CmdClose}, time.Second); err != nil {
		t.Fatalf("close via wire: %v", err)
	}
	select {
	case err := <-done:
		if !errors.Is(err, errSessionClosed) {
			t.Fatalf("ServeConn returned %v, want errSessionClosed", err)
		}
	case <-time.After(time.Second):
		t.Fatal("ServeConn did not finish after close")
	}
}

func TestServeConnPanicsBecomeErrors(t *testing.T) {
	b := panicBackend{}
	c, _ := driveConn(t, b)
	resp, err := c.Call(protocol.Request{Cmd: protocol.CmdSnapshot}, time.Second)
	if err != nil || resp.OK || !strings.Contains(resp.Error, "adapter panic") {
		t.Fatalf("panic handling: %v %+v", err, resp)
	}
}

type panicBackend struct{}

func (panicBackend) Launch(apk string) *protocol.Response          { panic("boom") }
func (panicBackend) Snapshot() *protocol.Response                  { panic("boom") }
func (panicBackend) Tap(x, y int) *protocol.Response               { panic("boom") }
func (panicBackend) Type(x, y int, text string) *protocol.Response { panic("boom") }
func (panicBackend) Screenshot() *protocol.Response                { panic("boom") }
func (panicBackend) Close() *protocol.Response                     { panic("boom") }
func (panicBackend) Stop()                                         {}

func TestClientTimeout(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	defer serverConn.Close()
	go func() {
		// A backend that never answers.
		buf := make([]byte, 4096)
		for {
			if _, err := serverConn.Read(buf); err != nil {
				return
			}
		}
	}()
	c := &Client{conn: clientConn, br: protocol.NewReader(clientConn)}
	_, err := c.Call(protocol.Request{Cmd: protocol.CmdSnapshot}, 50*time.Millisecond)
	if err == nil || !strings.Contains(err.Error(), "read snapshot response") {
		t.Fatalf("expected timeout error, got %v", err)
	}
}

func TestAbstractSocketSession(t *testing.T) {
	if !fileExists("/proc/self/exe") {
		t.Skip("abstract unix sockets are Linux-only")
	}
	session := "rt-" + time.Now().Format("150405.000000000")
	ln, err := net.Listen("unix", "@"+SocketPrefix+session)
	if err != nil {
		t.Fatalf("bind: %v", err)
	}
	defer ln.Close()
	serveDone := make(chan error, 1)
	go func() { serveDone <- Serve(ln, NewFakeBackend(""), func() { ln.Close() }) }()

	client, err := DialSession(session, time.Second)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	apk := filepath.Join(t.TempDir(), "a.apk")
	if err := os.WriteFile(apk, []byte("PK\x03\x04 payload"), 0o644); err != nil {
		t.Fatalf("write apk: %v", err)
	}
	resp, err := client.Call(protocol.Request{Cmd: protocol.CmdLaunch, APK: apk}, time.Second)
	if err != nil || !resp.OK {
		t.Fatalf("launch over socket: %v %+v", err, resp)
	}

	// Missing sessions produce actionable errors.
	if _, err := DialSession("no-such-session", 100*time.Millisecond); err == nil || !strings.Contains(err.Error(), "no live session") {
		t.Fatalf("dial missing session: %v", err)
	}

	ln.Close()
	select {
	case <-serveDone:
	case <-time.After(time.Second):
		t.Fatal("Serve did not return after listener close")
	}
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

func TestNewBackendUnknown(t *testing.T) {
	if _, err := NewBackend("emulator", "", ""); err == nil || !strings.Contains(err.Error(), "unknown backend") {
		t.Fatalf("unknown backend: %v", err)
	}
	for _, name := range []string{RuntimeFake, RuntimeATL, RuntimeWineDroid} {
		b, err := NewBackend(name, "", "")
		if err != nil || b == nil {
			t.Fatalf("backend %s: %v", name, err)
		}
	}
}

func TestOversizedResponseIsRejected(t *testing.T) {
	// A real loopback transport, because net.Pipe is unbuffered and would
	// deadlock the two writes we need in flight simultaneously.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen tcp: %v", err)
	}
	defer ln.Close()
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		// Drain the inbound request so the close below is a FIN, not an
		// RST: a reset would mask the size-limit error with a transport
		// error and a truncated final token.
		go func() { _, _ = io.Copy(io.Discard, conn) }()
		payload := bytes.Repeat([]byte("a"), protocol.MaxMessageSize+64)
		payload = append(payload, '\n')
		_ = conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
		_, _ = conn.Write(payload)
	}()
	conn, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatalf("dial tcp: %v", err)
	}
	defer conn.Close()
	c := &Client{conn: conn, br: protocol.NewReader(conn)}
	_, err = c.Call(protocol.Request{Cmd: protocol.CmdSnapshot}, 10*time.Second)
	if err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("expected size-limit error, got %v", err)
	}
}

func TestServeConnEOF(t *testing.T) {
	err := ServeConn(strings.NewReader(""), io.Discard, &recordingBackend{})
	if !errors.Is(err, io.EOF) {
		t.Fatalf("EOF handling: %v", err)
	}
}
