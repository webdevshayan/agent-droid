package tests_test

// The hacking phase: memory-abuse tests. These throw zero-byte and
// half-gigabyte payloads at the `type` pipeline and oversized junk at the
// snapshot parser, and assert three properties for each:
//
//  1. the input is rejected (never accepted, never a panic),
//  2. the rejection is linear-time (a generous wall-clock bound),
//  3. the rejection does not multiply the payload in memory
//     (cumulative allocation stays O(1) in the payload).
//
// argv can never carry a 500 MB string (the kernel caps exec arguments),
// so these tests hit the same layers a real invocation would: cli.Parse
// and device.ValidateText, exactly the functions `type` flows through.

import (
	"fmt"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/webdevshayan/agent-droid/core/cli"
	"github.com/webdevshayan/agent-droid/core/device"
	"github.com/webdevshayan/agent-droid/core/snapshot"
)

// allocSince returns a function measuring cumulative bytes allocated since
// the call. TotalAlloc is monotonic, so this is exact, not GC-dependent.
func allocSince(t *testing.T) func() uint64 {
	t.Helper()
	var before runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	return func() uint64 {
		var after runtime.MemStats
		runtime.ReadMemStats(&after)
		if after.TotalAlloc < before.TotalAlloc {
			return 0
		}
		return after.TotalAlloc - before.TotalAlloc
	}
}

func TestHackingTypeZeroBytes(t *testing.T) {
	for name, argv := range map[string][]string{
		"no text at all": {"type"},
		"empty string":   {"type", ""},
		"blank spaces":   {"type", "   "},
	} {
		_, err := cli.Parse(argv)
		if err == nil {
			t.Fatalf("type with %s must be rejected", name)
		}
	}
}

func TestHackingType500MB(t *testing.T) {
	if testing.Short() {
		t.Skip("memory phase needs the full run")
	}
	const size = 500 << 20
	huge := strings.Repeat("a", size)
	measure := allocSince(t)
	start := time.Now()

	_, err := cli.Parse([]string{"type", huge})

	elapsed := time.Since(start)
	allocated := measure()

	if err == nil {
		t.Fatal("a 500 MB type payload must be rejected")
	}
	if !strings.Contains(err.Error(), "over the") {
		t.Fatalf("rejection should cite the length cap, got: %v", err)
	}
	if elapsed > 10*time.Second {
		t.Fatalf("rejecting 500 MB took %v — the parser is doing super-linear work", elapsed)
	}
	if allocated > 16<<20 {
		t.Fatalf("rejecting 500 MB allocated %d bytes (%s) — the payload is being copied or multiplied", allocated, humanAlloc(allocated))
	}

	// Same abuse at the device validation layer, the second gate `type`
	// passes through before adb runs.
	measure = allocSince(t)
	start = time.Now()
	_, err = device.ValidateText(huge)
	elapsed = time.Since(start)
	allocated = measure()

	if err == nil {
		t.Fatal("device layer must also reject a 500 MB payload")
	}
	if elapsed > 10*time.Second {
		t.Fatalf("device-layer rejection took %v", elapsed)
	}
	if allocated > 16<<20 {
		t.Fatalf("device-layer rejection allocated %d bytes (%s)", allocated, humanAlloc(allocated))
	}
}

func TestHackingTypeHalfGBSpaces(t *testing.T) {
	if testing.Short() {
		t.Skip("memory phase needs the full run")
	}
	spaces := strings.Repeat(" ", 500<<20)
	measure := allocSince(t)
	_, err := cli.Parse([]string{"type", spaces})
	if err == nil {
		t.Fatal("a half-gigabyte of whitespace must be rejected")
	}
	if allocated := measure(); allocated > 16<<20 {
		t.Fatalf("whitespace rejection allocated %d bytes (%s) — no scan or trim copy is allowed before the length cap", allocated, humanAlloc(allocated))
	}
}

func TestHackingTypeNearLimitIsFast(t *testing.T) {
	// Just under the cap: the payload must still flow through the parser
	// untouched (no quadratic joining on the happy path).
	payload := strings.Repeat("a", cli.MaxTextLen)
	measure := allocSince(t)
	cmd, err := cli.Parse([]string{"type", payload})
	if err != nil || cmd.Text != payload {
		t.Fatalf("payload at the cap must parse verbatim, got err=%v", err)
	}
	if allocated := measure(); allocated > uint64(2*cli.MaxTextLen) {
		t.Fatalf("parsing a max-length payload allocated %d bytes; at most one copy is allowed", allocated)
	}
}

func TestHackingSnapshotOversize(t *testing.T) {
	if testing.Short() {
		t.Skip("memory phase needs the full run")
	}
	for _, size := range []int{snapshot.MaxSnapshotBytes + 1, 500 << 20} {
		name := humanAlloc(uint64(size))
		t.Run(name, func(t *testing.T) {
			junk := make([]byte, size)
			measure := allocSince(t)
			start := time.Now()

			_, err := snapshot.Parse(junk)

			if err == nil {
				t.Fatalf("a %s dump must be rejected", name)
			}
			if !strings.Contains(err.Error(), "limit") {
				t.Fatalf("rejection should cite the size limit, got: %v", err)
			}
			if elapsed := time.Since(start); elapsed > 10*time.Second {
				t.Fatalf("size-guard rejection took %v", elapsed)
			}
			if allocated := measure(); allocated > 8<<20 {
				t.Fatalf("size-guard allocated %d bytes (%s) — nothing should be parsed or copied before the cap", allocated, humanAlloc(allocated))
			}
			junk = nil
			runtime.GC()
		})
	}
}

func TestHackingSnapshotBombShapes(t *testing.T) {
	// Element-count bomb: 50000 tiny tappable nodes must hit the cap.
	var b strings.Builder
	b.WriteString("<hierarchy>")
	for i := 0; i < 50000; i++ {
		b.WriteString(`<node bounds="[0,0][1,1]"/>`)
	}
	b.WriteString("</hierarchy>")
	if _, err := snapshot.Parse([]byte(b.String())); err == nil || !strings.Contains(err.Error(), "more than") {
		t.Fatalf("element bomb must hit the cap, got %v", err)
	}

	// Nesting bomb: 200 levels of <node> must hit the depth cap, not the
	// Go stack.
	deep := "<hierarchy>" + strings.Repeat("<node>", 200) + strings.Repeat("</node>", 200) + "</hierarchy>"
	_, err := snapshot.Parse([]byte(deep))
	if err == nil || !strings.Contains(err.Error(), "nesting deeper") {
		t.Fatalf("deeply nested XML must hit the depth cap, got %v", err)
	}
	// Both cap rejections and XML errors are acceptable here; the key
	// property is: no stack overflow, no panic, no hang.
}

func humanAlloc(n uint64) string {
	switch {
	case n >= 1<<30:
		return fmt.Sprintf("%.1f GiB", float64(n)/(1<<30))
	case n >= 1<<20:
		return fmt.Sprintf("%.0f MiB", float64(n)/(1<<20))
	case n >= 1024:
		return fmt.Sprintf("%.1f KiB", float64(n)/1024)
	default:
		return fmt.Sprintf("%d B", n)
	}
}
