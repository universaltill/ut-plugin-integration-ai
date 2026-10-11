//go:build !wasip1

package ai

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/universaltill/universal-till/sdk/plugin"
)

// ut-docs#2835: the claude provider runs the same "Ask your till" tool loop
// as ollama and openai, on the Anthropic Messages API wire format.

type claudeAskReq struct {
	Model     string           `json:"model"`
	MaxTokens int              `json:"max_tokens"`
	System    string           `json:"system"`
	Tools     []map[string]any `json:"tools"`
	Messages  []map[string]any `json:"messages"`
}

func decodeClaudeAsk(t *testing.T, req plugin.HTTPRequest) claudeAskReq {
	t.Helper()
	var r claudeAskReq
	if err := json.Unmarshal(req.Body, &r); err != nil {
		t.Fatalf("decode claude ask request: %v", err)
	}
	return r
}

func claudeToolUseResponse(id, name string, input map[string]any) plugin.HTTPResponse {
	return jsonResponse(200, map[string]any{
		"id": "msg_tu", "type": "message", "role": "assistant", "model": "test-model",
		"content": []map[string]any{
			{"type": "text", "text": "Let me check."},
			{"type": "tool_use", "id": id, "name": name, "input": input},
		},
		"stop_reason": "tool_use",
	})
}

func claudeAskService(t *testing.T, handler func(t *testing.T, req plugin.HTTPRequest) plugin.HTTPResponse) *Service {
	t.Helper()
	useServer(t, handler)
	return New(Config{Provider: "claude", APIKey: "test-key", Model: "test-model"})
}

func TestClaudeAskToolLoop(t *testing.T) {
	var gotArgs map[string]any
	call := 0
	svc := claudeAskService(t, func(t *testing.T, req plugin.HTTPRequest) plugin.HTTPResponse {
		call++
		if req.URL != "https://api.anthropic.com/v1/messages" {
			t.Errorf("url = %s", req.URL)
		}
		if req.Headers["x-api-key"] != "test-key" || req.Headers["anthropic-version"] != "2023-06-01" {
			t.Errorf("headers = %v", req.Headers)
		}
		r := decodeClaudeAsk(t, req)
		if r.Model != "test-model" || r.MaxTokens <= 0 {
			t.Errorf("model/max_tokens = %q/%d", r.Model, r.MaxTokens)
		}
		if !strings.Contains(r.System, "point-of-sale till") {
			t.Errorf("system prompt not sent: %q", r.System)
		}
		if len(r.Tools) != 1 || r.Tools[0]["name"] != "sales_by_day" || r.Tools[0]["description"] != "daily sales" {
			t.Errorf("tools = %+v", r.Tools)
		}
		if schema, _ := r.Tools[0]["input_schema"].(map[string]any); schema["type"] != "object" {
			t.Errorf("input_schema = %+v", r.Tools[0]["input_schema"])
		}
		switch call {
		case 1:
			if len(r.Messages) != 1 || r.Messages[0]["role"] != "user" || r.Messages[0]["content"] != "how did we do today?" {
				t.Errorf("first round messages = %+v", r.Messages)
			}
			return claudeToolUseResponse("toolu_1", "sales_by_day", map[string]any{"days": 7})
		case 2:
			if len(r.Messages) != 3 {
				t.Fatalf("second round messages = %+v", r.Messages)
			}
			// The assistant turn is echoed back verbatim, tool_use block included.
			asst := r.Messages[1]
			blocks, _ := asst["content"].([]any)
			if asst["role"] != "assistant" || len(blocks) != 2 {
				t.Fatalf("assistant turn not echoed: %+v", asst)
			}
			if tu, _ := blocks[1].(map[string]any); tu["type"] != "tool_use" || tu["id"] != "toolu_1" {
				t.Fatalf("tool_use block not echoed: %+v", blocks[1])
			}
			user := r.Messages[2]
			results, _ := user["content"].([]any)
			if user["role"] != "user" || len(results) != 1 {
				t.Fatalf("tool results turn = %+v", user)
			}
			res, _ := results[0].(map[string]any)
			if res["type"] != "tool_result" || res["tool_use_id"] != "toolu_1" || !strings.Contains(res["content"].(string), "12345") {
				t.Fatalf("tool_result = %+v", res)
			}
			return jsonResponse(200, map[string]any{
				"content": []map[string]any{
					{"type": "text", "text": "You took £123.45 today."},
					{"type": "text", "text": "Best day this week."},
				},
				"stop_reason": "end_turn",
			})
		default:
			t.Fatalf("unexpected extra round %d", call)
		}
		return plugin.HTTPResponse{}
	})
	if !svc.CanAsk() {
		t.Fatal("claude implements the ask loop — CanAsk must be true")
	}
	answer, err := svc.Ask("how did we do today?", ShopContext{StoreName: "Shop"}, testTools(t, &gotArgs))
	if err != nil {
		t.Fatal(err)
	}
	if answer != "You took £123.45 today.\nBest day this week." {
		t.Fatalf("answer = %q", answer)
	}
	if gotArgs["days"] != float64(7) {
		t.Fatalf("tool args = %+v", gotArgs)
	}
}

