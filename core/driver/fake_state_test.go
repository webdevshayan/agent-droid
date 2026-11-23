package driver

// Internal tests for the fake state path and its advisory lock — these
// reach the unexported pieces the external tests cannot.

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFakeStatePathDefaultIsCacheDir(t *testing.T) {
	// AGD-16: the default state file lives under the user's cache dir, not
	// at a fixed world-known temp path another user could pre-create.
	t.Setenv("AGENTDROID_FAKE_STATE", "")
	p := fakeStatePath()
	if filepath.Base(p) != "fake-state.json" || filepath.Base(filepath.Dir(p)) != "agentdroid" {
		t.Fatalf("default state path %q must be <cache>/agentdroid/fake-state.json", p)
	}
	cache, err := os.UserCacheDir()
	if err == nil && cache != "" {
		if !filepath.IsAbs(p) || filepath.Dir(filepath.Dir(p)) != cache {
			t.Fatalf("default state path %q must sit under the cache dir %s", p, cache)
		}
	}
	// The env override always wins.
	custom := filepath.Join(t.TempDir(), "x.json")
	t.Setenv("AGENTDROID_FAKE_STATE", custom)
	if got := fakeStatePath(); got != custom {
		t.Fatalf("env override ignored: %q != %q", got, custom)
	}
}

func TestStateLockRefcount(t *testing.T) {
	// AGD-17: locks are per path, exclusive across processes, and
	// reference-counted within one process so several Fakes on one path
	// (what tests do) do not deadlock each other.
	p := filepath.Join(t.TempDir(), "state.json")
	l1 := acquireStateLock(p)
	if l1 == nil {
		t.Fatal("first lock acquisition failed")
	}
	l2 := acquireStateLock(p)
	if l2 != l1 {
		t.Fatal("second acquisition must reuse the same lock")
	}
	l1.release() // one of two holders gone — the lock must stay held
	if _, ok := stateLocks.m[p+".lock"]; !ok {
		t.Fatal("lock released while a holder remains")
	}
	l2.release() // last holder gone — lock must be gone
	if _, ok := stateLocks.m[p+".lock"]; ok {
		t.Fatal("lock must be removed after the last release")
	}
	// A fresh acquisition takes the same lock file again.
	l3 := acquireStateLock(p)
	if l3 == nil {
		t.Fatal("re-acquisition after full release failed")
	}
	l3.release()
}
