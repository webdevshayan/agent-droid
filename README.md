# AgentDroid

An open-source, zero-dependency AI mobile-testing CLI. AgentDroid drives Android
apps on a plain Linux box — no physical phone, no emulator images, no ADB, no
Docker, no API keys, no paid services.

The orchestrator spawns a free, open-source runtime (Android Translation Layer
or WineDroid) in the background, exposes the app's view tree as stable element
references (`@e1`, `@e2`, …), and drives interactions with six commands.

## Layout

| Path | Scope |
| --- | --- |
| [`core/`](core/) | **Account 1** — standalone CLI + orchestrator (Go, standard library only) |

## Install (core)

```
go install github.com/webdevshayan/agent-droid/core/cmd/agentdroid@latest
```

## Try it in 30 seconds

```
agentdroid --runtime fake launch demo.apk   # simulated app, works with no runtime installed
agentdroid snapshot
agentdroid tap @e4
agentdroid screenshot > screen.png
agentdroid close
```

See [`core/README.md`](core/README.md) for the full command reference and
[`core/docs/DRIVER_PROTOCOL.md`](core/docs/DRIVER_PROTOCOL.md) for the runtime
adapter protocol.

## License

MIT — see [LICENSE](LICENSE).
