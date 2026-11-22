package tests_test

// Session-mode parser surface: serve, --session, --backend, --stdio,
// --runtime-bin, and the atl/winedroid runtime values. The parser is the
// gate: every malformed invocation must fail here with exit code 2
// semantics (an error, never a panic), and every valid one must parse.

import (
	"fmt"
	"strings"
	"testing"

	"github.com/webdevshayan/agent-droid/core/cli"
)

func TestParseSessionModeHappyPaths(t *testing.T) {
	cases := []struct {
		name string
		argv []string
		want func(c cli.Command) error
	}{
		{"launch fake session", []string{"launch", "demo.apk", "--runtime", "fake", "--session", "s1"}, func(c cli.Command) error {
			if c.Verb != cli.VerbLaunch || c.Session != "s1" || c.Runtime != cli.RuntimeFake || c.Path != "demo.apk" {
				return fmt.Errorf("launch parse: %+v", c)
			}
			return nil
		}},
		{"launch atl session", []string{"--runtime", "atl", "--session", "s2", "launch", "app.apk"}, func(c cli.Command) error {
			if c.Session != "s2" || c.Runtime != cli.RuntimeATL {
				return fmt.Errorf("atl parse: %+v", c)
			}
			return nil
		}},
		{"launch winedroid session", []string{"launch", "app.apk", "--runtime", "winedroid", "--session", "s3", "--runtime-bin", "/opt/adapter"}, func(c cli.Command) error {
			if c.Runtime != cli.RuntimeWineDroid || c.RuntimeBin != "/opt/adapter" {
				return fmt.Errorf("winedroid parse: %+v", c)
			}
			return nil
		}},
		{"attach snapshot", []string{"snapshot", "--session", "s1"}, func(c cli.Command) error {
			if c.Verb != cli.VerbSnapshot || c.Session != "s1" || c.Runtime != "" {
				return fmt.Errorf("attach snapshot: %+v", c)
			}
			return nil
		}},
		{"attach tap", []string{"--session", "s1", "tap", "@e3"}, func(c cli.Command) error {
			if c.Verb != cli.VerbTap || c.Ref != "@e3" || c.Session != "s1" {
				return fmt.Errorf("attach tap: %+v", c)
			}
			return nil
		}},
		{"attach type", []string{"type", "hello world", "--session", "s1"}, func(c cli.Command) error {
			if c.Verb != cli.VerbType || c.Text != "hello world" || c.Session != "s1" {
				return fmt.Errorf("attach type: %+v", c)
			}
			return nil
		}},
		{"attach screenshot xml-independent", []string{"screenshot", "--session", "s1"}, func(c cli.Command) error {
			if c.Verb != cli.VerbScreenshot || c.Session != "s1" {
				return fmt.Errorf("attach screenshot: %+v", c)
			}
			return nil
		}},
		{"attach close", []string{"close", "--session", "s1"}, func(c cli.Command) error {
			if c.Verb != cli.VerbClose || c.Session != "s1" {
				return fmt.Errorf("attach close: %+v", c)
			}
			return nil
		}},
		{"serve socket mode", []string{"serve", "--backend", "winedroid", "--session", "s9"}, func(c cli.Command) error {
			if c.Verb != cli.VerbServe || c.Backend != "winedroid" || c.Session != "s9" {
				return fmt.Errorf("serve parse: %+v", c)
			}
			return nil
		}},
		{"serve stdio", []string{"serve", "--backend", "fake", "--stdio"}, func(c cli.Command) error {
			if c.Verb != cli.VerbServe || !c.Stdio || c.Backend != "fake" {
				return fmt.Errorf("serve stdio parse: %+v", c)
			}
			return nil
		}},
	}
	for _, tc := range cases {
		c, err := cli.Parse(tc.argv)
		if err != nil {
			t.Fatalf("%s: unexpected error: %v", tc.name, err)
		}
		if err := tc.want(c); err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
	}
}

func TestParseSessionModeErrors(t *testing.T) {
	cases := []struct {
		name string
		argv []string
		want string
	}{
		{"launch session without runtime", []string{"launch", "demo.apk", "--session", "s1"}, "requires --runtime fake, atl or winedroid"},
		{"launch session with adb", []string{"launch", "demo.apk", "--runtime", "adb", "--session", "s1"}, "requires --runtime fake, atl or winedroid"},
		{"atl without session", []string{"launch", "demo.apk", "--runtime", "atl"}, "persistent sessions"},
		{"winedroid without session", []string{"install", "demo.apk", "--runtime", "winedroid"}, "persistent sessions"},
		{"attach with adb runtime", []string{"snapshot", "--runtime", "adb", "--session", "s1"}, "drop --runtime"},
		{"devices with session", []string{"devices", "--session", "s1"}, "adb runtime only"},
		{"swipe in session", []string{"swipe", "@e1", "@e2", "--session", "s1"}, "not supported in session mode"},
		{"clear in session", []string{"clear", "@e1", "--session", "s1"}, "not supported in session mode"},
		{"back in session", []string{"back", "--session", "s1"}, "not supported in session mode"},
		{"home in session", []string{"home", "--session", "s1"}, "not supported in session mode"},
		{"session with help", []string{"help", "--session", "s1"}, "does not apply to help"},
		{"serial with session", []string{"tap", "@e1", "--serial", "X", "--session", "s1"}, "adb runtime only"},
		{"serve without backend", []string{"serve", "--session", "s1"}, "--backend is required"},
		{"serve unknown backend", []string{"serve", "--backend", "qemu", "--session", "s1"}, "unknown backend"},
		{"serve adb backend", []string{"serve", "--backend", "adb", "--session", "s1"}, "unknown backend"},
		{"serve without session or stdio", []string{"serve", "--backend", "fake"}, "--session is required"},
		{"serve with runtime flag", []string{"serve", "--backend", "fake", "--runtime", "fake", "--session", "s1"}, "--backend, not --runtime"},
		{"backend outside serve", []string{"snapshot", "--backend", "fake"}, "--backend only applies to serve"},
		{"stdio outside serve", []string{"snapshot", "--stdio"}, "--stdio only applies to serve"},
		{"runtime-bin outside serve/launch", []string{"snapshot", "--runtime-bin", "/x"}, "--runtime-bin only applies"},
		{"unknown runtime value", []string{"--runtime", "qemu", "snapshot"}, "unknown runtime"},
		{"empty session value", []string{"snapshot", "--session", ""}, "non-empty"},
		{"empty backend value", []string{"serve", "--backend", ""}, "non-empty"},
		{"duplicate session", []string{"snapshot", "--session", "a", "--session", "b"}, "duplicate flag --session"},
	}
	for _, tc := range cases {
		_, err := cli.Parse(tc.argv)
		if err == nil {
			t.Fatalf("%s: expected a usage error, got none", tc.name)
		}
		if !strings.Contains(err.Error(), tc.want) {
			t.Fatalf("%s: error %q does not mention %q", tc.name, err, tc.want)
		}
	}
}
