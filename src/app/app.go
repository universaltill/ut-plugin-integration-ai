// Package app wires the AI engine (src/ai) to the till's plugin seams
// (ut-docs#4032, ADR-0121): the sell screen's catalog.identify job, and
// "Ask your till" as a reports.panels content slot whose action runs the
// tool loop as a job over core read views.
package app

import (
	"github.com/universaltill/universal-till/sdk/plugin"
)

// PluginID is manifest.json's id: the same id the config-only plugin had,
// so existing installs keep their settings.
const PluginID = "com.universaltill.integration-ai"

// AskView is the Ask panel's view name (manifest entry "view").
const AskView = "ai.ask"

// AskJobEvent is the Ask job: in the plugin's own namespace (ADR-0121 §2,
// plugin-views.md "Jobs"), hooked in the manifest.
const AskJobEvent = PluginID + ".ask"

// Core event names (plugin-views.md).
const (
	identifyEvent          = "catalog.identify"
	identifyConfirmedEvent = "catalog.identify.confirmed"
	viewAskEvent           = "ui.view.ask"
	actionAskEvent         = "ui.action.ask"
)

// Handlers is every event the plugin answers; each has a manifest hook.
func Handlers() plugin.Handlers {
	return plugin.Handlers{
		identifyEvent: handleIdentify,
		// The pick is already the item's newest ai_ref (the till stored it),
		// which the next identify reads through item_image_open: nothing to
		// keep here yet, so the event is acknowledged with no answer.
		identifyConfirmedEvent: func(plugin.Event) (any, error) { return nil, nil },
		viewAskEvent:           handleView,
		actionAskEvent:         handleAction,
		AskJobEvent:            handleAskJob,
	}
}

// progress reports job progress; best effort (outside a job, or a refused
// key, only loses the progress message).
func progress(pct int, k string) {
	_ = plugin.JobProgress(pct, k)
}
