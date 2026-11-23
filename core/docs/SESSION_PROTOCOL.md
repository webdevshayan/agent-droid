# AgentDroid Session Wire Protocol

The contract between the `agentdroid` CLI (the orchestrator) and a runtime
adapter serving one Android app session. Everything here is transport-level:
an adapter that speaks this protocol plugs into the CLI with zero changes.

## Transport

A session is a long-lived adapter process listening on a **Linux abstract Unix
socket** at:

```
@agentdroid-<session>
```

Abstract sockets live only in the kernel — no file on disk, no `/tmp` litter,
nothing to clean up. This is what makes sessions zero-storage.

**Trust boundary.** The protocol carries no credentials: whoever can dial the
socket can drive the session. The daemon closes this gap with the kernel —
on Linux every accepted connection is checked with `SO_PEERCRED`, and
connections from processes of a different OS user are dropped before the
first request. Within one OS user the socket stays open by design (that is
what makes the CLI usable from any agent process of that user); on a shared
account, treat same-user processes as trusted.

`launch` spawns the adapter detached (setsid), waits for the socket to accept,
then performs the `launch` handshake. Every subsequent CLI invocation dials the
socket, sends one request, prints the response, and exits. The adapter keeps
the runtime and all session state in memory.

## Framing

Messages are **newline-delimited JSON** (NDJSON): one UTF-8 JSON object per
line, terminated by `\n`. Lines longer than 16 MiB are a protocol violation.

Unknown JSON fields must be ignored by both sides, so the protocol can add
fields without breaking deployed adapters.

## Requests (CLI → adapter)

```json
{"id": 1, "cmd": "launch",     "apk": "/abs/path/app.apk"}
{"id": 2, "cmd": "snapshot"}
{"id": 3, "cmd": "tap",        "x": 270, "y": 448}
{"id": 4, "cmd": "type",       "x": 270, "y": 178, "text": "shayan@example.com"}
{"id": 5, "cmd": "screenshot"}
{"id": 6, "cmd": "close"}
```

- `id` starts at 1 and increases per request on a connection; the response
  must echo it.
- Coordinates are pixels in the rendered frame, origin top-left.
- `type` also accepts the coordinate-less form `{"cmd": "type", "x": -1,
  "y": -1, "text": "…"}`: the adapter types into the field it has focused
  (how the CLI's `type <text>` reaches a field focused by an earlier `tap`).
  An adapter that cannot honor it returns `ok=false` with a clear error.

## Responses (adapter → CLI)

```json
{"id": 1, "ok": true, "pid": 7965}
{"id": 2, "ok": true, "tree": {"class": "android.widget.FrameLayout", "bounds": [0,0,540,960], "children": []}}
{"id": 3, "ok": true}
{"id": 4, "ok": true}
{"id": 5, "ok": true, "png": "<base64 PNG bytes>"}
{"id": 6, "ok": true}
{"id": 7, "ok": false, "error": "no app launched in this session"}
```

- `ok=false` always carries a human-readable `error`.
- `snapshot` returns the full view tree. `bounds` must be exactly
  `[x, y, width, height]` (four integers; the CLI rejects anything else).
- `screenshot` returns raw PNG bytes, base64-encoded by JSON.
- A successful `close` is the last response on a session: the adapter then
  stops the runtime process and exits.

## View tree schema

| Field          | Type    | Meaning                                        |
| -------------- | ------- | ---------------------------------------------- |
| `class`        | string  | View class, e.g. `android.widget.Button`       |
| `text`         | string  | Displayed text (input value for editable text) |
| `content_desc` | string  | Accessibility description                      |
| `resource_id`  | string  | Android resource id, e.g. `demo:id/sign_in`    |
| `bounds`       | [4]int  | `[x, y, width, height]` in pixels              |
| `clickable`    | bool    | View accepts click events                      |
| `focusable`    | bool    | View accepts keyboard focus                    |
| `children`     | array   | Child views, depth-first order                 |

## Element references

The CLI flattens the tree depth-first (pre-order) and assigns refs
`@e1`, `@e2`, … to **every view with valid bounds** — refs are positional,
not clickability-filtered (the `clickable` flag is shown when true), and a
node with missing or malformed `bounds` gets no ref. `tap @eN` is resolved
by the CLI against a **fresh snapshot at command time** and sent as
coordinates. `type` takes literal text and goes to the field focused by an
earlier `tap`; there is no `type @eN` form. Refs therefore never go stale
between CLI invocations, and neither the CLI nor the adapter needs an
on-disk element map.

## Implementing an adapter

See [ADAPTERS.md](ADAPTERS.md) for the backend contract, the existing
adapters, and what an ATL or WineDroid hook still needs to expose. `serve
--stdio` runs the same protocol over stdin/stdout, which is the simplest way
to test an adapter under development.
