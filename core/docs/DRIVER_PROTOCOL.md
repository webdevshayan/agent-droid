# DRIVER_PROTOCOL — runtime adapters for AgentDroid

AgentDroid does not talk to Android directly. Everything behind the CLI is a
**driver**: a small adapter that launches an app, reports its view tree, and
feeds input events. The CLI, the ref system and the snapshot parser are
driver-agnostic — a new runtime (Android Translation Layer, WineDroid, a
cloud device farm, a custom emulator) only has to implement one Go interface.

## The contract

```go
package driver // core/driver

type Driver interface {
    // Launch installs/starts the APK (adb: `install -r`; fake: session start).
    Launch(apk string) error
    // Close ends the session (adb: HOME key; fake: terminates the app).
    Close() error
    // Devices lists attached devices (adb only; others return an error).
    Devices() (string, error)
    // Dump returns the current view tree as uiautomator-style XML.
    Dump() ([]byte, error)
    // Tap taps screen coordinates.
    Tap(x, y int) error
    // Type sends text to the focused element.
    Type(text string) error
    // Swipe drags from (x1,y1) to (x2,y2) over durationMs.
    Swipe(x1, y1, x2, y2, durationMs int) error
    // Key sends a keycode (4 = BACK, 3 = HOME, 66 = ENTER, ...).
    Key(code int) error
    // Screenshot returns raw PNG bytes.
    Screenshot() ([]byte, error)
}
```

To add a runtime: implement `driver.Driver` in a new package, register it in
`core/app.openDriver` behind a `--runtime <name>` value, and add parser,
dispatch and e2e tests under `tests/`. Nothing else changes.

## View-tree format

`Dump()` must return **uiautomator-style XML** — the format Android's
`uiautomator dump` produces. The parser (`core/snapshot`) consumes exactly
this subset:

```xml
<?xml version="1.0" encoding="UTF-8"?>
<hierarchy rotation="0">
  <node class="android.widget.Button" text="Sign in" resource-id="demo:id/signin"
        content-desc="" clickable="true" enabled="true" scrollable="false"
        bounds="[340,400][740,520]"/>
</hierarchy>
```

Rules the parser enforces (drivers do not need to re-check them, but the
cleaner the dump the more elements get refs):

- the root element must be `<hierarchy>`;
- only `<node>` elements become refs;
- a node gets a ref **only if** its `bounds` attribute parses as
  `[x1,y1][x2,y2]` with non-negative, well-ordered coordinates — malformed
  bounds skip the node (its children are still walked);
- refs are assigned in document order: `@e1..@eN`.

`Tap(x, y)` coordinates come from the parsed bounds' center, so bounds must
be honest screen coordinates in the driver's own space.

## Semantics the CLI relies on

- **Stateless refs.** The CLI re-dumps before every ref command and picks the
  Nth element. Drivers do not need to keep ref mappings; they need
  *consistent ordering between consecutive dumps of an unchanged screen*.
- **One action per process.** Each `agentdroid` invocation is a fresh
  process. Drivers that own session state (like `fake`) persist it
  themselves — the fake driver writes a small JSON file (path override:
  `AGENTDROID_FAKE_STATE` env var) and reloads it on the next invocation.
- **Error contract.** Drivers must never panic and must return errors that
  make sense to an agent ("no fake app running; run `… launch demo.apk`
  first"). The CLI maps any error to exit code `1` with an `agentdroid:`
  prefix; malformed *arguments* are rejected earlier, at the parser, with
  exit code `2`.
- **Input validation.** Coordinate bounds (0..100000) and the `type` length
  cap (4096 bytes) are enforced in the CLI *and* defensively inside the adb
  driver. Third-party drivers should validate too — the contract is "never
  trust the caller".

## Reference drivers

| Driver | Package | Notes |
| --- | --- | --- |
| `adb` | `core/device` + `core/driver` | real devices/emulators; `launch` = `install -r`, `close` = HOME key; output capped at 64 MiB per call, 60 s timeouts |
| `fake` | `core/driver/fake.go` | in-process simulated app with three screens; persists session state to a JSON file; accepts Unicode input; generates deterministic PNG screenshots |

## Planned drivers

- **ATL (Android Translation Layer)** — run APKs on Linux without an
  emulator; driver would manage the spawned process and translate its view
  tree into the XML dialect above.
- **WineDroid** — same shape, different host layer.

Both fit this protocol unchanged: implement `Driver`, emit the XML dialect,
validate inputs, return agent-readable errors.
