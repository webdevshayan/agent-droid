# AgentDroid

An open-source, zero-dependency AI mobile-testing CLI. AgentDroid exposes an
Android app's view tree as stable element references (`@e1`, `@e2`, …) and
drives interactions with plain shell commands — designed for AI agents, not
for humans squinting at screenshots.

It runs in two modes:

- **`--runtime fake`** — a simulated app built into the binary. No physical
  phone, no emulator images, no ADB, no Docker, no API keys, no paid
  services. The whole agent loop runs anywhere Go runs, in seconds.
- **`--runtime adb`** (default) — real devices and emulators over adb.

Additional free, open-source runtimes (Android Translation Layer, WineDroid)
plug into the same contract — see
[`core/docs/DRIVER_PROTOCOL.md`](core/docs/DRIVER_PROTOCOL.md).

## Layout

| Path | Scope |
| --- | --- |
| [`core/`](core/) | **Account 1** — standalone CLI + driver layer (Go, standard library only) |
| [`tests/`](tests/) | **Account 2** — fuzzing, memory-abuse, e2e and hardening suite (separate Go module) |
| [`docs/`](docs/) | **Account 2** — landing page and docs assets |

## Install (core)

```
go install github.com/webdevshayan/agent-droid/core/cmd/agentdroid@latest
```

Zero third-party dependencies — one static binary. Releases are tagged
`core/vX.Y.Z` (the nested module needs the `core/` prefix), so `@latest`
resolves to the newest tagged release instead of a main-branch pseudo-
version; the release workflow stamps that version into the binary —
`agentdroid --version` tells the truth on release builds and per-tag
binaries are attached to the GitHub release. Dev builds report the
compile-time default (0.1.0).

## Try it in 30 seconds — no device, no adb

```
agentdroid --runtime fake launch demo.apk   # simulated app, works with no runtime installed
agentdroid --runtime fake snapshot
agentdroid --runtime fake tap @e3           # tap "Sign in"
agentdroid --runtime fake snapshot          # → "Welcome, …"
agentdroid --runtime fake screenshot > screen.png
agentdroid --runtime fake close
```

Against a real device or emulator:

```
agentdroid devices
agentdroid launch demo.apk
agentdroid snapshot
agentdroid tap @e3
```

See [`core/README.md`](core/README.md) for the full command reference, the
agent loop and the security model, and
[`core/docs/DRIVER_PROTOCOL.md`](core/docs/DRIVER_PROTOCOL.md) for the runtime
adapter protocol.

## How an AI agent uses it

1. `agentdroid snapshot` — every element with valid bounds comes back as one
   line with a ref: `@e1 android.widget.Button text="Sign in" clickable bounds=… center=(540,460)`.
2. `agentdroid tap @e3`, `agentdroid type "text"`, `agentdroid swipe @e1 @e2` — act by ref, no coordinate guessing.
3. `agentdroid snapshot` again — refs always resolve against the *current*
   screen, so snapshot-after-every-action is the loop.

Exit codes are part of the contract: `0` success, `1` runtime error (retryable),
`2` usage error (fix the command). Any agent that can run shell commands —
Z.ai agent mode, Claude Code, Cursor, Codex CLI — can drive a phone with this.

## Testing & security

The [`tests/`](tests/) module owns quality:

- **Fuzzing** — `FuzzParseArgs`, `FuzzParseXML`, `FuzzParseRef` (plus the
  session tree's `FuzzParseTree`) run in CI at 30 s per target with a
  scheduled weekly long-fuzz lane at 10 min per target — zero crashes so
  far.
- **Memory-abuse phase** — 0-byte and 500 MB payloads are thrown at `type` and
  the dump parser; rejections must be linear-time with O(1) allocation.
- **Coverage gate** — ≥ 90% across all `core/` packages, enforced in CI.
- **Graceful-failure smoke** — missing adb, bad APK paths, invalid refs and
  malformed dumps must produce clear errors and documented exit codes, never
  panics.

## License

MIT — see [LICENSE](LICENSE).
