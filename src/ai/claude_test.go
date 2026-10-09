//go:build !wasip1

package ai

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/universaltill/universal-till/sdk/plugin"
)

// Ported from universal-till internal/ai/claude_test.go. Core calls the
// Messages API through anthropic-sdk-go; the plugin speaks the same wire
// format over the till's http_request, so these tests also pin the wire
// shape the SDK used to produce (headers, system block, image blocks,
// cache_control, output_config.format).

func testClaudeProvider(t *testing.T, handler func(t *testing.T, req plugin.HTTPRequest) plugin.HTTPResponse) *claudeProvider {
	t.Helper()
	useServer(t, handler)
	return newClaudeProvider("test-key", "test-model")
}

// requestBlock digs the first element out of a captured request-body list
// field, failing with a message (not a panic) if the wire shape changed.
func requestBlock(t *testing.T, body map[string]any, field string) map[string]any {
	t.Helper()
	list, ok := body[field].([]any)
	if !ok || len(list) == 0 {
		t.Fatalf("request %s missing or empty: %+v", field, body[field])
	}
	m, ok := list[0].(map[string]any)
	if !ok {
		t.Fatalf("request %s[0] is not an object: %+v", field, list[0])
	}
	return m
}

func claudeTextResponse(text, stopReason string) plugin.HTTPResponse {
	return jsonResponse(200, map[string]any{
		"id": "msg_test", "type": "message", "role": "assistant", "model": "test-model",
		"content":     []map[string]any{{"type": "text", "text": text}},
		"stop_reason": stopReason,
		"usage":       map[string]any{"input_tokens": 1, "output_tokens": 1},
	})
}

// With reference images, the prompt-cache breakpoint sits on the LAST
// reference image so repeated identifications reuse the catalog+refs prefix.
func TestClaudeIdentifyWithRefs(t *testing.T) {
	var gotBody map[string]any
	var gotReq plugin.HTTPRequest
	p := testClaudeProvider(t, func(t *testing.T, req plugin.HTTPRequest) plugin.HTTPResponse {
		gotReq = req
		gotBody = decodeBody(t, req)
		content, _ := json.Marshal(IdentifyResult{Matches: []Candidate{{ItemID: "itm001", Confidence: "high"}}})
		return claudeTextResponse(string(content), "end_turn")
	})

	res, err := p.identify([]byte{9}, "image/jpeg",
		[]CatalogItem{{ID: "itm001", SKU: "S1", Name: "Milk"}},
		[]RefImage{{ItemID: "itm001", MediaType: "image/png", Data: []byte{1}}})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Matches) != 1 || res.Matches[0].ItemID != "itm001" {
		t.Fatalf("result = %+v", res)
	}

	if gotReq.URL != "https://api.anthropic.com/v1/messages" || gotReq.Method != "POST" {
		t.Fatalf("request = %s %s", gotReq.Method, gotReq.URL)
	}
	if gotReq.Headers["x-api-key"] != "test-key" || gotReq.Headers["anthropic-version"] != "2023-06-01" || gotReq.Headers["Content-Type"] != "application/json" {
		t.Fatalf("headers = %v", gotReq.Headers)
	}
	if gotBody["model"] != "test-model" || gotBody["max_tokens"] != float64(1024) {
		t.Fatalf("model/max_tokens = %v/%v", gotBody["model"], gotBody["max_tokens"])
	}

	system := requestBlock(t, gotBody, "system")
	if system["type"] != "text" || !strings.Contains(system["text"].(string), "itm001") {
		t.Fatalf("catalog not in system prompt: %v", system)
	}
	if _, cached := system["cache_control"]; cached {
		t.Fatal("with refs, the cache breakpoint belongs on the last ref image, not the system block")
	}
	msg := requestBlock(t, gotBody, "messages")
	if msg["role"] != "user" {
		t.Fatalf("message role = %v", msg["role"])
	}
	blocks, ok := msg["content"].([]any)
	if !ok {
		t.Fatalf("message content missing: %+v", msg)
	}
	// ref label, ref image (cache breakpoint), photo label, photo image
	if len(blocks) != 4 {
		t.Fatalf("content blocks = %d, want 4: %+v", len(blocks), blocks)
	}
	label := blocks[0].(map[string]any)
	if label["type"] != "text" || label["text"] != "Reference image for item itm001:" {
		t.Fatalf("ref label = %+v", label)
	}
	refImg := blocks[1].(map[string]any)
	if refImg["type"] != "image" || refImg["cache_control"] == nil {
		t.Fatalf("last ref image must carry the cache breakpoint: %+v", refImg)
	}
	if cc := refImg["cache_control"].(map[string]any); cc["type"] != "ephemeral" {
		t.Fatalf("cache_control = %+v", cc)
	}
	src := refImg["source"].(map[string]any)
	if src["type"] != "base64" || src["media_type"] != "image/png" || src["data"] != "AQ==" {
		t.Fatalf("ref image source = %+v", src)
	}
	if blocks[2].(map[string]any)["text"] != "Photo taken at the till — identify this product:" {
		t.Fatalf("photo label = %+v", blocks[2])
	}
	photo := blocks[3].(map[string]any)["source"].(map[string]any)
	if photo["media_type"] != "image/jpeg" || photo["data"] != "CQ==" {
		t.Fatalf("photo source = %+v", photo)
	}
	oc, ok := gotBody["output_config"].(map[string]any)
	if !ok {
		t.Fatal("structured output format not requested")
	}
	format := oc["format"].(map[string]any)
	if format["type"] != "json_schema" || format["schema"] == nil {
		t.Fatalf("output_config.format = %+v", format)
	}
}

