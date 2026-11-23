package tests_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/webdevshayan/agent-droid/core/cli"
)

func TestParseHappyPaths(t *testing.T) {
	cases := []struct {
		name string
		argv []string
		want cli.Command
	}{
		{"devices", []string{"devices"}, cli.Command{Verb: cli.VerbDevices, Runtime: cli.RuntimeAdb}},
		{"snapshot", []string{"snapshot"}, cli.Command{Verb: cli.VerbSnapshot, Runtime: cli.RuntimeAdb}},
		{"snapshot fake", []string{"--runtime", "fake", "snapshot"}, cli.Command{Verb: cli.VerbSnapshot, Runtime: cli.RuntimeFake}},
		{"snapshot xml", []string{"snapshot", "--xml"}, cli.Command{Verb: cli.VerbSnapshot, Runtime: cli.RuntimeAdb, XML: true}},
		{"launch", []string{"launch", "demo.apk"}, cli.Command{Verb: cli.VerbLaunch, Runtime: cli.RuntimeAdb, Path: "demo.apk"}},
		{"launch fake", []string{"--runtime", "fake", "launch", "demo.apk"}, cli.Command{Verb: cli.VerbLaunch, Runtime: cli.RuntimeFake, Path: "demo.apk"}},
		{"install alias", []string{"install", "app.apk"}, cli.Command{Verb: cli.VerbInstall, Runtime: cli.RuntimeAdb, Path: "app.apk"}},
		{"close", []string{"close"}, cli.Command{Verb: cli.VerbClose, Runtime: cli.RuntimeAdb}},
		{"close fake", []string{"--runtime", "fake", "close"}, cli.Command{Verb: cli.VerbClose, Runtime: cli.RuntimeFake}},
		{"tap", []string{"tap", "@e1"}, cli.Command{Verb: cli.VerbTap, Runtime: cli.RuntimeAdb, Ref: "@e1"}},
		{"tap big ref", []string{"tap", "@e123456789"}, cli.Command{Verb: cli.VerbTap, Runtime: cli.RuntimeAdb, Ref: "@e123456789"}},
		{"type joins args", []string{"type", "hello", "world"}, cli.Command{Verb: cli.VerbType, Runtime: cli.RuntimeAdb, Text: "hello world"}},
		{"type single", []string{"type", "hi"}, cli.Command{Verb: cli.VerbType, Runtime: cli.RuntimeAdb, Text: "hi"}},
		{"type at max", []string{"type", strings.Repeat("a", cli.MaxTextLen)}, cli.Command{Verb: cli.VerbType, Runtime: cli.RuntimeAdb, Text: strings.Repeat("a", cli.MaxTextLen)}},
		{"swipe refs", []string{"swipe", "@e1", "@e2"}, cli.Command{Verb: cli.VerbSwipe, Runtime: cli.RuntimeAdb, Ref: "@e1", Ref2: "@e2"}},
		{"swipe coords", []string{"swipe", "0", "0", "1080", "2400"}, cli.Command{Verb: cli.VerbSwipe, Runtime: cli.RuntimeAdb, Coords: []int{0, 0, 1080, 2400}}},
		{"screenshot stdout default", []string{"screenshot"}, cli.Command{Verb: cli.VerbScreenshot, Runtime: cli.RuntimeAdb}},
		{"screenshot out", []string{"screenshot", "--out", "x/a.png"}, cli.Command{Verb: cli.VerbScreenshot, Runtime: cli.RuntimeAdb, Out: "x/a.png"}},
		{"serial before verb", []string{"--serial", "emulator-5554", "tap", "@e1"}, cli.Command{Verb: cli.VerbTap, Runtime: cli.RuntimeAdb, Serial: "emulator-5554", Ref: "@e1"}},
		{"serial short flag", []string{"tap", "-s", "dev1", "@e1"}, cli.Command{Verb: cli.VerbTap, Runtime: cli.RuntimeAdb, Serial: "dev1", Ref: "@e1"}},
		{"clear", []string{"clear", "@e3"}, cli.Command{Verb: cli.VerbClear, Runtime: cli.RuntimeAdb, Ref: "@e3"}},
		{"back", []string{"back"}, cli.Command{Verb: cli.VerbBack, Runtime: cli.RuntimeAdb}},
		{"home", []string{"home"}, cli.Command{Verb: cli.VerbHome, Runtime: cli.RuntimeAdb}},
		{"dashdash payload", []string{"type", "--", "--looks-like-flag"}, cli.Command{Verb: cli.VerbType, Runtime: cli.RuntimeAdb, Text: "--looks-like-flag"}},
		{"help word", []string{"help"}, cli.Command{Verb: cli.VerbHelp, Runtime: cli.RuntimeAdb}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := cli.Parse(tc.argv)
			if err != nil {
				t.Fatalf("Parse(%q) returned error: %v", tc.argv, err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("Parse(%q) = %+v, want %+v", tc.argv, got, tc.want)
			}
		})
	}
}

