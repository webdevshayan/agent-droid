// Package cli implements the agentdroid command line: argument parsing for
// the session commands plus serve/version/help, and their execution against
// a runtime adapter session.
package cli

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/webdevshayan/agent-droid/core/internal/runtime"
	"github.com/webdevshayan/agent-droid/core/internal/uitree"
)

// Version of the standalone CLI and orchestrator (Account 1 scope).
const Version = "0.1.0"

// Runtime choices and the default session, re-exported from the runtime
// package for flag validation.
const (
	RuntimeATL       = runtime.RuntimeATL
	RuntimeWineDroid = runtime.RuntimeWineDroid
	RuntimeFake      = runtime.RuntimeFake
	DefaultSession   = runtime.DefaultSession
)

// Defaults for per-command request timeouts and the launch handshake.
const (
	DefaultTimeout        = 15 * time.Second
	DefaultLaunchDeadline = 30 * time.Second
)

// Global carries the flags shared by every command.
type Global struct {
	Runtime    string
	RuntimeBin string
	Session    string
	Timeout    time.Duration
	JSON       bool
	Tree       bool
}

// Command is one parsed agentdroid invocation.
type Command struct {
	Name    string // launch|snapshot|tap|type|screenshot|close|serve|version|help
	APK     string
	Ref     string
	Text    string
	Backend string // serve --backend
	Stdio   bool   // serve --stdio
	Global
}

// UsageError marks a bad invocation: it exits with code 2, while a failed
// operation on a live session exits with code 1.
type UsageError struct{ Err error }

func (e *UsageError) Error() string { return e.Err.Error() }
func (e *UsageError) Unwrap() error { return e.Err }

func usagef(format string, a ...any) error {
	return &UsageError{Err: fmt.Errorf(format, a...)}
}

// Usage returns the full help text.
func Usage() string { return usageText }

const usageText = `AgentDroid — zero-dependency Android app testing CLI (core / Account 1)

Usage:
  agentdroid [global flags] <command> [args] [flags]

Commands:
  launch <app.apk>       Spawn a runtime adapter session for an APK
  snapshot               List the current UI as element refs (@e1, @e2, ...)
  tap <@eN>              Tap the center of an element
  type <@eN> <text>      Focus an element and type text into it
  screenshot             Write a PNG of the current frame to stdout
  close                  End the session and stop the runtime
  serve                  Serve an adapter backend (used by launch; also scriptable)
  version                Print the CLI version

Global flags:
  --runtime NAME         atl | winedroid | fake (default "atl", env AGENTDROID_RUNTIME)
  --runtime-bin PATH     Override the runtime binary (ATL/WineDroid launcher or adapter)
  --session NAME         Session name (default "default", env AGENTDROID_SESSION)
  --timeout DURATION     Per-command timeout (default 15s)
  --json                 Emit raw protocol JSON instead of formatted output
  --tree                 With snapshot: print the view tree instead of the element list
  --help                 Show this help

Notes:
  Refs are resolved against the live screen at command time, so run
  "agentdroid snapshot" to see the current mapping. A "--" terminator
  disables flag parsing for the rest of the line.

Examples:
  agentdroid --runtime fake launch demo.apk
  agentdroid snapshot
  agentdroid tap @e4
  agentdroid type @e2 "shayan@example.com"
  agentdroid screenshot > screen.png
  agentdroid close
`

