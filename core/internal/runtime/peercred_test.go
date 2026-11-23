package runtime

// Trust-boundary gates for the session socket: the kernel-enforced same-user
// check must never lock out same-user clients or uncheckable transports.

import (
	"net"
	"path/filepath"
	"testing"
)

func TestSameUserAllowsNetPipe(t *testing.T) {
	// net.Pipe conns carry no peer credentials: the fallback must stay
	// open (tests, embedders and non-Linux hosts rely on it).
	a, b := net.Pipe()
	defer a.Close()
	defer b.Close()
	if !sameUser(a) || !sameUser(b) {
		t.Fatal("uncheckable transports must not be locked out")
	}
}

func TestSameUserOwnUnixSocket(t *testing.T) {
	// A real filesystem Unix socket: the accepted peer is this same test
	// process, so sameUser must accept it.
	dir := t.TempDir()
	ln, err := net.Listen("unix", filepath.Join(dir, "s"))
	if err != nil {
		t.Skipf("unix listen unavailable: %v", err)
	}
	defer ln.Close()

	result := make(chan bool, 1)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			result <- false
			return
		}
		defer conn.Close()
		result <- sameUser(conn)
	}()

	c, err := net.Dial("unix", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if !<-result {
		t.Fatal("same-process connections must pass the ownership check")
	}
}
