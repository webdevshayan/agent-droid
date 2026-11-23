//go:build !unix

package driver

import "os"

// flockExclusive is a no-op where flock does not exist (e.g. Windows): the
// fake degrades to the historical unlocked behavior on those platforms.
func flockExclusive(f *os.File) error { return nil }

// funlock is the matching no-op.
func funlock(f *os.File) error { return nil }
