//go:build !wasip1

package app

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/universaltill/universal-till/sdk/plugin"
)

// "Ask your till" as a reports.panels content slot (ut-docs#4032): core's
// /api/reports/ask (internal/pages/ask_api.go) becomes a view, an action
// that starts a job, and the job that runs the tool loop through core read
// views.

func viewPayload() map[string]any {
	return map[string]any{"view": AskView, "params": map[string]any{"slot": "reports.panels", "days": "7"}, "locale": "en"}
}

func actionPayload(action, question string) map[string]any {
	return map[string]any{"view": AskView, "action": action, "form": map[string]any{"question": question}, "invalid": []any{}, "upload_handles": []any{}, "locale": "en"}
}

func jobPayload(question string) map[string]any {
	p := actionPayload("ask", question)
	p["job_id"] = "9f0c0000000000000000000000000000"
	return p
}

// findAskForm returns the document's one form component.
func findAskForm(t *testing.T, cs []map[string]any) map[string]any {
	t.Helper()
	forms := ofType(cs, "form")
	if len(forms) != 1 {
		t.Fatalf("want one form, got %v", cs)
	}
	f := forms[0]
	if f["action"] != "ask" || f["submit"].(map[string]any)["key"] != "integration_ai.ask.button" {
		t.Fatalf("form = %v", f)
	}
	fields := f["fields"].([]any)
	if len(fields) != 1 {
		t.Fatalf("fields = %v", fields)
	}
	q := fields[0].(map[string]any)
	if q["name"] != "question" || q["kind"] != "text" || q["required"] != true {
		t.Fatalf("question field = %v", q)
	}
	return q
}

func TestAskView_DrawsTheForm(t *testing.T) {
	newHost(t, ollamaSettings())
	answer, err := dispatch(t, "ui.view.ask", viewPayload())
	if err != nil {
		t.Fatal(err)
	}
	assertOwnKeys(t, answer)
	cs := components(t, answer)
	findAskForm(t, cs)
	if keys := textKeys(cs); len(keys) != 1 || keys[0] != "integration_ai.ask.hint" {
		t.Fatalf("keys = %v", keys)
	}
}

// Core hid the panel when the provider has no ask loop (claude) or AI is
// not configured; in a slot "no answer" is skipped, so the panel hides.
func TestAskView_HiddenWithoutAskLoop(t *testing.T) {
	for name, settings := range map[string]map[string]string{
		"claude":         {"provider": "claude", "api_key": "k"},
		"not configured": {"provider": "self_hosted"},
	} {
		t.Run(name, func(t *testing.T) {
			h := newHost(t, settings)
			answer, err := dispatch(t, "ui.view.ask", viewPayload())
			if err != nil || answer != nil {
				t.Fatalf("answer=%v err=%v, want no answer", answer, err)
			}
			if h.Calls["http_request"] != 0 {
				t.Fatal("no model call to draw a view")
			}
		})
	}
}

func TestAskView_UnknownViewFails(t *testing.T) {
	newHost(t, ollamaSettings())
	if _, err := dispatch(t, "ui.view.ask", map[string]any{"view": "other", "locale": "en"}); err == nil {
		t.Fatal("an unknown view must fail")
	}
}

func TestAskAction_StartsTheJob(t *testing.T) {
	h := newHost(t, ollamaSettings())
	answer, err := dispatch(t, "ui.action.ask", actionPayload("ask", "  What sold best this week?  "))
	if err != nil {
		t.Fatal(err)
	}
	job, ok := answer["job"].(map[string]any)
	if !ok || job["event"] != AskJobEvent || len(answer) != 1 {
		t.Fatalf("answer = %v", answer)
	}
	if AskJobEvent != "com.universaltill.integration-ai.ask" || !strings.HasPrefix(AskJobEvent, PluginID+".") {
		t.Fatalf("job event %q is not in the plugin's own namespace", AskJobEvent)
	}
	if h.Calls["http_request"] != 0 {
		t.Fatal("the action must not call the model: the job does")
	}
}

