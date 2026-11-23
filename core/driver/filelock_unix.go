//go:build unix

package driver

import (
	"os"
	"syscall"
)

// flockExclusive takes an exclusive advisory lock on the file. Supported on
// every Unix; the sidecar lock file makes concurrent fake-state writers
// serialize instead of last-write-winning.
func flockExclusive(f *os.File) error {
	return syscall.Flock(int(f.Fd()), syscall.LOCK_EX)
}

// funlock releases the advisory lock.
func funlock(f *os.File) error {
	return syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
}