// Several tool_use blocks in one turn → all results in ONE user message.
func TestClaudeAskParallelToolCallsOneResultTurn(t *testing.T) {
	var gotArgs map[string]any
	call := 0
	svc := claudeAskService(t, func(t *testing.T, req plugin.HTTPRequest) plugin.HTTPResponse {
		call++
		if call == 1 {
			return jsonResponse(200, map[string]any{
				"content": []map[string]any{
					{"type": "tool_use", "id": "a", "name": "sales_by_day", "input": map[string]any{}},
					{"type": "tool_use", "id": "b", "name": "nope", "input": map[string]any{}},
				},
				"stop_reason": "tool_use",
			})
		}
		r := decodeClaudeAsk(t, req)
		results, _ := r.Messages[len(r.Messages)-1]["content"].([]any)
		if len(results) != 2 {
			t.Fatalf("want both tool results in one turn, got %+v", r.Messages[len(r.Messages)-1])
		}
		second, _ := results[1].(map[string]any)
		if second["tool_use_id"] != "b" || !strings.Contains(second["content"].(string), "unknown tool") {
			t.Fatalf("unknown tool result = %+v", second)
		}
		return claudeTextResponse("done", "end_turn")
	})
	if _, err := svc.Ask("q", ShopContext{}, testTools(t, &gotArgs)); err != nil {
		t.Fatal(err)
	}
}

func TestClaudeAskLoopBounded(t *testing.T) {
	calls := 0
	svc := claudeAskService(t, func(*testing.T, plugin.HTTPRequest) plugin.HTTPResponse {
		calls++
		return claudeToolUseResponse("t", "sales_by_day", map[string]any{})
	})
	var gotArgs map[string]any
	if _, err := svc.Ask("loop", ShopContext{}, testTools(t, &gotArgs)); err == nil {
		t.Fatal("unbounded tool loop should error")
	}
	if calls != maxToolRounds {
		t.Fatalf("rounds = %d, want %d", calls, maxToolRounds)
	}
}

func TestClaudeAskBudgetEndsLoop(t *testing.T) {
	clock := time.Date(2026, 10, 11, 12, 0, 0, 0, time.UTC)
	restore := now
	now = func() time.Time { return clock }
	t.Cleanup(func() { now = restore })
	calls := 0
	svc := claudeAskService(t, func(*testing.T, plugin.HTTPRequest) plugin.HTTPResponse {
		calls++
		clock = clock.Add(askTimeout)
		return claudeToolUseResponse("t", "sales_by_day", map[string]any{})
	})
	var gotArgs map[string]any
	if _, err := svc.Ask("q", ShopContext{}, testTools(t, &gotArgs)); err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("want a timeout, got %v", err)
	}
	if calls != 1 {
		t.Fatalf("rounds after the budget ran out: %d", calls)
	}
}

