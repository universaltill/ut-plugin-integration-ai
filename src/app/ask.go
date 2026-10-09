package app

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/universaltill/universal-till/sdk/plugin"
	"github.com/universaltill/ut-plugin-integration-ai/src/ai"
)

// Ported from universal-till internal/pages/ask_api.go: the five Ask tools
// now read core data through core read views (ADR-0121 §5, view_query) —
// the views return exactly what the tools returned on the same data
// (core's TestCoreViewsMatchAskTools) — and the panel is a reports.panels
// content slot whose action runs the loop as a job.

// maxQuestionChars is core's 1–500 rule (core counted bytes; the plugin
// counts characters, so a non-Latin question gets the same 500).
const maxQuestionChars = 500

// maxAnswerBytes bounds the answer drawn on the panel (a 256 KiB document
// limit; 32 KiB is pages of text).
const maxAnswerBytes = 32 << 10

// shop grounds the system prompt. A plugin has no host function or view
// for the shop's name or currency yet, so the prompt names the shop the way
// core's own fallback did ("this shop", storeNameOrDefault) and the
// currency generically. Two decimal places is right for most currencies;
// for a zero-decimal one (JPY, KRW, …) the model may misplace the point.
var shop = ai.ShopContext{StoreName: "this shop", CurrencyCode: "the shop's currency", CurrencyDecimals: 2}

// intArg reads a bounded integer tool argument (JSON numbers arrive as
// float64); out-of-range or missing values fall back to def. The views are
// strict (an out-of-range value is -4), so the fallback happens here,
// exactly as core's tools did it.
func intArg(args map[string]any, k string, def, lo, hi int) int {
	f, ok := args[k].(float64)
	if !ok {
		return def
	}
	n := int(f)
	if n < lo || n > hi {
		return def
	}
	return n
}

func daysArg(args map[string]any) int { return intArg(args, "days", 14, 1, 365) }

func viewTool(view string, args map[string]int) (any, error) {
	raw, err := plugin.ViewQuery(view, args)
	if err != nil {
		return nil, err
	}
	return raw, nil
}

// askTools is the read-only tool surface for "Ask your till". Data
// red-line: identity-free aggregates only — no customer data, and the
// audit view returns counts, not payloads. Names, descriptions and
// parameter schemas are core's.
func askTools() []ai.AskTool {
	days := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"days": map[string]any{"type": "integer", "description": "how many days back to look (1-365)"},
		},
	}
	return []ai.AskTool{
		{
			Name:        "sales_by_day",
			Description: "Daily sales totals: per day the number of completed sales, revenue total and tax total (minor units). Returns/refunds are excluded, not netted.",
			Params:      days,
			Run: func(args map[string]any) (any, error) {
				return viewTool("sales.by_day.v1", map[string]int{"days": daysArg(args)})
			},
		},
		{
			Name:        "top_items",
			Description: "Best-selling items over a period: name, quantity sold, revenue (minor units). Returns/refunds are excluded.",
			Params: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"days":  map[string]any{"type": "integer", "description": "how many days back to look (1-365)"},
					"limit": map[string]any{"type": "integer", "description": "how many items to return (1-50)"},
				},
			},
			Run: func(args map[string]any) (any, error) {
				return viewTool("items.top.v1", map[string]int{"days": daysArg(args), "limit": intArg(args, "limit", 10, 1, 50)})
			},
		},
		{
			Name:        "payment_breakdown",
			Description: "Takings per payment method over a period: method, transaction count, amount taken (minor units).",
			Params:      days,
			Run: func(args map[string]any) (any, error) {
				return viewTool("payments.breakdown.v1", map[string]int{"days": daysArg(args)})
			},
		},
		{
			Name:        "stock_levels",
			Description: "Current stock levels per item and location, including reorder levels and lead time (days to receive a reorder). Use for questions about stock, low stock and reordering.",
			Params:      map[string]any{"type": "object", "properties": map[string]any{}},
			Run: func(map[string]any) (any, error) {
				return viewTool("stock.levels.v1", map[string]int{})
			},
		},
		{
			Name:        "till_activity_summary",
			Description: "Counts of till audit actions per operator over a period (logins, voids, no-sales, stock overrides…). Use for questions about staff activity or anything suspicious.",
			Params:      days,
			Run: func(args map[string]any) (any, error) {
				return viewTool("audit.summary.v1", map[string]int{"days": daysArg(args)})
			},
		},
	}
}

