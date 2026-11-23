//go:build linux

package runtime

import (
	"net"
	"os"
	"syscall"
)

// peerUID returns the uid of the process at the other end of a Unix socket
// connection, via SO_PEERCRED — the kernel does the authentication, so no
// credential exchange is needed in the protocol itself. ok is false for
// non-Unix connections (net.Pipe, tests) or when the socket option is
// unavailable; callers must not lock anyone out in that case.
func peerUID(conn net.Conn) (uid uint32, ok bool) {
	uc, isUnix := conn.(*net.UnixConn)
	if !isUnix {
		return 0, false
	}
	raw, err := uc.SyscallConn()
	if err != nil {
		return 0, false
	}
	var cred *syscall.Ucred
	ctrlErr := raw.Control(func(fd uintptr) {
		cred, err = syscall.GetsockoptUcred(int(fd), syscall.SOL_SOCKET, syscall.SO_PEERCRED)
	})
	if ctrlErr != nil || err != nil || cred == nil {
		return 0, false
	}
	return cred.Uid, true
}

// sameUser reports whether conn comes from a process of the same OS user as
// this daemon. Uncheckable connections count as same-user: the check exists
// to keep other users out, not to lock out clients on platforms without
// SO_PEERCRED.
func sameUser(conn net.Conn) bool {
	uid, ok := peerUID(conn)
	if !ok {
		return true
	}
	return uid == uint32(os.Getuid())
}
