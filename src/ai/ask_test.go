//go:build !wasip1

package ai

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/universaltill/universal-till/sdk/plugin"
)

// Ported from universal-till internal/ai/ask_test.go.

func askService(t *testing.T, handler func(t *testing.T, req plugin.HTTPRequest) plugin.HTTPResponse) *Service {
	t.Helper()
	useServer(t, handler)
	return New(Config{Provider: "ollama", Endpoint: "http://localhost:11434", Model: "vision", AskModel: "text"})
}

func testTools(t *testing.T, gotArgs *map[string]any) []AskTool {
	t.Helper()
	return []AskTool{{
		Name:        "sales_by_day",
		Description: "daily sales",
		Params:      map[string]any{"type": "object"},
		Run: func(args map[string]any) (any, error) {
			*gotArgs = args
			return []map[string]any{{"day": "2026-07-14", "total": 12345}}, nil
		},
	}}
}

type ollamaAskReq struct {
	Model    string           `json:"model"`
	Messages []map[string]any `json:"messages"`
	Tools    []map[string]any `json:"tools"`
}

func decodeAsk(t *testing.T, req plugin.HTTPRequest) ollamaAskReq {
	t.Helper()
	var r ollamaAskReq
	if err := json.Unmarshal(req.Body, &r); err != nil {
		t.Fatalf("decode request: %v", err)
	}
	return r
}

func TestAskToolLoop(t *testing.T) {
	var gotArgs map[string]any
	call := 0
	svc := askService(t, func(t *testing.T, req plugin.HTTPRequest) plugin.HTTPResponse {
		call++
		r := decodeAsk(t, req)
		if r.Model != "text" {
			t.Errorf("ask used model %q, want the ask model", r.Model)
		}
		switch call {
		case 1:
			if len(r.Tools) != 1 {
				t.Errorf("tools not sent: %+v", r.Tools)
			}
			if r.Messages[0]["role"] != "system" || r.Messages[1]["content"] != "how did we do today?" {
				t.Errorf("system+question not sent: %+v", r.Messages)
			}
			return rawResponse(200, `{"message":{"role":"assistant","content":"","tool_calls":[{"function":{"name":"sales_by_day","arguments":{"days":7}}}]}}`)
		case 2:
			// The tool result must have come back as a role:"tool" message.
			last := r.Messages[len(r.Messages)-1]
			if last["role"] != "tool" || last["tool_name"] != "sales_by_day" || !strings.Contains(last["content"].(string), "12345") {
				t.Errorf("tool result not in messages: %+v", last)
			}
			return rawResponse(200, `{"message":{"role":"assistant","content":"You took £123.45 today."}}`)
		default:
			t.Fatalf("unexpected extra round %d", call)
		}
		return plugin.HTTPResponse{}
	})
	if !svc.CanAsk() {
		t.Fatal("ollama service should support ask")
	}
	answer, err := svc.Ask("how did we do today?", ShopContext{StoreName: "Test", CurrencyCode: "GBP", CurrencyDecimals: 2}, testTools(t, &gotArgs))
	if err != nil {
		t.Fatalf("Ask: %v", err)
	}
	if !strings.Contains(answer, "123.45") {
		t.Errorf("answer = %q", answer)
	}
	if gotArgs["days"] != float64(7) {
		t.Errorf("tool args = %+v", gotArgs)
	}
}

func TestAskLoopBounded(t *testing.T) {
	calls := 0
	svc := askService(t, func(*testing.T, plugin.HTTPRequest) plugin.HTTPResponse {
		calls++
		return rawResponse(200, `{"message":{"role":"assistant","content":"","tool_calls":[{"function":{"name":"sales_by_day","arguments":{}}}]}}`)
	})
	var gotArgs map[string]any
	if _, err := svc.Ask("loop forever", ShopContext{}, testTools(t, &gotArgs)); err == nil {
		t.Fatal("unbounded tool loop should error")
	}
	if calls != maxToolRounds {
		t.Fatalf("rounds = %d, want %d", calls, maxToolRounds)
	}
}