func TestClaudeAskFailures(t *testing.T) {
	var gotArgs map[string]any
	cases := map[string]struct {
		resp plugin.HTTPResponse
		want string
		not  string
	}{
		"refusal":    {claudeTextResponse("", "refusal"), "declined", ""},
		"empty":      {claudeTextResponse("  ", "end_turn"), "empty model response", ""},
		"not json":   {rawResponse(200, "nope"), "parse claude response", ""},
		"overloaded": {rawResponse(529, `{"error":"overloaded"}`), "overloaded", ""},
		"auth 401":   {rawResponse(401, `{"error":"invalid x-api-key sk-ant-SECRET"}`), "authentication failed", "SECRET"},
		"auth 403":   {rawResponse(403, `{"error":"sk-ant-SECRET"}`), "authentication failed", "SECRET"},
		"max_tokens": {claudeTextResponse("half an answ", "max_tokens"), "cut off", ""},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			svc := claudeAskService(t, func(*testing.T, plugin.HTTPRequest) plugin.HTTPResponse { return c.resp })
			_, err := svc.Ask("q", ShopContext{}, testTools(t, &gotArgs))
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("want %q, got %v", c.want, err)
			}
			if c.not != "" && strings.Contains(err.Error(), c.not) {
				t.Fatalf("auth error leaked the body: %v", err)
			}
		})
	}
}

// The assistant turn goes back byte-for-byte as received: thinking blocks
// keep their signature (the API rejects them otherwise), tool_use keeps its
// input, unknown block types survive (review finding, ut-docs#2835).
func TestClaudeAskEchoesAssistantContentVerbatim(t *testing.T) {
	var gotArgs map[string]any
	first := []map[string]any{
		{"type": "thinking", "thinking": "", "signature": "EqQBsig"},
		{"type": "redacted_thinking", "data": "abc"},
		{"type": "tool_use", "id": "t1", "name": "sales_by_day", "input": map[string]any{"days": 3}},
	}
	call := 0
	svc := claudeAskService(t, func(t *testing.T, req plugin.HTTPRequest) plugin.HTTPResponse {
		call++
		if call == 1 {
			return jsonResponse(200, map[string]any{"content": first, "stop_reason": "tool_use"})
		}
		r := decodeClaudeAsk(t, req)
		got, _ := json.Marshal(r.Messages[1]["content"])
		want, _ := json.Marshal(first)
		var g, w any
		_ = json.Unmarshal(got, &g)
		_ = json.Unmarshal(want, &w)
		if gb, _ := json.Marshal(g); string(gb) != func() string { wb, _ := json.Marshal(w); return string(wb) }() {
			t.Fatalf("assistant content not echoed verbatim:\n got %s\nwant %s", got, want)
		}
		return claudeTextResponse("ok", "end_turn")
	})
	if _, err := svc.Ask("q", ShopContext{}, testTools(t, &gotArgs)); err != nil {
		t.Fatal(err)
	}
	if gotArgs["days"] != float64(3) {
		t.Fatalf("tool args = %+v", gotArgs)
	}
}

// A failed tool goes back flagged is_error so the model can tell a failure
// from data.
func TestClaudeAskToolErrorFlagged(t *testing.T) {
	var gotArgs map[string]any
	call := 0
	svc := claudeAskService(t, func(t *testing.T, req plugin.HTTPRequest) plugin.HTTPResponse {
		call++
		if call == 1 {
			return claudeToolUseResponse("t1", "nope", map[string]any{})
		}
		r := decodeClaudeAsk(t, req)
		res := r.Messages[2]["content"].([]any)[0].(map[string]any)
		if res["is_error"] != true {
			t.Fatalf("tool error not flagged: %+v", res)
		}
		return claudeTextResponse("ok", "end_turn")
	})
	if _, err := svc.Ask("q", ShopContext{}, testTools(t, &gotArgs)); err != nil {
		t.Fatal(err)
	}
}

// A tool with no parameter schema still sends a valid input_schema: the
// Messages API rejects a tool without one.
func TestClaudeToolDefsDefaultSchema(t *testing.T) {
	defs, _ := claudeToolDefs([]AskTool{{Name: "x", Description: "d"}})
	if schema, _ := defs[0]["input_schema"].(map[string]any); schema["type"] != "object" {
		t.Fatalf("input_schema = %+v", defs[0]["input_schema"])
	}
}
