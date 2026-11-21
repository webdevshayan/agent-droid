package app

// Usage is the full help text, mirrored by core/README.md.
const Usage = `AgentDroid — an Android CLI for AI agents.

Every screen is dumped as numbered element refs @e1..@eN. Refs are resolved
against the CURRENT screen, so run ` + "`snapshot`" + ` again after every action.

Usage:
  agentdroid devices                      list attached devices (adb runtime)
  agentdroid launch <path.apk>            install+start an app (fake: session start)
  agentdroid close                        end the session (adb: HOME key)
  agentdroid snapshot [--xml]             print screen elements as refs
  agentdroid tap @eN                      tap an element's center
  agentdroid type <text>                  type into the focused element
  agentdroid swipe @eA @eB                swipe between two elements
  agentdroid swipe x1 y1 x2 y2            swipe between coordinates
  agentdroid screenshot [--out <path>]    capture a PNG (no --out: stdout)
  agentdroid clear @eN                    focus element, then delete its text
  agentdroid back | home                  system navigation keys
  agentdroid --runtime fake <command>     run against the simulated app
  agentdroid [--serial <id>] <command>    target a specific device (adb)
  agentdroid help | --version             this text / build version

Try it with no device at all:
  agentdroid --runtime fake launch demo.apk
  agentdroid --runtime fake snapshot
  agentdroid --runtime fake tap @e3

Agent loop:
  1. agentdroid snapshot        -> read @eN refs, decide what to do
  2. agentdroid tap @e3         -> act by ref
  3. agentdroid type "hello"    -> fill the focused field
  4. agentdroid snapshot        -> verify the new screen

Exit codes: 0 success, 1 runtime error (no device, adb missing, install
failed, ...), 2 usage error (bad arguments).

Docs: https://github.com/webdevshayan/agent-droid
`