// Parse turns argv into a Command. It never executes anything; malformed
// invocations come back as *UsageError.
func Parse(args []string) (*Command, error) {
	cmd := &Command{Global: defaults()}

	type flag struct{ name, val string }
	var flags []flag
	var pos []string
	term := false
	for i := 0; i < len(args); i++ {
		a := args[i]
		if !term && strings.HasPrefix(a, "--") && len(a) > 2 {
			name, val, hasVal := strings.Cut(a[2:], "=")
			if !knownFlags[name] {
				return nil, usagef("unknown flag --%s", name)
			}
			if boolFlags[name] && !hasVal {
				val = "true"
			} else if !hasVal {
				if i+1 >= len(args) {
					return nil, usagef("flag --%s requires a value", name)
				}
				i++
				val = args[i]
			}
			flags = append(flags, flag{name, val})
			continue
		}
		if !term && a == "--" {
			term = true
			continue
		}
		pos = append(pos, a)
	}

	for _, f := range flags {
		if err := applyFlag(cmd, f.name, f.val); err != nil {
			return nil, err
		}
	}

	if len(pos) == 0 {
		if cmd.Name != "help" {
			return nil, usagef("no command given (try agentdroid --help)")
		}
		return cmd, nil
	}
	cmd.Name = pos[0]
	rest := pos[1:]
	switch cmd.Name {
	case "launch":
		if len(rest) != 1 {
			return nil, usagef("launch takes exactly one APK path")
		}
		cmd.APK = rest[0]
	case "snapshot", "screenshot", "close", "version":
		if len(rest) != 0 {
			return nil, usagef("%s takes no arguments", cmd.Name)
		}
	case "tap":
		if len(rest) != 1 {
			return nil, usagef("tap takes exactly one element ref (see agentdroid snapshot)")
		}
		if !uitree.ValidRef(rest[0]) {
			return nil, usagef("invalid element ref %q (expected @e1 style)", rest[0])
		}
		cmd.Ref = rest[0]
	case "type":
		if len(rest) < 2 {
			return nil, usagef("type takes an element ref and the text to type")
		}
		if !uitree.ValidRef(rest[0]) {
			return nil, usagef("invalid element ref %q (expected @e1 style)", rest[0])
		}
		cmd.Ref = rest[0]
		cmd.Text = strings.Join(rest[1:], " ")
	case "serve":
		if len(rest) != 0 {
			return nil, usagef("serve takes no positional arguments")
		}
		if cmd.Backend == "" {
			cmd.Backend = runtime.RuntimeFake
		}
	case "help":
		if len(rest) != 0 {
			return nil, usagef("help takes no arguments")
		}
	default:
		return nil, usagef("unknown command %q (try agentdroid --help)", cmd.Name)
	}
	return cmd, nil
}

func defaults() Global {
	g := Global{
		Runtime: runtime.RuntimeATL,
		Session: runtime.DefaultSession,
		Timeout: DefaultTimeout,
	}
	if v := os.Getenv("AGENTDROID_RUNTIME"); v != "" {
		g.Runtime = v
	}
	if v := os.Getenv("AGENTDROID_SESSION"); v != "" {
		g.Session = v
	}
	return g
}

var boolFlags = map[string]bool{
	"json":  true,
	"tree":  true,
	"stdio": true,
	"help":  true,
}

// knownFlags is validated at parse time so an unknown trailing flag is
// reported as unknown rather than as a missing value.
var knownFlags = map[string]bool{
	"runtime":     true,
	"runtime-bin": true,
	"session":     true,
	"timeout":     true,
	"json":        true,
	"tree":        true,
	"stdio":       true,
	"backend":     true,
	"help":        true,
}

func applyFlag(cmd *Command, name, val string) error {
	switch name {
	case "runtime":
		switch val {
		case runtime.RuntimeATL, runtime.RuntimeWineDroid, runtime.RuntimeFake:
			cmd.Runtime = val
		default:
			return usagef("unknown runtime %q (want atl, winedroid or fake)", val)
		}
	case "runtime-bin":
		if val == "" {
			return usagef("--runtime-bin requires a path")
		}
		cmd.RuntimeBin = val
	case "session":
		if !validSession(val) {
			return usagef("invalid session name %q (use letters, digits, '.', '_' or '-')", val)
		}
		cmd.Session = val
	case "timeout":
		d, err := time.ParseDuration(val)
		if err != nil || d <= 0 {
			return usagef("invalid --timeout %q (want e.g. 15s, 500ms)", val)
		}
		cmd.Timeout = d
	case "json":
		b, err := parseBool(val)
		if err != nil {
			return usagef("invalid --json value %q", val)
		}
		cmd.JSON = b
	case "tree":
		b, err := parseBool(val)
		if err != nil {
			return usagef("invalid --tree value %q", val)
		}
		cmd.Tree = b
	case "stdio":
		b, err := parseBool(val)
		if err != nil {
			return usagef("invalid --stdio value %q", val)
		}
		cmd.Stdio = b
	case "backend":
		switch val {
		case runtime.RuntimeATL, runtime.RuntimeWineDroid, runtime.RuntimeFake:
			cmd.Backend = val
		default:
			return usagef("unknown backend %q (want atl, winedroid or fake)", val)
		}
	case "help":
		cmd.Name = "help"
	default:
		return usagef("unknown flag --%s", name)
	}
	return nil
}

func parseBool(val string) (bool, error) {
	switch strings.ToLower(val) {
	case "true", "1", "yes":
		return true, nil
	case "false", "0", "no":
		return false, nil
	}
	return false, fmt.Errorf("not a bool: %q", val)
}

func validSession(s string) bool {
	if len(s) == 0 || len(s) > 64 {
		return false
	}
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9',
			r == '.', r == '_', r == '-':
		default:
			return false
		}
	}
	return true
}
