# AgentDroid core — the CLI and driver layer

`agentdroid` turns an Android app's view tree into numbered element refs
(`@e1`–`@eN`) that an AI agent reads as text and acts on deterministically.
It talks to **drivers**: the built-in `adb` driver drives real devices and
emulators; the built-in `fake` driver is a simulated app that needs no
device, no adb and no emulator at all. Third-party runtimes (Android
Translation Layer, WineDroid, …) implement the same contract — see
[docs/DRIVER_PROTOCOL.md](docs/DRIVER_PROTOCOL.md).

Standard library only. One static binary. No configuration files, no API keys.

## Install

Requirements: **Go 1.22+**. The `adb` driver additionally needs Android
platform-tools (`adb`) on `PATH`; the `fake` driver needs nothing.

```sh
go install github.com/webdevshayan/agent-droid/core/cmd/agentdroid@latest
agentdroid --version
```

## Try it in 30 seconds

```console
$ agentdroid --runtime fake launch demo.apk
launched demo.apk

$ agentdroid --runtime fake snapshot
@e1  android.widget.TextView  text="Demo App"  id=demo:id/title  bounds=[0,0][1080,140]  center=(540,70)
@e2  android.widget.EditText  text=""  id=demo:id/username  clickable  bounds=[54,200][1026,320]  center=(540,260)
@e3  android.widget.Button  text="Sign in"  id=demo:id/signin  clickable  bounds=[340,400][740,520]  center=(540,460)
@e4  android.widget.Button  text="About"  id=demo:id/about  clickable  bounds=[340,560][740,680]  center=(540,620)

$ agentdroid --runtime fake tap @e2
tapped @e2 at (540,260)

$ agentdroid --runtime fake type "demo@gmail.com"
typed 14 bytes

$ agentdroid --runtime fake tap @e3
tapped @e3 at (540,460)

$ agentdroid --runtime fake snapshot
@e1  android.widget.TextView  text="Welcome, demo@gmail.com"  ...
```

## Command reference

| Command | Effect |
| --- | --- |
| `agentdroid devices` | list attached devices (adb runtime; fails with a clear error if none) |
| `agentdroid launch <path.apk>` | adb: validate then `install -r` the APK · fake: start the simulated app |
| `agentdroid close` | adb: send HOME (app stays installed, backgrounded) · fake: end the session |
| `agentdroid snapshot` | dump the screen; print one line per tappable element with its ref |
| `agentdroid snapshot --xml` | print the raw view-tree XML instead |
| `agentdroid tap @eN` | tap the element's center |
| `agentdroid type <text>` | type into the focused element (tap a field first) |
| `agentdroid swipe @eA @eB` | swipe from element A to element B |
| `agentdroid swipe x1 y1 x2 y2` | swipe between raw coordinates |
| `agentdroid screenshot [--out <path>]` | capture a PNG — without `--out` the PNG goes to stdout, so `agentdroid screenshot > screen.png` works |
| `agentdroid clear @eN` | focus the element, move-end, then a bounded DEL sweep (40 chars) |
| `agentdroid back` / `agentdroid home` | system navigation keys |
| `agentdroid --runtime fake <cmd>` | run against the simulated app |
| `agentdroid --runtime adb <cmd>` | explicit adb mode (the default) |
| `agentdroid --serial <id> <cmd>` | target a specific device (adb; short form `-s`) |
| `agentdroid help`, `agentdroid --version` | usage text / build version |

`install` is kept as an alias of `launch` for the adb runtime.

**Exit codes** — part of the agent contract:

- `0` — success
- `1` — runtime error (no device, adb missing, APK not found, ref out of range, …). stderr starts with `agentdroid:` and names the problem.
- `2` — usage error (unknown command, malformed ref, oversized text). Same message format; the agent should fix the command, not retry it blindly.

### Snapshot output format

One line per element, fields omitted when empty — a stable contract agents
parse line-by-line:

```
@eN  <class>  [text="…"]  [desc="…"]  [id=<resource-id>]  [pkg=<package>]  [clickable]  [scrollable]  [disabled]  bounds=[x1,y1][x2,y2]  center=(x,y)
```

`text` and `desc` are Go-quoted (control characters escaped), so one element
is always exactly one line.

### Ref semantics (read this before scripting)

Refs are **positional and stateless**. Every ref command re-dumps the
*current* screen and picks the Nth element. Each command is self-consistent,
which means:

- **Snapshot immediately before acting.** If the screen changed, `@e3` may
  now be a different element.
- Snapshot after every action — the loop is *snapshot → act → snapshot*.
- Refs are strictly `@e` + digits (1–9 digits, no leading zeros); anything
  else is a usage error (`@e0`, `@E1`, `@e99999999999999999999` are rejected,
  never silently wrapped).

### Typing rules

- The CLI caps `type` payloads at **4096 bytes**, rejected in O(1) before any
  copy or process spawn.