// Without references the breakpoint moves to the system block (still caching
// the catalog context).
func TestClaudeIdentifyNoRefsCachesSystem(t *testing.T) {
	var gotBody map[string]any
	p := testClaudeProvider(t, func(t *testing.T, req plugin.HTTPRequest) plugin.HTTPResponse {
		gotBody = decodeBody(t, req)
		return claudeTextResponse(`{"matches":[],"suggested_name":"Oat Milk"}`, "end_turn")
	})
	res, err := p.identify([]byte{9}, "image/jpeg", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.SuggestedName != "Oat Milk" || len(res.Matches) != 0 {
		t.Fatalf("result = %+v", res)
	}
	system := requestBlock(t, gotBody, "system")
	if system["cache_control"] == nil {
		t.Fatal("without refs, the system block must carry the cache breakpoint")
	}
}

func TestClaudeIdentifyErrorPaths(t *testing.T) {
	t.Run("refusal", func(t *testing.T) {
		p := testClaudeProvider(t, func(*testing.T, plugin.HTTPRequest) plugin.HTTPResponse {
			return claudeTextResponse("", "refusal")
		})
		if _, err := p.identify([]byte{9}, "image/jpeg", nil, nil); err == nil || !strings.Contains(err.Error(), "declined") {
			t.Fatalf("refusal must surface as declined, got %v", err)
		}
	})
	t.Run("no text block", func(t *testing.T) {
		p := testClaudeProvider(t, func(*testing.T, plugin.HTTPRequest) plugin.HTTPResponse {
			return jsonResponse(200, map[string]any{
				"id": "msg_test", "type": "message", "role": "assistant", "model": "test-model",
				"content": []map[string]any{}, "stop_reason": "end_turn",
				"usage": map[string]any{"input_tokens": 1, "output_tokens": 1},
			})
		})
		if _, err := p.identify([]byte{9}, "image/jpeg", nil, nil); err == nil || !strings.Contains(err.Error(), "empty model response") {
			t.Fatalf("want empty-response error, got %v", err)
		}
	})
	t.Run("unparseable answer", func(t *testing.T) {
		p := testClaudeProvider(t, func(*testing.T, plugin.HTTPRequest) plugin.HTTPResponse {
			return claudeTextResponse("not json", "end_turn")
		})
		if _, err := p.identify([]byte{9}, "image/jpeg", nil, nil); err == nil || !strings.Contains(err.Error(), "parse model response") {
			t.Fatalf("want parse error, got %v", err)
		}
	})
	t.Run("unparseable envelope", func(t *testing.T) {
		p := testClaudeProvider(t, func(*testing.T, plugin.HTTPRequest) plugin.HTTPResponse {
			return rawResponse(200, "<html>")
		})
		if _, err := p.identify([]byte{9}, "image/jpeg", nil, nil); err == nil || !strings.Contains(err.Error(), "parse claude response") {
			t.Fatalf("want envelope parse error, got %v", err)
		}
	})
	t.Run("api error", func(t *testing.T) {
		p := testClaudeProvider(t, func(*testing.T, plugin.HTTPRequest) plugin.HTTPResponse {
			return rawResponse(400, `{"type":"error","error":{"type":"invalid_request_error","message":"bad request"}}`)
		})
		if _, err := p.identify([]byte{9}, "image/jpeg", nil, nil); err == nil || !strings.Contains(err.Error(), "bad request") {
			t.Fatalf("api error must propagate, got %v", err)
		}
	})
	t.Run("rejected key never echoed", func(t *testing.T) {
		p := testClaudeProvider(t, func(*testing.T, plugin.HTTPRequest) plugin.HTTPResponse {
			return rawResponse(401, `{"type":"error","error":{"type":"authentication_error","message":"invalid x-api-key test-key"}}`)
		})
		_, err := p.identify([]byte{9}, "image/jpeg", nil, nil)
		if err == nil || strings.Contains(err.Error(), "test-key") || !strings.Contains(err.Error(), "authentication failed") {
			t.Fatalf("auth failure must not echo the body, got %v", err)
		}
	})
	t.Run("unreachable", func(t *testing.T) {
		useDownServer(t)
		p := newClaudeProvider("k", "m")
		if _, err := p.identify([]byte{9}, "image/jpeg", nil, nil); err == nil {
			t.Fatal("transport error must propagate")
		}
	})
}
