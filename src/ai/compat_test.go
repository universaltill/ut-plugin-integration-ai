//go:build !wasip1

package ai

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/universaltill/universal-till/sdk/plugin"
)

// ut-docs#2835: provider = openai_compatible speaks OpenAI's Chat
// Completions wire format to the shop's own server (LM Studio, vLLM,
// llama.cpp, Ollama's /v1) at the endpoint setting.

func compatConfig(endpoint, key string) Config {
	return Config{Provider: "openai_compatible", Endpoint: endpoint, APIKey: key, Model: "vis", AskModel: "txt"}
}

func TestCompatBaseURL(t *testing.T) {
	for in, want := range map[string]string{
		"http://192.168.1.50:1234":          "http://192.168.1.50:1234/v1",
		"http://192.168.1.50:1234/":         "http://192.168.1.50:1234/v1",
		"http://localhost:11434":            "http://localhost:11434/v1",
		"https://llm.example.com/v1":        "https://llm.example.com/v1",
		"https://llm.example.com/v1/":       "https://llm.example.com/v1",
		"https://gw.example.com/openai/v1/": "https://gw.example.com/openai/v1",
		"http://lm.local:1234?x=1":          "http://lm.local:1234/v1?x=1",
	} {
		if got := compatBaseURL(in); got != want {
			t.Errorf("compatBaseURL(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestNewCompat(t *testing.T) {
	if New(Config{Provider: "openai_compatible"}).Enabled() {
		t.Fatal("openai_compatible without an endpoint must be disabled")
	}
	svc := New(compatConfig("http://lm.local:1234", ""))
	if !svc.Enabled() || !svc.CanAsk() {
		t.Fatal("openai_compatible with an endpoint must be enabled and ask-capable, key or not")
	}
	if svc.UsesReferenceImages() {
		t.Fatal("small local models get no reference images (same as ollama)")
	}
}

func TestCompatIdentifyNoKeyNoAuthHeaderNoRefs(t *testing.T) {
	var gotReq plugin.HTTPRequest
	useServer(t, func(t *testing.T, req plugin.HTTPRequest) plugin.HTTPResponse {
		gotReq = req
		content, _ := json.Marshal(IdentifyResult{Matches: []Candidate{{ItemID: "itm001", Confidence: "high"}}})
		return openAIMessageResponse(map[string]any{"role": "assistant", "content": string(content)})
	})
	svc := New(compatConfig("http://lm.local:1234", ""))
	res, err := svc.Identify([]byte{9}, "image/jpeg", []CatalogItem{{ID: "itm001", Name: "Milk"}},
		[]RefImage{{ItemID: "itm001", MediaType: "image/png", Data: []byte{1}}})
	if err != nil || len(res.Matches) != 1 {
		t.Fatalf("res=%+v err=%v", res, err)
	}
	if gotReq.URL != "http://lm.local:1234/v1/chat/completions" {
		t.Fatalf("url = %s", gotReq.URL)
	}
	if _, ok := gotReq.Headers["Authorization"]; ok {
		t.Fatalf("no key → no Authorization header, got %v", gotReq.Headers)
	}
	body := decodeBody(t, gotReq)
	if body["model"] != "vis" {
		t.Fatalf("model = %v", body["model"])
	}
	if strings.Contains(string(gotReq.Body), "Reference image") {
		t.Fatal("reference images must not be sent to openai_compatible")
	}
}

// ADR-0126 §7: a key stored on the shop's own server setup goes to that
// endpoint and nowhere else — never to api.openai.com.
func TestCompatAskKeyGoesOnlyToEndpoint(t *testing.T) {
	var urls []string
	var auths []string
	call := 0
	useServer(t, func(t *testing.T, req plugin.HTTPRequest) plugin.HTTPResponse {
		call++
		urls = append(urls, req.URL)
		auths = append(auths, req.Headers["Authorization"])
		if body := decodeBody(t, req); body["model"] != "txt" {
			t.Errorf("ask model = %v", body["model"])
		}
		if call == 1 {
			return openAIMessageResponse(map[string]any{"role": "assistant", "tool_calls": []map[string]any{{
				"id": "c1", "type": "function", "function": map[string]any{"name": "sales_by_day", "arguments": "{}"},
			}}})
		}
		return openAIMessageResponse(map[string]any{"role": "assistant", "content": "fine"})
	})
	var gotArgs map[string]any
	svc := New(compatConfig("https://llm.example.com", "sk-stray"))
	answer, err := svc.Ask("q", ShopContext{}, testTools(t, &gotArgs))
	if err != nil || answer != "fine" {
		t.Fatalf("answer=%q err=%v", answer, err)
	}
	for i, u := range urls {
		if u != "https://llm.example.com/v1/chat/completions" {
			t.Fatalf("request %d went to %s", i, u)
		}
		if auths[i] != "Bearer sk-stray" {
			t.Fatalf("request %d auth = %q", i, auths[i])
		}
	}
}

func TestCompatErrorsNameTheProvider(t *testing.T) {
	useServer(t, func(*testing.T, plugin.HTTPRequest) plugin.HTTPResponse {
		return rawResponse(401, `{"error":"bad key sk-SECRET"}`)
	})
	var gotArgs map[string]any
	_, err := New(compatConfig("http://lm.local:1234", "sk-SECRET")).Ask("q", ShopContext{}, testTools(t, &gotArgs))
	if err == nil || !strings.HasPrefix(err.Error(), "openai_compatible 401") || strings.Contains(err.Error(), "SECRET") {
		t.Fatalf("err = %v", err)
	}
	useServer(t, func(*testing.T, plugin.HTTPRequest) plugin.HTTPResponse {
		return rawResponse(500, "model not loaded")
	})
	_, err = New(compatConfig("http://lm.local:1234", "")).Ask("q", ShopContext{}, testTools(t, &gotArgs))
	if err == nil || !strings.Contains(err.Error(), "openai_compatible 500") || !strings.Contains(err.Error(), "model not loaded") {
		t.Fatalf("err = %v", err)
	}
}