- The **adb driver** enforces a shell-safe charset allowlist:
  `[a-zA-Z0-9-_.:,!?@#~^%*+=/]`, spaces become `%s`. Shell metacharacters
  (`& ; | < > $ ` " ' \`), control characters and non-ASCII are rejected with
  exit `1` — an allowlist is the only way to guarantee no injection into the
  device-side shell.
- The **fake driver** accepts Unicode (it never touches a shell); XML
  metacharacters in text are escaped in dumps and survive parsing verbatim.
- Newlines are not supported by `input text`; split text across `type` +
  `keyevent 66` steps, or use `clear`/app-level controls.

## The agent loop (works with any shell-capable agent)

Z.ai agent mode, Claude Code, Cursor, Codex CLI, Aider, or your own harness —
anything that can run a shell command and read stdout:

1. **Discover**: `agentdroid devices` — confirm a device; use `--serial` with
   several. (Skip entirely with `--runtime fake`.)
2. **Read**: `agentdroid snapshot` — every line is one decision candidate.
3. **Act**: `tap @eN`, `type "…"`, `swipe @eA @eB`, `clear @eN`, `back`,
   `home`, `launch`, `close`. Branch on the exit code.
4. **Verify**: `agentdroid snapshot` — confirm the screen changed as intended.

A minimal system prompt for an agent driving a phone:

```text
You are testing an Android device with the agentdroid CLI.
Rules:
- Run `agentdroid snapshot` first and after EVERY action; refs are always
  re-resolved against the current screen.
- Act only by ref (`agentdroid tap @e3`) or by coordinates for free-form swipes.
- `agentdroid type "text"` goes to the focused element: tap the field first.
- Exit code 0 = success, 1 = retryable environment error, 2 = your command was
  malformed — fix the command, do not retry it blindly.
- State what you did and what you verified on the screen after each step.
```

Because output is plain text with stable line formats, agents can grep it
(`agentdroid snapshot | grep -i 'sign in'`) and diff two snapshots to detect
screen transitions.

## Security model

Everything crossing the boundary — CLI arguments, view-tree dumps, adb
output — is treated as untrusted:

- **CLI parser** (`core/cli`) is a pure layer: never panics, validates every
  flag and argument, strict about unknown flags. Fuzzed continuously.
- **View-tree parser** (`core/snapshot`) is streaming (no unbounded
  recursion) with hard caps: 20 MiB input, 64-level nesting, 10,000 elements.
  Malformed bounds skip the element; garbage input returns an error, never a
  crash.
- **`type` pipeline** enforces the length cap and (for adb) the charset
  allowlist *before* any copy or process spawn — a 500 MB payload is
  rejected in ~90 ms with negligible allocation (covered by dedicated
  memory-abuse tests in `tests/`).
- **adb output** is read through a 64 MiB cap; stderr is truncated to 4 KiB;
  every adb call has a 60 s timeout.
- **No shell interpolation anywhere**: arguments go straight to `exec`, text
  payloads are allowlisted, and there is no path from user text to
  device-shell execution.
- **Fake runtime state** is a JSON file under the OS temp dir (override with
  `AGENTDROID_FAKE_STATE`), written with `0600` permissions.

## Development

The repo is a Go workspace: `core/` is the product module, `tests/` the
quality module (see the repo-level README for the Account split).

```sh
go build ./core/... ./tests/...
go test ./tests                                # unit + fuzz seeds + memory-abuse + e2e
go test ./tests -run '^$' -fuzz FuzzParseArgs -fuzztime 60s
go test ./tests -run '^$' -fuzz FuzzParseXML -fuzztime 60s
go test ./tests -run '^$' -fuzz FuzzParseRef -fuzztime 30s
go test ./tests -coverpkg=github.com/webdevshayan/agent-droid/core/... -coverprofile=c.out
go tool cover -func=c.out
```

Layout:

```
core/cli/        argv parser (pure, fuzzed)
core/snapshot/   view-tree XML parser + ref resolution (fuzzed)
core/device/     adb wrapper: timeouts, output caps, charset allowlist
core/driver/     the Driver contract + adb and fake implementations
core/app/        dispatch, exit codes, help text
core/cmd/agentdroid/  entry point
tests/           everything: unit, fuzz, memory-abuse, e2e, hardening
docs/site/       landing page (static HTMX, no build step)
```

CI runs vet, the full suite, a 90% coverage gate over `core/`, a fuzz smoke
per parser, and a no-device smoke of the fake runtime on every push.

## Roadmap

- ATL / WineDroid drivers behind the same contract
- clipboard/IME input path for Unicode text on real devices
- `snapshot --json` machine-readable output mode
- wait-for-element (`wait @e5 --timeout 10s`)
- `launch` starting an installed app by package name on adb (v1 installs)

## License

MIT — see [LICENSE](LICENSE).
