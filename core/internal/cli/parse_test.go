package cli

import (
	"errors"
	"testing"
	"time"
)

func TestParseLaunch(t *testing.T) {
	cmd, err := Parse([]string{"launch", "demo.apk"})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if cmd.Name != "launch" || cmd.APK != "demo.apk" {
		t.Fatalf("unexpected command: %+v", cmd)
	}
	// Defaults: atl runtime, default session, 15s timeout.
	if cmd.Runtime != RuntimeATL || cmd.Session != DefaultSession || cmd.Timeout != DefaultTimeout {
		t.Fatalf("unexpected defaults: %+v", cmd.Global)
	}
}

func TestParseFlagsAnyOrder(t *testing.T) {
	cmd, err := Parse([]string{"--runtime=fake", "--session", "s1", "launch", "a.apk", "--json"})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if cmd.Runtime != RuntimeFake || cmd.Session != "s1" || !cmd.JSON || cmd.APK != "a.apk" {
		t.Fatalf("unexpected command: %+v", cmd)
	}
}

func TestParseTapAndType(t *testing.T) {
	cmd, err := Parse([]string{"tap", "@e4"})
	if err != nil {
		t.Fatalf("parse tap: %v", err)
	}
	if cmd.Ref != "@e4" {
		t.Fatalf("unexpected ref: %+v", cmd)
	}
	cmd, err = Parse([]string{"type", "@e2", "shayan@example.com"})
	if err != nil {
		t.Fatalf("parse type: %v", err)
	}
	if cmd.Ref != "@e2" || cmd.Text != "shayan@example.com" {
		t.Fatalf("unexpected type command: %+v", cmd)
	}
	// Multi-word text joins the trailing positionals.
	cmd, err = Parse([]string{"type", "@e2", "hello", "world"})
	if err != nil {
		t.Fatalf("parse multi-word type: %v", err)
	}
	if cmd.Text != "hello world" {
		t.Fatalf("text join: %q", cmd.Text)
	}
}

func TestParseServe(t *testing.T) {
	cmd, err := Parse([]string{"serve", "--backend", "fake", "--stdio"})
	if err != nil {
		t.Fatalf("parse serve: %v", err)
	}
	if cmd.Backend != RuntimeFake || !cmd.Stdio {
		t.Fatalf("unexpected serve command: %+v", cmd)
	}
	// Backend defaults to fake when omitted.
	cmd, err = Parse([]string{"serve"})
	if err != nil {
		t.Fatalf("parse bare serve: %v", err)
	}
	if cmd.Backend != RuntimeFake {
		t.Fatalf("serve backend default: %+v", cmd)
	}
}

func TestParseTerminator(t *testing.T) {
	cmd, err := Parse([]string{"type", "@e2", "--", "--json-looking-text"})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if cmd.Text != "--json-looking-text" {
		t.Fatalf("terminator handling: %+v", cmd)
	}
}

func TestParseHelpAndVersion(t *testing.T) {
	cases := []struct {
		args []string
		want string
	}{
		{[]string{"help"}, "help"},
		{[]string{"--help"}, "help"},
		{[]string{"version"}, "version"},
	}
	for _, tc := range cases {
		cmd, err := Parse(tc.args)
		if err != nil {
			t.Fatalf("parse %v: %v", tc.args, err)
		}
		if cmd.Name != tc.want {
			t.Fatalf("parse %v: name %q, want %q", tc.args, cmd.Name, tc.want)
		}
	}
}

func TestParseEnvDefaults(t *testing.T) {
	t.Setenv("AGENTDROID_RUNTIME", "fake")
	t.Setenv("AGENTDROID_SESSION", "envsession")
	cmd, err := Parse([]string{"snapshot"})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if cmd.Runtime != RuntimeFake || cmd.Session != "envsession" {
		t.Fatalf("env defaults ignored: %+v", cmd.Global)
	}
}

func TestParseTimeoutFlag(t *testing.T) {
	cmd, err := Parse([]string{"snapshot", "--timeout", "500ms"})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if cmd.Timeout != 500*time.Millisecond {
		t.Fatalf("timeout: %v", cmd.Timeout)
	}
}

func TestParseErrors(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want string
	}{
		{"no args", []string{}, "no command given"},
		{"unknown command", []string{"deploy"}, "unknown command"},
		{"launch missing apk", []string{"launch"}, "exactly one APK path"},
		{"launch two apks", []string{"launch", "a.apk", "b.apk"}, "exactly one APK path"},
		{"tap missing ref", []string{"tap"}, "exactly one element ref"},
		{"tap bad ref", []string{"tap", "e1"}, "invalid element ref"},
		{"type missing text", []string{"type", "@e1"}, "ref and the text"},
		{"type bad ref", []string{"type", "@x1", "hi"}, "invalid element ref"},
		{"snapshot with args", []string{"snapshot", "extra"}, "takes no arguments"},
		{"unknown flag", []string{"snapshot", "--wat"}, "unknown flag --wat"},
		{"flag missing value", []string{"launch", "--runtime"}, "requires a value"},
		{"bad runtime", []string{"launch", "a.apk", "--runtime", "emulator"}, "unknown runtime"},
		{"bad session", []string{"snapshot", "--session", "bad name"}, "invalid session name"},
		{"bad timeout", []string{"snapshot", "--timeout", "5"}, "invalid --timeout"},
		{"bad backend", []string{"serve", "--backend", "emulator"}, "unknown backend"},
		{"unknown serve flag", []string{"serve", "--port", "9"}, "unknown flag --port"},
	}
	for _, tc := range cases {
		_, err := Parse(tc.args)
		if err == nil {
			t.Fatalf("%s: expected error", tc.name)
		}
		var ue *UsageError
		if !errors.As(err, &ue) {
			t.Fatalf("%s: error is not a UsageError: %v", tc.name, err)
		}
		if !contains(err.Error(), tc.want) {
			t.Fatalf("%s: error %q does not mention %q", tc.name, err, tc.want)
		}
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (func() bool {
		for i := 0; i+len(sub) <= len(s); i++ {
			if s[i:i+len(sub)] == sub {
				return true
			}
		}
		return false
	})()
}

func TestUsageTextMentionsEveryCommand(t *testing.T) {
	u := Usage()
	for _, want := range []string{"launch", "snapshot", "tap", "type", "screenshot", "close", "serve", "version", "--runtime", "--session", "--json"} {
		if !contains(u, want) {
			t.Fatalf("usage text missing %q", want)
		}
	}
}