// The ask budget (askTimeout, core's context deadline) ends the loop
// between rounds: the plugin has no context to cancel an in-flight call,
// the job deadline (limits.long_call_s) covers that.
func TestAskBudgetEndsLoop(t *testing.T) {
	clock := time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)
	restore := now
	now = func() time.Time { return clock }
	t.Cleanup(func() { now = restore })
	calls := 0
	svc := askService(t, func(*testing.T, plugin.HTTPRequest) plugin.HTTPResponse {
		calls++
		clock = clock.Add(askTimeout) // each round eats the whole budget
		return rawResponse(200, `{"message":{"role":"assistant","content":"","tool_calls":[{"function":{"name":"sales_by_day","arguments":{}}}]}}`)
	})
	var gotArgs map[string]any
	_, err := svc.Ask("q", ShopContext{}, testTools(t, &gotArgs))
	if err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("want a timeout, got %v", err)
	}
	if calls != 1 {
		t.Fatalf("rounds after the budget ran out: %d", calls)
	}
}

func TestAskUnknownToolRecovers(t *testing.T) {
	call := 0
	svc := askService(t, func(t *testing.T, req plugin.HTTPRequest) plugin.HTTPResponse {
		call++
		if call == 1 {
			return rawResponse(200, `{"message":{"role":"assistant","content":"","tool_calls":[{"function":{"name":"nope","arguments":{}}}]}}`)
		}
		r := decodeAsk(t, req)
		last := r.Messages[len(r.Messages)-1]
		if !strings.Contains(last["content"].(string), "unknown tool") {
			t.Errorf("model not told about unknown tool: %+v", last)
		}
		return rawResponse(200, `{"message":{"role":"assistant","content":"Sorry, I cannot check that."}}`)
	})
	var gotArgs map[string]any
	if _, err := svc.Ask("q", ShopContext{}, testTools(t, &gotArgs)); err != nil {
		t.Fatalf("Ask should recover from unknown tool: %v", err)
	}
}

func TestCanAsk(t *testing.T) {
	if (&Service{}).CanAsk() {
		t.Error("disabled service must not offer ask")
	}
	if New(Config{Provider: "claude", APIKey: "k", Model: "m"}).CanAsk() {
		t.Error("claude provider has no ask loop yet — must report false")
	}
}

// Ask's guards: no question, a provider without the tool loop.
func TestAskGuards(t *testing.T) {
	svc := askService(t, func(*testing.T, plugin.HTTPRequest) plugin.HTTPResponse {
		return rawResponse(200, `{"message":{"role":"assistant","content":"hi"}}`)
	})
	if _, err := svc.Ask("", ShopContext{}, nil); err == nil {
		t.Fatal("empty question must error")
	}

	claude := New(Config{Provider: "claude", APIKey: "k", Model: "m"})
	if _, err := claude.Ask("q", ShopContext{}, nil); err == nil {
		t.Fatal("provider without a tool loop must error")
	}
}

func TestAskChatFailures(t *testing.T) {
	var gotArgs map[string]any

	svc := askService(t, func(*testing.T, plugin.HTTPRequest) plugin.HTTPResponse {
		return rawResponse(503, "overloaded")
	})
	if _, err := svc.Ask("q", ShopContext{}, testTools(t, &gotArgs)); err == nil || !strings.Contains(err.Error(), "overloaded") {
		t.Fatalf("chat http error must surface, got %v", err)
	}

	svc = askService(t, func(*testing.T, plugin.HTTPRequest) plugin.HTTPResponse {
		return rawResponse(200, "not json")
	})
	if _, err := svc.Ask("q", ShopContext{}, testTools(t, &gotArgs)); err == nil || !strings.Contains(err.Error(), "parse ollama response") {
		t.Fatalf("chat parse error must surface, got %v", err)
	}

	svc = askService(t, func(*testing.T, plugin.HTTPRequest) plugin.HTTPResponse {
		return rawResponse(200, `{"message":{"role":"assistant","content":"   "}}`)
	})
	if _, err := svc.Ask("q", ShopContext{}, testTools(t, &gotArgs)); err == nil || !strings.Contains(err.Error(), "empty model response") {
		t.Fatalf("blank final answer must error, got %v", err)
	}
}