// Core's 1–500 character rule; a bad question gets the form back with the
// operator's text and a notice, never a job.
func TestAskAction_ValidatesQuestion(t *testing.T) {
	for name, q := range map[string]string{"empty": "   ", "too long": strings.Repeat("a", 501)} {
		t.Run(name, func(t *testing.T) {
			newHost(t, ollamaSettings())
			answer, err := dispatch(t, "ui.action.ask", actionPayload("ask", q))
			if err != nil {
				t.Fatal(err)
			}
			assertOwnKeys(t, answer)
			cs := components(t, answer)
			if keys := textKeys(cs); len(keys) != 2 || keys[1] != "integration_ai.ask.question_invalid" {
				t.Fatalf("keys = %v", keys)
			}
			if name == "too long" {
				if v, _ := findAskForm(t, cs)["value"].(string); len(v) > maxLiteralBytes {
					t.Fatalf("refill value over the 4 KiB string limit: %d", len(v))
				}
			}
		})
	}
}

func TestAskAction_UnavailableProviderAnswersNotice(t *testing.T) {
	newHost(t, map[string]string{"provider": "claude", "api_key": "k"})
	answer, err := dispatch(t, "ui.action.ask", actionPayload("ask", "q"))
	if err != nil {
		t.Fatal(err)
	}
	if keys := textKeys(components(t, answer)); keys[len(keys)-1] != "integration_ai.ask.unavailable" {
		t.Fatalf("keys = %v", keys)
	}
}

func TestAskAction_UnknownActionFails(t *testing.T) {
	newHost(t, ollamaSettings())
	if _, err := dispatch(t, "ui.action.ask", actionPayload("delete_everything", "q")); err == nil {
		t.Fatal("an unknown action must fail")
	}
}

// The job runs core's tool loop with the five Ask tools, each backed by its
// core read view (ADR-0121 §5), and answers the model's text as a literal
// under the form, which keeps the question for the next ask.
func TestAskJob_RunsToolLoopThroughViews(t *testing.T) {
	h := newHost(t, ollamaSettings())
	h.InJob = true
	var viewCalls []string
	h.Views = func(name string, args json.RawMessage) (json.RawMessage, error) {
		if name == "shop.context.v1" {
			return json.RawMessage(`[{"store_name":"","till_name":"Till 1","currency_code":"GBP","currency_decimals":2,"locale":"en"}]`), nil
		}
		viewCalls = append(viewCalls, name+string(args))
		return json.RawMessage(`[{"day":"2026-10-09","count":3,"total":12345,"tax_total":2057}]`), nil
	}
	round := 0
	h.HTTP = func(req plugin.HTTPRequest) (plugin.HTTPResponse, error) {
		round++
		var body struct {
			Model    string           `json:"model"`
			Messages []map[string]any `json:"messages"`
			Tools    []map[string]any `json:"tools"`
		}
		_ = json.Unmarshal(req.Body, &body)
		if body.Model != "llama3.2" {
			t.Errorf("model = %s, want the ask model", body.Model)
		}
		switch round {
		case 1:
			var names []string
			for _, tl := range body.Tools {
				names = append(names, tl["function"].(map[string]any)["name"].(string))
			}
			if strings.Join(names, ",") != "sales_by_day,top_items,payment_breakdown,stock_levels,till_activity_summary" {
				t.Errorf("tools = %v", names)
			}
			sys := body.Messages[0]["content"].(string)
			if !strings.Contains(sys, `in the shop "this shop"`) {
				t.Errorf("system prompt = %s", sys)
			}
			if body.Messages[1]["content"] != "How did we do?" {
				t.Errorf("question = %v", body.Messages[1])
			}
			return plugin.HTTPResponse{Status: 200, Body: []byte(`{"message":{"role":"assistant","content":"","tool_calls":[{"function":{"name":"sales_by_day","arguments":{"days":7}}},{"function":{"name":"top_items","arguments":{"days":9999,"limit":0}}}]}}`)}, nil
		default:
			last := body.Messages[len(body.Messages)-1]
			if !strings.Contains(last["content"].(string), "12345") {
				t.Errorf("view rows not returned to the model: %v", last)
			}
			return plugin.HTTPResponse{Status: 200, Body: []byte(`{"message":{"role":"assistant","content":"You took 123.45 today."}}`)}, nil
		}
	}
	answer, err := dispatch(t, AskJobEvent, jobPayload("How did we do?"))
	if err != nil {
		t.Fatal(err)
	}
	assertOwnKeys(t, answer)
	cs := components(t, answer)
	if q := findAskForm(t, cs); q["value"] != "How did we do?" {
		t.Fatalf("form must keep the question, got %v", q["value"])
	}
	if !strings.Contains(literals(cs), "You took 123.45 today.") {
		t.Fatalf("answer text missing: %v", cs)
	}
	// Out-of-range tool args fall back to the tool's default (core intArg)
	// rather than reaching the strict view as a -4.
	want := []string{`sales.by_day.v1{"days":7}`, `items.top.v1{"days":14,"limit":10}`}
	if strings.Join(viewCalls, " ") != strings.Join(want, " ") {
		t.Fatalf("view calls = %v, want %v", viewCalls, want)
	}
	if len(h.Progress) == 0 {
		t.Fatal("no progress reported")
	}
}

