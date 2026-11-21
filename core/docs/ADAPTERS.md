# Runtime Adapters

An adapter implements the `Backend` interface (Go, `internal/runtime/backend.go`)
and serves it to the CLI over the session socket — see
[DRIVER_PROTOCOL.md](DRIVER_PROTOCOL.md) for the wire format.

```go
type Backend interface {
    Launch(apk string) *protocol.Response
    Snapshot() *protocol.Response
    Tap(x, y int) *protocol.Response
    Type(x, y int, text string) *protocol.Response
    Screenshot() *protocol.Response
    Close() *protocol.Response
    Stop() // kill any spawned runtime process; idempotent
}
```

The CLI spawns `agentdroid serve --backend <name> --session <name>` (or any
executable via `--runtime-bin`) as a detached daemon; `serve --stdio` runs the
same protocol on stdin/stdout for embedding or testing.

## Status matrix

| Backend    | Process lifecycle (spawn / keep alive / clean shutdown) | View tree (`snapshot`, tap/type targeting) | Framebuffer (`screenshot`) |
| ---------- | ------------------------------------------------------- | ------------------------------------------ | -------------------------- |
| `fake`     | ✅ simulated, self-contained                             | ✅ full login app state machine             | ✅ real PNG (540×960)      |
| `atl`      | ✅ real: spawns the ATL launcher, kills the process group on close | ⚠️ needs upstream accessor | ⚠️ needs upstream accessor |
| `winedroid`| ✅ real: spawns the WineDroid launcher, kills the process group on close | ⚠️ needs upstream accessor | ⚠️ needs upstream accessor |

⚠️ means the adapter returns an explicit, documented `unsupported` protocol
error rather than pretending. Nothing in the orchestrator assumes data that is
not there.

## What the ATL / WineDroid hooks need

Both runtimes execute APKs as ordinary Linux processes (no emulator, no ADB,
no container), so process management is already real. The two missing pieces
are:

1. **View tree dump** — the runtime must expose the app's view hierarchy in
   the `snapshot` response schema (classes, text, ids, bounds, flags).
   * For ATL this is tractable upstream: ATL implements the Android view
     system natively, so its launcher can walk the widget tree it already
     owns and print it as JSON (a new `--agentdroid-driver` flag or an
     environment switch is enough — the adapter picks it up and proxies).
   * For WineDroid the equivalent hook is a small patch in its window
     management layer.
2. **Framebuffer capture** — the runtime must return the current frame as PNG
   bytes (or raw RGBA + geometry the adapter can convert). A window-surface
   read in the same hook is fine.

Both changes are additive, self-contained, and testable with the `fake`
backend as the reference implementation. Once a runtime exposes either hook,
wire it into `processBackend` (`internal/runtime/process.go`) — the session
protocol, refs, rendering and tests stay untouched.

## Environment overrides

| Variable                  | Effect                                        |
| ------------------------- | --------------------------------------------- |
| `AGENTDROID_ATL_BIN`      | ATL launcher binary (default: `android-launcher` in PATH) |
| `AGENTDROID_ATL_ARGS`     | Argument template for the launcher; `{apk}` is substituted (default: `{apk}`) |
| `AGENTDROID_WINEDROID_BIN`| WineDroid launcher binary (default: `winedroid` in PATH) |
| `AGENTDROID_WINEDROID_ARGS`| Argument template (default: `{apk}`)         |

`--runtime-bin` on the CLI overrides the adapter binary itself (used by the
test suite to re-exec its own binary).

## Writing your own adapter

1. Implement `Backend` (or speak the NDJSON protocol directly and run `agentdroid
   serve --stdio` as a bridge).
2. Register it in `NewBackend` (`internal/runtime/backend.go`) or run it with
   `--runtime-bin`.
3. Add a session test against the `fake` flow in `internal/integration` —
   launch → snapshot → tap/type → screenshot → close is the full contract.