func TestParseErrors(t *testing.T) {
	cases := []struct {
		name   string
		argv   []string
		substr string
	}{
		{"empty", nil, "no command"},
		{"unknown command", []string{"frobnicate"}, "unknown command"},
		{"launch no path", []string{"launch"}, "exactly one APK path"},
		{"launch empty path", []string{"launch", ""}, "path is empty"},
		{"launch extra", []string{"launch", "a.apk", "b.apk"}, "exactly one APK path"},
		{"close extra", []string{"close", "x"}, "close takes no arguments"},
		{"runtime unknown", []string{"--runtime", "wine", "snapshot"}, "unknown runtime"},
		{"runtime missing value", []string{"snapshot", "--runtime"}, "requires a value"},
		{"runtime duplicate", []string{"--runtime", "adb", "--runtime", "fake", "snapshot"}, "duplicate"},
		{"fake devices", []string{"--runtime", "fake", "devices"}, "devices applies to the adb runtime"},
		{"fake serial", []string{"--runtime", "fake", "--serial", "x", "tap", "@e1"}, "--serial applies to the adb runtime"},
		{"tap missing ref", []string{"tap"}, "exactly one"},
		{"tap extra arg", []string{"tap", "@e1", "x"}, "exactly one"},
		{"tap zero ref", []string{"tap", "@e0"}, "invalid element ref"},
		{"tap non ref", []string{"tap", "e1"}, "tap: invalid element ref"},
		{"tap huge ref", []string{"tap", "@e99999999999999999999"}, "index too large"},
		{"tap letter ref", []string{"tap", "@exx"}, "invalid element ref"},
		{"type no text", []string{"type"}, "type needs text"},
		{"type empty", []string{"type", ""}, "text is empty"},
		{"type blank", []string{"type", "   "}, "text is empty"},
		{"type too long", []string{"type", strings.Repeat("a", cli.MaxTextLen+1)}, "over the"},
		{"type multi-arg too long is rejected before joining", []string{"type", strings.Repeat("a", cli.MaxTextLen/2), strings.Repeat("b", cli.MaxTextLen/2+2)}, "over the"},
		{"negative number is a positional, not a flag", []string{"swipe", "-5", "0", "-10", "1"}, "coordinate -5 out of range"},
		{"type many args too long", []string{"type", strings.Repeat("a", 2000), strings.Repeat("b", 3000)}, "over the"},
		{"install empty path", []string{"install", ""}, "path is empty"},
		{"install no path", []string{"install"}, "exactly one APK path"},
		{"install extra", []string{"install", "a.apk", "b.apk"}, "exactly one APK path"},
		{"swipe mixed", []string{"swipe", "@e1", "2"}, "invalid element ref"},
		{"swipe three coords", []string{"swipe", "1", "2", "3"}, "swipe wants"},
		{"swipe five coords", []string{"swipe", "1", "2", "3", "4", "5"}, "swipe wants"},
		{"swipe coord not int", []string{"swipe", "x", "0", "1", "1"}, "not an integer"},
		{"swipe coord int64 overflow", []string{"swipe", "999999999999999999999999", "0", "1", "1"}, "not an integer"},
		{"swipe coord too big", []string{"swipe", "200000", "0", "1", "1"}, "out of range"},
		{"swipe negative coordinate reaches the range check", []string{"swipe", "-1", "0", "1", "1"}, "coordinate -1 out of range"},
		{"swipe negative via explicit dashdash", []string{"swipe", "--", "-1", "0", "1", "1"}, "coordinate -1 out of range"},
		{"unknown flag", []string{"--frobnicate"}, "unknown flag"},
		{"unknown short flag", []string{"tap", "-x", "@e1"}, "unknown flag"},
		{"xml on tap", []string{"tap", "--xml", "@e1"}, "--xml only applies"},
		{"out on snapshot", []string{"snapshot", "--out", "a.png"}, "--out only applies"},
		{"serial missing value", []string{"tap", "--serial"}, "requires a value"},
		{"serial empty value", []string{"tap", "--serial", "", "@e1"}, "non-empty"},
		{"serial on help", []string{"--serial", "x", "help"}, "--serial applies to the adb runtime"},
		{"duplicate serial", []string{"-s", "a", "-s", "b", "devices"}, "duplicate"},
		{"duplicate out", []string{"screenshot", "--out", "a", "--out", "b"}, "duplicate"},
		{"out missing value", []string{"screenshot", "--out"}, "requires a value"},
		{"out empty value", []string{"screenshot", "--out", ""}, "non-empty"},
		{"devices extra", []string{"devices", "x"}, "no arguments"},
		{"snapshot positional", []string{"snapshot", "x"}, "no positional"},
		{"back extra", []string{"back", "x"}, "takes no arguments"},
		{"clear bad ref", []string{"clear", "@x1"}, "invalid element ref"},
		{"help with serial", []string{"help", "--serial", "x"}, "--serial applies to the adb runtime"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := cli.Parse(tc.argv)
			if err == nil {
				t.Fatalf("Parse(%q) = nil error, want error containing %q", tc.argv, tc.substr)
			}
			if !strings.Contains(err.Error(), tc.substr) {
				t.Fatalf("Parse(%q) error = %q, want it to contain %q", tc.argv, err, tc.substr)
			}
		})
	}
}

func TestParseHelpShortCircuits(t *testing.T) {
	// Help and version flags win over everything else, including garbage.
	for _, argv := range [][]string{
		{"-h"},
		{"--help"},
		{"tap", "--help"},
		{"tap", "@e1", "--help", "--frobnicate"},
		{"--runtime", "fake", "snapshot", "--help"},
	} {
		cmd, err := cli.Parse(argv)
		if err != nil {
			t.Fatalf("Parse(%q) error: %v", argv, err)
		}
		if cmd.Verb != cli.VerbHelp {
			t.Fatalf("Parse(%q) verb = %q, want help", argv, cmd.Verb)
		}
	}
	for _, argv := range [][]string{{"--version"}, {"tap", "--version"}, {"type", "--version", "junk"}} {
		cmd, err := cli.Parse(argv)
		if err != nil {
			t.Fatalf("Parse(%q) error: %v", argv, err)
		}
		if cmd.Verb != cli.VerbVersion {
			t.Fatalf("Parse(%q) verb = %q, want version", argv, cmd.Verb)
		}
	}
}