// A provider failure answers core's "could not answer" text as an error
// notice, with the form kept (core rendered reports.ask.error).
func TestAskJob_ProviderErrorAnswersNotice(t *testing.T) {
	h := newHost(t, ollamaSettings())
	h.HTTP = func(plugin.HTTPRequest) (plugin.HTTPResponse, error) {
		return plugin.HTTPResponse{Status: 503, Body: []byte("overloaded")}, nil
	}
	answer, err := dispatch(t, AskJobEvent, jobPayload("q"))
	if err != nil {
		t.Fatal(err)
	}
	assertOwnKeys(t, answer)
	cs := components(t, answer)
	findAskForm(t, cs)
	notices := ofType(cs, "notice")
	if len(notices) != 1 || notices[0]["level"] != "error" || notices[0]["text"].(map[string]any)["key"] != "integration_ai.ask.error" {
		t.Fatalf("notices = %v", notices)
	}
	if strings.Contains(literals(cs), "overloaded") {
		t.Fatal("the raw provider error must not reach the operator")
	}
}

// The job re-validates: a payload replayed with a bad question never
// reaches the model.
func TestAskJob_RevalidatesQuestion(t *testing.T) {
	h := newHost(t, ollamaSettings())
	answer, err := dispatch(t, AskJobEvent, jobPayload(""))
	if err != nil {
		t.Fatal(err)
	}
	if keys := textKeys(components(t, answer)); keys[len(keys)-1] != "integration_ai.ask.question_invalid" {
		t.Fatalf("keys = %v", keys)
	}
	if h.Calls["http_request"] != 0 {
		t.Fatal("no model call for an invalid question")
	}
}

// A long answer is split into ≤4 KiB literals at rune boundaries (the view
// format's string limit), losing nothing.
func TestAskJob_LongAnswerSplitIntoLiterals(t *testing.T) {
	long := strings.Repeat("Sales were steady. Ünïcödé ✓\n", 400) // ~12 KiB
	h := newHost(t, ollamaSettings())
	h.HTTP = func(plugin.HTTPRequest) (plugin.HTTPResponse, error) {
		b, _ := json.Marshal(map[string]any{"message": map[string]any{"role": "assistant", "content": long}})
		return plugin.HTTPResponse{Status: 200, Body: b}, nil
	}
	answer, err := dispatch(t, AskJobEvent, jobPayload("q"))
	if err != nil {
		t.Fatal(err)
	}
	cs := components(t, answer)
	texts := 0
	for _, c := range ofType(cs, "text") {
		if l, ok := c["text"].(map[string]any)["literal"].(string); ok {
			texts++
			if len(l) > maxLiteralBytes {
				t.Fatalf("literal of %d bytes", len(l))
			}
		}
	}
	if texts < 3 || literals(cs) != strings.TrimSpace(long) {
		t.Fatalf("answer not split losslessly: %d literals", texts)
	}
}

