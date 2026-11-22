// Package cli parses agentdroid command-line arguments into Command values.
//
// It is a pure layer: no I/O, no adb calls, no os.Exit. That makes it safe
// to fuzz — every malformed invocation must come back as an error or a
// validated Command, never a panic.
package cli

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/webdevshayan/agent-droid/core/snapshot"
)

// Verb identifies the subcommand a Command represents.
type Verb string

const (
	VerbDevices    Verb = "devices"
	VerbLaunch     Verb = "launch"
	VerbClose      Verb = "close"
	VerbInstall    Verb = "install"
	VerbSnapshot   Verb = "snapshot"
	VerbTap        Verb = "tap"
	VerbType       Verb = "type"
	VerbSwipe      Verb = "swipe"
	VerbScreenshot Verb = "screenshot"
	VerbClear      Verb = "clear"
	VerbBack       Verb = "back"
	VerbHome       Verb = "home"
	VerbHelp       Verb = "help"
	VerbVersion    Verb = "version"
	VerbServe      Verb = "serve"
)

// Runtime selectors for the --runtime flag. adb and fake run in-process;
// atl and winedroid are persistent session runtimes (spawned as daemons,
// attached with --session). `serve --backend` uses the same names — keep
// in sync with internal/runtime.
const (
	RuntimeFake      = "fake"
	RuntimeAdb       = "adb"
	RuntimeATL       = "atl"
	RuntimeWineDroid = "winedroid"
)

// MaxTextLen caps `type` payloads. adb's `input text` ships the argument
// through the device-side shell, which chokes on very long strings, so
// anything above this limit is rejected before a process is ever spawned.
const MaxTextLen = 4096

// MaxCoord bounds every swipe coordinate. Real screen coordinates are never
// anywhere near this, so larger values mean a malformed command.
const MaxCoord = 100000

// Command is the fully validated result of parsing one invocation.
type Command struct {
	Verb    Verb
	Runtime string // "adb" (default) or "fake"
	Serial  string // adb -s selector; "" = first attached device

	Ref    string // tap/clear target, swipe source
	Ref2   string // swipe destination
	Text   string // type payload
	Path   string // install/launch APK path
	Out    string // screenshot output path; "" = PNG to stdout
	XML    bool   // snapshot: print the raw view-tree dump
	Coords []int  // swipe coordinate mode: x1 y1 x2 y2

	// Session mode: zero-storage runtime daemons behind driver.RT.
	Session    string // --session: attach to (or launch) a named daemon session
	Backend    string // serve --backend: fake, atl or winedroid
	Stdio      bool   // serve --stdio: session protocol on stdin/stdout
	RuntimeBin string // --runtime-bin: override the adapter binary
}

