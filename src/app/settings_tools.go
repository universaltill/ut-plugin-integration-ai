package app

import (
	"errors"
	"fmt"
	"unicode/utf8"

	"github.com/universaltill/universal-till/sdk/plugin"
	"github.com/universaltill/ut-plugin-integration-ai/src/ai"
)

// Settings tools (ut-docs#4129, ADR-0153 Decision 3). settings_describe reads
// core's settings.describe.v1 view: every Settings region with the screen's
// own label and help, whether the assistant may change it, the current value
// when it is safe to read, and a link to the card. settings_propose asks core
// for settings.set.v1; core checks the grant, the operator's role and the
// allow-list, works out before and after, and draws the confirm card itself.
// Nothing changes until the operator presses Confirm, so the tool only ever
// returns "awaiting confirmation".

const (
	settingsDescribeView = "settings.describe.v1"
	settingsSetAction    = "settings.set.v1"
	// maxProposalsPerTurn mirrors core's ceiling of 3 confirm cards in one
	// document and 3 open proposals per request (ADR-0153 Decision 6).
	maxProposalsPerTurn = 3
	settingKeyMax       = 64
)

// settingsGuidance is appended to the Ask system prompt while the settings
// tools are offered. It differs from core's old Ask prompt on purpose: that
// engine had no settings tools.
const settingsGuidance = " You can also explain this till's settings: call settings_describe and answer " +
	"from its label and help text, in the user's language, with the current value and the link " +
	"it returns (write the link path as plain text; never invent a link, and for a setting settings_describe does not list, say you cannot see it and name the Settings screen only). To change a setting the user asked for, " +
	"call settings_propose only for a row whose writable is true; core then shows the user a " +
	"confirmation card and nothing changes until they confirm, so never say it is done. " +
	"For a setting that is not writable (tax, payments, fiscal, users, updates, AI, anything " +
	"else) never try to change it: explain the steps and give its link so the user can change it themselves."

// proposals collects the confirm cards of this event. A WASM instance runs
// one event at a time, so a package variable is safe; handleAskJob drains it.
var proposals []string

func resetProposals() { proposals = nil }

func takeProposals() []string {
	p := proposals
	proposals = nil
	return p
}

func settingsTools() []ai.AskTool {
	return []ai.AskTool{
		{
			Name: "settings_describe",
			Description: "List every setting of this till's Settings screen: key, label, help text, whether the assistant may change it (writable), " +
				"its current value when it is safe to show (on/off or text) and the link to its card. Use it to explain a setting or find the one matching what the user wants.",
			Params: map[string]any{"type": "object", "properties": map[string]any{}},
			Run: func(map[string]any) (any, error) {
				return viewTool(settingsDescribeView, map[string]int{})
			},
		},
		{
			Name: "settings_propose",
			Description: "Propose switching ONE writable setting on or off. The user sees a confirmation card with the old and new value and nothing changes " +
				"until they confirm. Use only keys that settings_describe lists with writable true.",
			Params: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"key":   map[string]any{"type": "string", "description": "the setting's key, exactly as settings_describe returns it"},
					"value": map[string]any{"type": "string", "enum": []string{"on", "off"}, "description": "the new state"},
				},
				"required": []string{"key", "value"},
			},
			Run: settingsPropose,
		},
	}
}

func settingsPropose(args map[string]any) (any, error) {
	k, _ := args["key"].(string)
	v, _ := args["value"].(string)
	if k == "" || utf8.RuneCountInString(k) > settingKeyMax || (v != "on" && v != "off") {
		return nil, fmt.Errorf("settings_propose needs a key and a value of on or off")
	}
	if len(proposals) >= maxProposalsPerTurn {
		return map[string]any{"status": "refused", "reason": "too_many_changes_at_once", "hint": "ask the user to confirm the open changes first"}, nil
	}
	id, err := plugin.ActionPropose(settingsSetAction, map[string]any{
		"changes": []map[string]string{{"key": k, "value": v}},
	})
	switch {
	case errors.Is(err, plugin.ErrInvalid):
		return map[string]any{"status": "refused", "reason": "not_changeable_by_the_assistant",
			"hint": "explain the steps and give the link from settings_describe so the user changes it themselves; also refused when it is already in that state"}, nil
	case errors.Is(err, plugin.ErrDenied):
		return map[string]any{"status": "refused", "reason": "not_permitted",
			"hint": "this user or till may not change settings through the assistant; explain the steps and give the link"}, nil
	case err != nil:
		return nil, err
	}
	proposals = append(proposals, id)
	return map[string]any{"status": "awaiting_confirmation", "note": "the user must press Confirm on the card shown below your answer; do not say it is done"}, nil
}