// The five tools map onto their core views with core's argument windows:
// days 1–365 (default 14), limit 1–50 (default 10); stock_levels takes
// none. A view error goes back to the model as "error:" text.
func TestAskTools_MapOntoCoreViews(t *testing.T) {
	h := newHost(t, ollamaSettings())
	var got []string
	h.Views = func(name string, args json.RawMessage) (json.RawMessage, error) {
		got = append(got, name+string(args))
		if name == "audit.summary.v1" {
			return nil, plugin.ErrDenied
		}
		return json.RawMessage(`[]`), nil
	}
	tools := askTools()
	byName := map[string]func(map[string]any) (any, error){}
	for _, tl := range tools {
		byName[tl.Name] = tl.Run
	}
	calls := []struct {
		tool string
		args map[string]any
	}{
		{"sales_by_day", map[string]any{"days": float64(30)}},
		{"sales_by_day", map[string]any{"days": float64(0)}},
		{"sales_by_day", map[string]any{"days": "7"}},
		{"top_items", map[string]any{"days": float64(365), "limit": float64(50)}},
		{"top_items", map[string]any{"limit": float64(51)}},
		{"payment_breakdown", nil},
		{"stock_levels", map[string]any{"days": float64(3)}},
	}
	for _, c := range calls {
		if _, err := byName[c.tool](c.args); err != nil {
			t.Fatalf("%s: %v", c.tool, err)
		}
	}
	if _, err := byName["till_activity_summary"](map[string]any{"days": float64(2)}); err == nil {
		t.Fatal("a view error must reach the model as an error")
	}
	want := []string{
		`sales.by_day.v1{"days":30}`,
		`sales.by_day.v1{"days":14}`,
		`sales.by_day.v1{"days":14}`,
		`items.top.v1{"days":365,"limit":50}`,
		`items.top.v1{"days":14,"limit":10}`,
		`payments.breakdown.v1{"days":14}`,
		`stock.levels.v1{}`,
		`audit.summary.v1{"days":2}`,
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("view calls:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	// Descriptions are core's (the model's only guide to the data).
	for _, tl := range tools {
		if tl.Description == "" || tl.Params["type"] != "object" {
			t.Fatalf("tool %s malformed: %+v", tl.Name, tl)
		}
	}
}

// Every view the plugin queries is declared in manifest.json's views_used
// with its view:<class> permission — the till refuses a view_query for
// anything else (-2).
func TestManifestDeclaresEveryViewAndHook(t *testing.T) {
	m := loadManifest(t)
	if m.ID != PluginID {
		t.Fatalf("manifest id %q != PluginID %q", m.ID, PluginID)
	}
	views := map[string]string{
		"sales.by_day.v1": "view:sales", "items.top.v1": "view:sales", "payments.breakdown.v1": "view:sales",
		"stock.levels.v1": "view:inventory", "audit.summary.v1": "view:audit", "catalog.items.v1": "view:inventory",
		"shop.context.v1": "view:sales",
	}
	used := map[string]bool{}
	for _, v := range m.ViewsUsed {
		used[v] = true
	}
	perms := map[string]bool{}
	for _, p := range m.Permissions {
		perms[p] = true
	}
	for v, perm := range views {
		if !used[v] || !perms[perm] {
			t.Errorf("view %s: views_used=%v, %s granted=%v", v, used[v], perm, perms[perm])
		}
	}
	if len(m.ViewsUsed) != len(views) {
		t.Errorf("views_used lists views the plugin never queries: %v", m.ViewsUsed)
	}
	hooks := map[string]bool{}
	for _, hk := range m.Hooks {
		hooks[hk.Event] = true
	}
	for ev := range Handlers() {
		if !hooks[ev] {
			t.Errorf("handler for %q has no manifest hook", ev)
		}
	}
	if len(hooks) != len(Handlers()) {
		t.Errorf("hooks %v vs handlers", hooks)
	}
	var entry *manifestEntry
	for i := range m.Entries {
		if m.Entries[i].View == AskView {
			entry = &m.Entries[i]
		}
	}
	if entry == nil || entry.Slot != "reports.panels" || !strings.HasPrefix(entry.Route, "/plugin/") || !perms["ui:slot:reports.panels"] {
		t.Fatalf("ask entry = %+v", entry)
	}
	if _, ok := enKeys(t)[entry.Label]; !ok {
		t.Fatalf("entry label %q not in the bundle", entry.Label)
	}
}

// askPrompt runs one Ask job on a till whose shop.context.v1 is the given
// stub and returns the system prompt the model was sent, the answer
// document's literals and the shop.context.v1 argument bytes it was called
// with ("" when never called).
func askPrompt(t *testing.T, shopView func(args json.RawMessage) (json.RawMessage, error)) (sys, answer, shopArgs string) {
	t.Helper()
	h := newHost(t, ollamaSettings())
	h.InJob = true
	h.Views = func(name string, args json.RawMessage) (json.RawMessage, error) {
		if name != "shop.context.v1" {
			t.Errorf("unexpected view %s", name)
			return json.RawMessage(`[]`), nil
		}
		shopArgs = string(args)
		return shopView(args)
	}
	h.HTTP = func(req plugin.HTTPRequest) (plugin.HTTPResponse, error) {
		var body struct {
			Messages []map[string]any `json:"messages"`
		}
		_ = json.Unmarshal(req.Body, &body)
		sys, _ = body.Messages[0]["content"].(string)
		return plugin.HTTPResponse{Status: 200, Body: []byte(`{"message":{"role":"assistant","content":"Fine."}}`)}, nil
	}
	out, err := dispatch(t, AskJobEvent, jobPayload("How did we do?"))
	if err != nil {
		t.Fatal(err)
	}
	return sys, literals(components(t, out)), shopArgs
}

func shopRow(row string) func(json.RawMessage) (json.RawMessage, error) {
	return func(json.RawMessage) (json.RawMessage, error) { return json.RawMessage(row), nil }
}

// The prompt names the shop and currency from shop.context.v1 (no
// arguments: any argument is -4), so a zero-decimal shop is told so.
func TestAskJob_PromptUsesShopContext(t *testing.T) {
	sys, answer, args := askPrompt(t, shopRow(`[{"store_name":"Sample Shop JP","till_name":"Till 1","currency_code":"JPY","currency_decimals":0,"locale":"ja"}]`))
	for _, want := range []string{`in the shop "Sample Shop JP"`, `minor units of JPY (0 decimal places)`} {
		if !strings.Contains(sys, want) {
			t.Errorf("system prompt lacks %q: %s", want, sys)
		}
	}
	if args != "{}" {
		t.Errorf("shop.context.v1 args = %q, want {}", args)
	}
	if !strings.Contains(answer, "Fine.") {
		t.Errorf("answer = %q", answer)
	}
}

// An older till has no such view: the fallback prompt is used and the
// answer is never blocked.
func TestAskJob_ShopContextUnavailableKeepsFallback(t *testing.T) {
	for name, stub := range map[string]func(json.RawMessage) (json.RawMessage, error){
		"view error": func(json.RawMessage) (json.RawMessage, error) { return nil, errors.New("unknown view") },
		"bad json":   shopRow(`not json`),
		"no rows":    shopRow(`[]`),
		"two rows":   shopRow(`[{"store_name":"A","currency_code":"JPY","currency_decimals":0},{"store_name":"B"}]`),
	} {
		t.Run(name, func(t *testing.T) {
			sys, answer, _ := askPrompt(t, stub)
			for _, want := range []string{`in the shop "this shop"`, `the shop's currency (2 decimal places)`} {
				if !strings.Contains(sys, want) {
					t.Errorf("system prompt lacks %q: %s", want, sys)
				}
			}
			if !strings.Contains(answer, "Fine.") {
				t.Errorf("answer = %q", answer)
			}
		})
	}
}

// An unset store name keeps "this shop"; the real currency is still used.
func TestAskJob_EmptyStoreNameKeepsFallbackName(t *testing.T) {
	sys, _, _ := askPrompt(t, shopRow(`[{"store_name":"  ","till_name":"Till 1","currency_code":" EUR ","currency_decimals":2,"locale":"de"}]`))
	for _, want := range []string{`in the shop "this shop"`, `minor units of EUR (2 decimal places)`} {
		if !strings.Contains(sys, want) {
			t.Errorf("system prompt lacks %q: %s", want, sys)
		}
	}
}

// Decimals outside 0..4 are not trusted (defensive bound): back to 2.
func TestAskJob_OutOfRangeDecimalsFallBack(t *testing.T) {
	for _, d := range []string{"9", "-1", "5"} {
		sys, _, _ := askPrompt(t, shopRow(`[{"store_name":"S","currency_code":"XYZ","currency_decimals":`+d+`,"locale":"en"}]`))
		if !strings.Contains(sys, `minor units of XYZ (2 decimal places)`) {
			t.Errorf("decimals %s: system prompt = %s", d, sys)
		}
	}
}