// Parse turns one argv (without the program name) into a Command.
// Flag values may appear anywhere; -h/--help and --version short-circuit
// everything else. The `--` separator forces the rest to be positional.
func Parse(argv []string) (Command, error) {
	var c Command
	if len(argv) == 0 {
		return c, errors.New("no command given; run `agentdroid help`")
	}

	var pos []string
	serialSet := false
	runtimeSet, sessionSet, backendSet, stdioSet, binSet := false, false, false, false, false
	for i := 0; i < len(argv); i++ {
		arg := argv[i]
		switch {
		case arg == "--":
			pos = append(pos, argv[i+1:]...)
			i = len(argv)
		case arg == "-h" || arg == "--help":
			return Command{Verb: VerbHelp, Runtime: RuntimeAdb}, nil
		case arg == "--version":
			return Command{Verb: VerbVersion, Runtime: RuntimeAdb}, nil
		case arg == "--serial" || arg == "-s":
			if serialSet {
				return c, errors.New("duplicate flag --serial")
			}
			if i+1 >= len(argv) {
				return c, fmt.Errorf("flag %s requires a value", arg)
			}
			i++
			if argv[i] == "" {
				return c, errors.New("flag --serial requires a non-empty value")
			}
			c.Serial, serialSet = argv[i], true
		case arg == "--runtime":
			if runtimeSet {
				return c, errors.New("duplicate flag --runtime")
			}
			if i+1 >= len(argv) {
				return c, fmt.Errorf("flag %s requires a value", arg)
			}
			i++
			switch argv[i] {
			case RuntimeAdb, RuntimeFake, RuntimeATL, RuntimeWineDroid:
				c.Runtime, runtimeSet = argv[i], true
			default:
				return c, fmt.Errorf("unknown runtime %q; want adb, fake, atl or winedroid", argv[i])
			}
		case arg == "--session":
			if sessionSet {
				return c, errors.New("duplicate flag --session")
			}
			if i+1 >= len(argv) {
				return c, fmt.Errorf("flag %s requires a value", arg)
			}
			i++
			if argv[i] == "" {
				return c, errors.New("flag --session requires a non-empty value")
			}
			c.Session, sessionSet = argv[i], true
		case arg == "--backend":
			if backendSet {
				return c, errors.New("duplicate flag --backend")
			}
			if i+1 >= len(argv) {
				return c, fmt.Errorf("flag %s requires a value", arg)
			}
			i++
			if argv[i] == "" {
				return c, errors.New("flag --backend requires a non-empty value")
			}
			c.Backend, backendSet = argv[i], true
		case arg == "--stdio":
			if stdioSet {
				return c, errors.New("duplicate flag --stdio")
			}
			c.Stdio, stdioSet = true, true
		case arg == "--runtime-bin":
			if binSet {
				return c, errors.New("duplicate flag --runtime-bin")
			}
			if i+1 >= len(argv) {
				return c, fmt.Errorf("flag %s requires a value", arg)
			}
			i++
			if argv[i] == "" {
				return c, errors.New("flag --runtime-bin requires a non-empty value")
			}
			c.RuntimeBin, binSet = argv[i], true
		case arg == "--xml":
			c.XML = true
		case arg == "--out":
			if c.Out != "" {
				return c, errors.New("duplicate flag --out")
			}
			if i+1 >= len(argv) {
				return c, fmt.Errorf("flag %s requires a value", arg)
			}
			i++
			if argv[i] == "" {
				return c, errors.New("flag --out requires a non-empty value")
			}
			c.Out = argv[i]
		case strings.HasPrefix(arg, "-") && arg != "-":
			return c, fmt.Errorf("unknown flag %q; run `agentdroid help`", arg)
		default:
			pos = append(pos, arg)
		}
	}

	if c.Runtime == "" && !sessionSet {
		c.Runtime = RuntimeAdb
	}

	if len(pos) == 0 {
		return c, errors.New("no command given; run `agentdroid help`")
	}

	switch Verb(pos[0]) {
	case VerbDevices:
		if len(pos) != 1 {
			return c, errors.New("devices takes no arguments")
		}
		if c.Runtime != RuntimeAdb {
			return c, errors.New("devices applies to the adb runtime only")
		}
		c.Verb = VerbDevices

	case VerbLaunch, VerbInstall:
		if len(pos) != 2 {
			return c, fmt.Errorf("%s wants exactly one APK path, e.g. agentdroid %s demo.apk", pos[0], pos[0])
		}
		if pos[1] == "" {
			return c, fmt.Errorf("%s: path is empty", pos[0])
		}
		c.Verb, c.Path = Verb(pos[0]), pos[1]

	case VerbClose:
		if len(pos) != 1 {
			return c, errors.New("close takes no arguments")
		}
		c.Verb = VerbClose

	case VerbSnapshot:
		if len(pos) != 1 {
			return c, errors.New("snapshot takes no positional arguments (use --xml for the raw dump)")
		}
		c.Verb = VerbSnapshot

	case VerbTap:
		if len(pos) != 2 {
			return c, errors.New("tap takes exactly one element ref, e.g. agentdroid tap @e1")
		}
		if _, err := snapshot.ParseRef(pos[1]); err != nil {
			return c, fmt.Errorf("tap: %w", err)
		}
		c.Verb, c.Ref = VerbTap, pos[1]

	case VerbType:
		if len(pos) < 2 {
			return c, errors.New(`type needs text to type, e.g. agentdroid type "hello world"`)
		}
		// Length check first (O(1), no copy, no scan) so oversized
		// payloads are rejected before any work is done on them.
		var text string
		if len(pos) == 2 {
			text = pos[1]
		} else {
			text = strings.Join(pos[1:], " ")
		}
		if len(text) > MaxTextLen {
			return c, fmt.Errorf("type: text is %d bytes, over the %d-byte limit", len(text), MaxTextLen)
		}
		if strings.TrimSpace(text) == "" {
			return c, errors.New("type: text is empty")
		}
		c.Verb, c.Text = VerbType, text

	case VerbSwipe:
		switch {
		case len(pos) == 3:
			for _, r := range pos[1:3] {
				if _, err := snapshot.ParseRef(r); err != nil {
					return c, fmt.Errorf("swipe: %w", err)
				}
			}
			c.Ref, c.Ref2 = pos[1], pos[2]
		case len(pos) == 5:
			coords := make([]int, 4)
			for i, s := range pos[1:5] {
				n, err := strconv.Atoi(s)
				if err != nil {
					return c, fmt.Errorf("swipe: coordinate %q is not an integer", s)
				}
				if n < 0 || n > MaxCoord {
					return c, fmt.Errorf("swipe: coordinate %d out of range 0..%d", n, MaxCoord)
				}
				coords[i] = n
			}
			c.Coords = coords
		default:
			return c, errors.New(`swipe wants "swipe @eA @eB" or "swipe x1 y1 x2 y2"`)
		}
		c.Verb = VerbSwipe

	case VerbScreenshot:
		if len(pos) != 1 {
			return c, errors.New("screenshot takes no positional arguments (use --out for the path)")
		}
		c.Verb = VerbScreenshot

	case VerbClear:
		if len(pos) != 2 {
			return c, errors.New("clear takes exactly one element ref, e.g. agentdroid clear @e1")
		}
		if _, err := snapshot.ParseRef(pos[1]); err != nil {
			return c, fmt.Errorf("clear: %w", err)
		}
		c.Verb, c.Ref = VerbClear, pos[1]

	case VerbBack, VerbHome:
		if len(pos) != 1 {
			return c, fmt.Errorf("%s takes no arguments", pos[0])
		}
		c.Verb = Verb(pos[0])

	case VerbServe:
		if len(pos) != 1 {
			return c, errors.New("serve takes no arguments")
		}
		switch c.Backend {
		case RuntimeFake, RuntimeATL, RuntimeWineDroid:
		default:
			if c.Backend == "" {
				return c, errors.New("serve: --backend is required (fake, atl or winedroid)")
			}
			return c, fmt.Errorf("serve: unknown backend %q; want fake, atl or winedroid", c.Backend)
		}
		if !c.Stdio && !sessionSet {
			return c, errors.New("serve: --session is required for socket mode (or use --stdio)")
		}
		c.Verb = VerbServe

	case VerbHelp, VerbVersion:
		c.Verb = Verb(pos[0])

	default:
		return c, fmt.Errorf("unknown command %q; valid commands: devices launch close install snapshot tap type swipe screenshot clear back home serve", pos[0])
	}

	// Flags only apply to the verbs/runtimes that understand them — a
	// strict parser gives agents a clear error instead of silently
	// ignoring a typo.
	if c.XML && c.Verb != VerbSnapshot {
		return c, errors.New("flag --xml only applies to snapshot")
	}
	if c.Out != "" && c.Verb != VerbScreenshot {
		return c, errors.New("flag --out only applies to screenshot")
	}
	if backendSet && c.Verb != VerbServe {
		return c, errors.New("flag --backend only applies to serve")
	}
	if stdioSet && c.Verb != VerbServe {
		return c, errors.New("flag --stdio only applies to serve")
	}
	if binSet && c.Verb != VerbServe && c.Verb != VerbLaunch && c.Verb != VerbInstall {
		return c, errors.New("flag --runtime-bin only applies to serve and launch")
	}
	if c.Verb == VerbServe && runtimeSet {
		return c, errors.New("serve selects its backend with --backend, not --runtime")
	}
	if sessionSet {
		switch c.Verb {
		case VerbSwipe, VerbClear, VerbBack, VerbHome:
			return c, fmt.Errorf("%s is not supported in session mode yet", pos[0])
		case VerbHelp, VerbVersion:
			return c, errors.New("flag --session does not apply to help or version")
		case VerbLaunch, VerbInstall:
			switch c.Runtime {
			case RuntimeFake, RuntimeATL, RuntimeWineDroid:
			default:
				return c, errors.New("launch --session requires --runtime fake, atl or winedroid")
			}
		case VerbSnapshot, VerbTap, VerbType, VerbScreenshot, VerbClose:
			if c.Runtime == RuntimeAdb {
				return c, errors.New("--session talks to a runtime daemon; drop --runtime or pick fake, atl or winedroid")
			}
		}
	} else if c.Verb == VerbLaunch || c.Verb == VerbInstall {
		if c.Runtime == RuntimeATL || c.Runtime == RuntimeWineDroid {
			return c, fmt.Errorf("%s runtimes are persistent sessions; add --session <name>", c.Runtime)
		}
	}
	if serialSet && (sessionSet || c.Verb == VerbServe || c.Verb == VerbHelp || c.Verb == VerbVersion || c.Runtime == RuntimeFake || c.Runtime == RuntimeATL || c.Runtime == RuntimeWineDroid) {
		return c, errors.New("flag --serial applies to the adb runtime only")
	}
	return c, nil
}
