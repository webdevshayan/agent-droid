package tests_test

import (
	"strings"
	"testing"

	"github.com/webdevshayan/agent-droid/core/cli"
	"github.com/webdevshayan/agent-droid/core/snapshot"
)

// FuzzParseArgs throws arbitrary argument vectors at the CLI parser.
// Invariants: Parse never panics; on success the verb is a known one, the
// type payload respects the length cap and is never blank, refs parse
// back, swipe coordinates stay inside range, and the runtime is valid.
func FuzzParseArgs(f *testing.F) {
	for _, s := range [][4]string{
		{"tap", "@e1", "", ""},
		{"tap", "@e999999999999999999999", "", ""},
		{"type", "hello world", "", ""},
		{"type", "", "", ""},
		{"type", "--", "--looks-like-flag", ""},
		{"swipe", "0", "0", "1080"},
		{"swipe", "@e1", "@e2", ""},
		{"--runtime", "fake", "snapshot", "--xml"},
		{"--runtime", "bogus", "snapshot", ""},
		{"launch", "demo.apk", "", ""},
		{"launch", "", "", ""},
		{"close", "", "", ""},
		{"install", "/tmp/app.apk", "", ""},
		{"install", "", "", ""},
		{"screenshot", "--out", "s.png", ""},
		{"clear", "@e12", "", ""},
		{"--help", "", "", ""},
		{"--version", "", "", ""},
		{"bogus", "", "", ""},
		{"--bogus", "", "", ""},
		{"tap", "--xml", "@e1", ""},
		{"type", "@e1 @e2 ; rm -rf /", "", ""},
		{"back", "x", "", ""},
		{"", "", "", ""},
	} {
		f.Add(s[0], s[1], s[2], s[3])
	}
	f.Fuzz(func(t *testing.T, a, b, c, d string) {
		args := []string{a, b, c, d}
		cmd, err := cli.Parse(args)
		if err != nil {
			return // errors are fine; panics are not
		}
		switch cmd.Verb {
		case cli.VerbDevices, cli.VerbLaunch, cli.VerbClose, cli.VerbInstall, cli.VerbSnapshot,
			cli.VerbTap, cli.VerbType, cli.VerbSwipe, cli.VerbScreenshot, cli.VerbClear,
			cli.VerbBack, cli.VerbHome, cli.VerbHelp, cli.VerbVersion:
		default:
			t.Fatalf("Parse(%q) returned unknown verb %q", args, cmd.Verb)
		}
		if cmd.Runtime != cli.RuntimeAdb && cmd.Runtime != cli.RuntimeFake {
			t.Fatalf("Parse(%q) returned unknown runtime %q", args, cmd.Runtime)
		}
		if cmd.Verb == cli.VerbType {
			if len(cmd.Text) > cli.MaxTextLen {
				t.Fatalf("type payload escaped the length cap: %d > %d", len(cmd.Text), cli.MaxTextLen)
			}
			if strings.TrimSpace(cmd.Text) == "" {
				t.Fatalf("type accepted blank text %q", cmd.Text)
			}
		}
		if cmd.Ref != "" {
			if _, err := snapshot.ParseRef(cmd.Ref); err != nil {
				t.Fatalf("Parse emitted invalid Ref %q: %v", cmd.Ref, err)
			}
		}
		if cmd.Ref2 != "" {
			if _, err := snapshot.ParseRef(cmd.Ref2); err != nil {
				t.Fatalf("Parse emitted invalid Ref2 %q: %v", cmd.Ref2, err)
			}
		}
		if len(cmd.Coords) != 0 {
			if len(cmd.Coords) != 4 {
				t.Fatalf("Coords must be nil or length 4, got %d", len(cmd.Coords))
			}
			for _, n := range cmd.Coords {
				if n < 0 || n > cli.MaxCoord {
					t.Fatalf("coordinate escaped range: %d", n)
				}
			}
		}
		if cmd.Verb == cli.VerbDevices && cmd.Runtime == cli.RuntimeFake {
			t.Fatal("devices must be rejected under the fake runtime")
		}
	})
}
