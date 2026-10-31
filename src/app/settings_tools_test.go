//go:build !wasip1

package app

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/universaltill/universal-till/sdk/plugin"
)

// ut-docs#4129: settings_describe (settings.describe.v1) and settings_propose
// (action_propose settings.set.v1) in the Ask loop. The change only becomes a
// proposal; the till draws the confirm card in its own words.

func toolByName(t *testing.T, name string) func(map[string]any) (any, error) {
	t.Helper()
	for _, tl := range askTools() {
		if tl.Name == name {
			return tl.Run
		}
	}
	t.Fatalf("no tool %s", name)
	return nil
}

func TestSettingsDescribe_ReadsTheView_4129(t *testing.T) {
	h := newHost(t, ollamaSettings())
	var got string
	h.Views = func(name string, args json.RawMessage) (json.RawMessage, error) {
		got = name + string(args)
		return json.RawMessage(`[{"key":"pos.allow_negative_inventory","link":"/settings#settings-stock-tracking","label":"Sell items without tracking stock","kind":"bool","writable":true,"value":"off"}]`), nil
	}
	out, err := toolByName(t, "settings_describe")(nil)
	if err != nil {
		t.Fatal(err)
	}
	if got != "settings.describe.v1{}" {
		t.Fatalf("view call = %s", got)
	}
	if !strings.Contains(string(out.(json.RawMessage)), "settings-stock-tracking") {
		t.Fatalf("result = %s", out)
	}
}

func TestSettingsPropose_CallsActionPropose_4129(t *testing.T) {
	h := newHost(t, ollamaSettings())
	var name string
	var args json.RawMessage
	h.Propose = func(n string, a json.RawMessage) (string, error) {
		name, args = n, a
		return "0123456789abcdef0123456789abcdef", nil
	}
	resetProposals()
	out, err := toolByName(t, "settings_propose")(map[string]any{"key": "pos.allow_negative_inventory", "value": "on"})
	if err != nil {
		t.Fatal(err)
	}
	if name != "settings.set.v1" || string(args) != `{"changes":[{"key":"pos.allow_negative_inventory","value":"on"}]}` {
		t.Fatalf("propose = %s %s", name, args)
	}
	b, _ := json.Marshal(out)
	if !strings.Contains(string(b), "awaiting_confirmation") || strings.Contains(string(b), "applied") {
		t.Fatalf("the model must be told the change awaits the operator: %s", b)
	}
	if got := takeProposals(); len(got) != 1 || got[0] != "0123456789abcdef0123456789abcdef" {
		t.Fatalf("collected proposals = %v", got)
	}
}

// Bad arguments never reach the till; a refusal by the till (key not on the
// allow-list, no grant, a cashier) is a result the model explains, with the
// link route, never a proposal.
func TestSettingsPropose_RefusalsAreResultsNotProposals_4129(t *testing.T) {
	h := newHost(t, ollamaSettings())
	calls := 0
	h.Propose = func(string, json.RawMessage) (string, error) {
		calls++
		return "", plugin.ErrInvalid
	}
	resetProposals()
	run := toolByName(t, "settings_propose")
	for _, a := range []map[string]any{
		nil, {"key": "", "value": "on"}, {"key": "x", "value": "maybe"}, {"key": 5.0, "value": "on"}, {"key": strings.Repeat("a", 65), "value": "on"},
	} {
		if _, err := run(a); err == nil {
			t.Errorf("args %v: want a tool error", a)
		}
	}
	if calls != 0 {
		t.Fatalf("%d bad calls reached the till", calls)
	}
	out, err := run(map[string]any{"key": "tax.default_rate", "value": "on"})
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(out)
	if !strings.Contains(string(b), "not_changeable") || !strings.Contains(string(b), "settings_describe") {
		t.Fatalf("refusal result = %s", b)
	}
	h.Propose = func(string, json.RawMessage) (string, error) { return "", plugin.ErrDenied }
	out, _ = run(map[string]any{"key": "pos.allow_negative_inventory", "value": "on"})
	b, _ = json.Marshal(out)
	if !strings.Contains(string(b), "not_permitted") {
		t.Fatalf("denied result = %s", b)
	}
	if len(takeProposals()) != 0 {
		t.Fatal("a refusal left a proposal")
	}
}

// The job draws the core confirm card for each proposal, after the answer.
func TestAskJob_DrawsConfirmCards_4129(t *testing.T) {
	h := newHost(t, ollamaSettings())
	h.InJob = true
	h.Views = func(name string, args json.RawMessage) (json.RawMessage, error) {
		return json.RawMessage(`[{"store_name":"","till_name":"T","currency_code":"GBP","currency_decimals":2,"locale":"en"}]`), nil
	}
	h.Propose = func(string, json.RawMessage) (string, error) { return "0123456789abcdef0123456789abcdef", nil }
	round := 0
	h.HTTP = func(req plugin.HTTPRequest) (plugin.HTTPResponse, error) {
		round++
		if round == 1 {
			if !strings.Contains(string(req.Body), "settings_propose") || !strings.Contains(string(req.Body), "settings_describe") {
				t.Errorf("settings tools not offered: %s", req.Body)
			}
			return plugin.HTTPResponse{Status: 200, Body: []byte(`{"message":{"role":"assistant","content":"","tool_calls":[{"function":{"name":"settings_propose","arguments":{"key":"pos.allow_negative_inventory","value":"on"}}}]}}`)}, nil
		}
		return plugin.HTTPResponse{Status: 200, Body: []byte(`{"message":{"role":"assistant","content":"Please confirm below."}}`)}, nil
	}
	answer, err := dispatch(t, AskJobEvent, jobPayload("I want to sell without looking at stock"))
	if err != nil {
		t.Fatal(err)
	}
	assertOwnKeys(t, answer)
	cs := components(t, answer)
	cf := ofType(cs, "confirm")
	if len(cf) != 1 || cf[0]["proposal"] != "0123456789abcdef0123456789abcdef" {
		t.Fatalf("confirm components = %v", cf)
	}
	if got := takeProposals(); len(got) != 0 {
		t.Fatalf("collector not drained: %v", got)
	}
}

func TestAskSystemPrompt_SettingsGuidanceOnlyWithTools_4129(t *testing.T) {
	if !strings.Contains(settingsGuidance, "settings_describe") || !strings.Contains(settingsGuidance, "never") {
		t.Fatalf("guidance = %q", settingsGuidance)
	}
}