// A tool that errors, and a tool whose result json.Marshal rejects, both go
// back to the model as "error:" text so it can recover — the loop never
// aborts on a tool problem.
func TestAskToolFailuresReturnToModel(t *testing.T) {
	failing := []AskTool{
		{
			Name: "boom", Description: "always fails", Params: map[string]any{"type": "object"},
			Run: func(map[string]any) (any, error) { return nil, fmt.Errorf("db locked") },
		},
		{
			Name: "unserialisable", Description: "bad result", Params: map[string]any{"type": "object"},
			Run: func(map[string]any) (any, error) { return func() {}, nil },
		},
	}
	call := 0
	svc := askService(t, func(t *testing.T, req plugin.HTTPRequest) plugin.HTTPResponse {
		call++
		if call == 1 {
			return rawResponse(200, `{"message":{"role":"assistant","content":"","tool_calls":[{"function":{"name":"boom","arguments":{}}},{"function":{"name":"unserialisable","arguments":{}}}]}}`)
		}
		r := decodeAsk(t, req)
		toolMsgs := r.Messages[len(r.Messages)-2:]
		for i, m := range toolMsgs {
			if m["role"] != "tool" || !strings.HasPrefix(m["content"].(string), "error:") {
				t.Errorf("tool failure %d not reported to model: %+v", i, m)
			}
		}
		if !strings.Contains(toolMsgs[0]["content"].(string), "db locked") {
			t.Errorf("tool error text lost: %+v", toolMsgs[0])
		}
		return rawResponse(200, `{"message":{"role":"assistant","content":"Sorry, the data is unavailable."}}`)
	})
	answer, err := svc.Ask("q", ShopContext{}, failing)
	if err != nil {
		t.Fatalf("loop must recover from tool failures: %v", err)
	}
	if !strings.Contains(answer, "unavailable") {
		t.Fatalf("answer = %q", answer)
	}
}

// Endpoint problems (a malformed URL, an unreachable server) surface as
// errors from the ask loop's chat layer too.
func TestAskEndpointFailures(t *testing.T) {
	var gotArgs map[string]any
	useDownServer(t)
	bad := New(Config{Provider: "ollama", Endpoint: "http://bad url", Model: "v", AskModel: "t"})
	if _, err := bad.Ask("q", ShopContext{}, testTools(t, &gotArgs)); err == nil {
		t.Fatal("malformed endpoint must error")
	}
	down := New(Config{Provider: "ollama", Endpoint: "http://127.0.0.1:1", Model: "v", AskModel: "t"})
	if _, err := down.Ask("q", ShopContext{}, testTools(t, &gotArgs)); err == nil {
		t.Fatal("unreachable endpoint must error")
	}
}

// The system prompt is core's, word for word (internal/ai/ask.go).
func TestAskSystemPrompt(t *testing.T) {
	got := askSystemPrompt(ShopContext{StoreName: "Corner Shop", CurrencyCode: "GBP", CurrencyDecimals: 2})
	want := fmt.Sprintf("You are the built-in assistant of a point-of-sale till in the shop %q. "+
		"You answer the shop manager's questions about sales, stock and till activity "+
		"using ONLY the provided tools — never invent figures. "+
		"All monetary amounts in tool results are integers in minor units of %s "+
		"(%d decimal places); convert them to normal readable amounts in your answer. "+
		"Today is %s. Be concise and practical: a short direct answer first, "+
		"then at most a few supporting numbers. If the data genuinely cannot answer "+
		"the question, say so plainly.", "Corner Shop", "GBP", 2, now().Format("Monday 2006-01-02"))
	if got != want {
		t.Fatalf("prompt drifted:\n got %s\nwant %s", got, want)
	}
}
