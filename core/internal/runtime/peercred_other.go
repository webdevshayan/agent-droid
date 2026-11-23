//go:build !linux

package runtime

import "net"

// sameUser is the non-Linux fallback: SO_PEERCRED does not exist there, so
// no ownership check is possible and the historical open behavior applies
// (documented trust boundary: local same-machine callers are trusted).
func sameUser(conn net.Conn) bool { return true }