// uiPayload is ui.view.ask / ui.action.ask / the job's payload.
type uiPayload struct {
	View   string         `json:"view"`
	Action string         `json:"action"`
	Form   map[string]any `json:"form"`
}

func (p uiPayload) question() string {
	q, _ := p.Form["question"].(string)
	return strings.TrimSpace(q)
}

func validQuestion(q string) bool {
	return q != "" && utf8.RuneCountInString(q) <= maxQuestionChars
}

// askForm is the question form, refilled with the operator's text so they
// can ask again (or fix it).
func askForm(value string) component {
	field := map[string]any{"name": "question", "label": key("integration_ai.ask.question"), "kind": "text", "required": true}
	if value != "" {
		field["value"] = truncate(value, maxLiteralBytes)
	}
	return component{"type": "form", "action": "ask", "submit": key("integration_ai.ask.button"), "fields": []any{field}}
}

// askPanel is the panel: hint, form, then whatever follows.
func askPanel(value string, rest ...component) map[string]any {
	cs := append([]component{text(key("integration_ai.ask.hint")), askForm(value)}, rest...)
	return document(cs...)
}

// canAsk: AI configured on a provider with a tool loop (core's
// Service.CanAsk gate).
func canAsk() (*ai.Service, bool) {
	svc := service()
	return svc, svc.CanAsk()
}

func handleView(e plugin.Event) (any, error) {
	var p uiPayload
	if err := e.Decode(&p); err != nil {
		return nil, err
	}
	if p.View != AskView {
		return nil, fmt.Errorf("unknown view %q", p.View)
	}
	if _, ok := canAsk(); !ok {
		// Core hid the panel when AI is off or the provider has no ask loop
		// (claude): a slot entry that gives no answer is skipped, so the
		// panel is not drawn.
		return nil, nil
	}
	return askPanel(""), nil
}

func handleAction(e plugin.Event) (any, error) {
	var p uiPayload
	if err := e.Decode(&p); err != nil {
		return nil, err
	}
	if p.View != AskView || p.Action != "ask" {
		return nil, fmt.Errorf("unknown action %q on view %q", p.Action, p.View)
	}
	q := p.question()
	if _, ok := canAsk(); !ok {
		return askPanel(q, notice("warn", "integration_ai.ask.unavailable")), nil
	}
	if !validQuestion(q) {
		return askPanel(q, notice("error", "integration_ai.ask.question_invalid")), nil
	}
	// The tool loop can take far longer than an ask's deadline: run it as a
	// job (plugin-views.md "Jobs"). The job gets this payload, form
	// included.
	return map[string]any{"job": map[string]any{"event": AskJobEvent}}, nil
}

func handleAskJob(e plugin.Event) (any, error) {
	var p uiPayload
	if err := e.Decode(&p); err != nil {
		return nil, err
	}
	q := p.question()
	svc, ok := canAsk()
	if !ok {
		return askPanel(q, notice("warn", "integration_ai.ask.unavailable")), nil
	}
	if !validQuestion(q) {
		return askPanel(q, notice("error", "integration_ai.ask.question_invalid")), nil
	}
	progress(10, "integration_ai.ask.thinking")
	answer, err := svc.Ask(q, shop, askTools())
	if err != nil {
		plugin.Logf("ai ask failed: %v", err)
		return askPanel(q, notice("error", "integration_ai.ask.error")), nil
	}
	rest := []component{component{"type": "heading", "text": key("integration_ai.ask.answer")}}
	for _, c := range chunks(truncate(strings.TrimSpace(answer), maxAnswerBytes), maxLiteralBytes) {
		rest = append(rest, text(literal(c)))
	}
	return askPanel(q, rest...), nil
}
