// AI Assistant (com.universaltill.integration-ai): the till's AI features —
// camera identify on the sell screen and "Ask your till" on Reports — as a
// WASI module the till runs in-process (ADR-0001, ADR-0121). Build with
// GOOS=wasip1 GOARCH=wasm (scripts/build.sh). The logic lives in src/app
// and src/ai, unit-tested natively on the SDK's FakeHost.
package main

import (
	"github.com/universaltill/universal-till/sdk/plugin"
	"github.com/universaltill/ut-plugin-integration-ai/src/app"
)

func main() {
	plugin.Run(app.Handlers())
}
