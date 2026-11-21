// Command agentdroid gives AI agents hands on Android: snapshot screens as
// @eN refs, tap/type/swipe by ref — on real devices via adb, or fully
// self-contained with --runtime fake.
package main

import (
	"os"

	"github.com/webdevshayan/agent-droid/core/app"
)

func main() {
	os.Exit(app.Run(os.Args[1:], os.Stdout, os.Stderr, nil))
}
