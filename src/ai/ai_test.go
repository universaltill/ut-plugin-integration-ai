//go:build !wasip1

package ai

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/universaltill/universal-till/sdk/plugin"
)

// Ported from universal-till internal/ai/ai_test.go. FromEnv is not ported:
// the plugin has no environment — its settings are the only configuration
// (src/app/config.go, ported from internal/pages/ai_resolve.go).

func TestNewDisabledWithoutBackend(t *testing.T) {
	if New(Config{}).Enabled() {
		t.Fatal("no backend must be disabled")
	}
	if New(Config{Provider: "ollama"}).Enabled() {
		t.Fatal("ollama without endpoint must be disabled")
	}
	if New(Config{Provider: "claude"}).Enabled() {
		t.Fatal("claude without key must be disabled")
	}
	if New(Config{Provider: "openai"}).Enabled() {
		t.Fatal("openai without key must be disabled")
	}
	var nilSvc *Service
	if nilSvc.Enabled() {
		t.Fatal("nil service must be disabled")
	}
	if !New(Config{Provider: "ollama", Endpoint: "http://x", Model: "m"}).Enabled() {
		t.Fatal("ollama with endpoint must be enabled")
	}
}

// Unlike claude, openai implements the ask loop — New() must wire it up so
// CanAsk reports true, and default the vision/ask models to
// DefaultOpenAIModel when the caller leaves them empty.
func TestNewOpenAIWithKeyEnabledAndCanAsk(t *testing.T) {
	svc := New(Config{Provider: "openai", APIKey: "k"})
	if !svc.Enabled() {
		t.Fatal("openai with a key must be enabled")
	}
	if !svc.CanAsk() {
		t.Fatal("openai implements the ask loop — CanAsk must be true")
	}
}

// Ollama never gets reference images (small open vision models lose
// accuracy with dozens of images); the hosted providers do. The plugin
// skips item_image_open entirely when they would be thrown away.
func TestUsesReferenceImages(t *testing.T) {
	if New(Config{Provider: "ollama", Endpoint: "http://x"}).UsesReferenceImages() {
		t.Error("ollama must not ask for reference images")
	}
	if !New(Config{Provider: "claude", APIKey: "k"}).UsesReferenceImages() {
		t.Error("claude sends reference images")
	}
	if !New(Config{Provider: "openai", APIKey: "k"}).UsesReferenceImages() {
		t.Error("openai sends reference images")
	}
	var nilSvc *Service
	if nilSvc.UsesReferenceImages() {
		t.Error("nil service must not ask for reference images")
	}
}

func TestOllamaIdentifyAndHallucinationFilter(t *testing.T) {
	useServer(t, func(t *testing.T, req plugin.HTTPRequest) plugin.HTTPResponse {
		if req.URL != "http://ollama.lan:11434/api/chat" {
			t.Errorf("url = %s", req.URL)
		}
		if req.Method != "POST" {
			t.Errorf("method = %s", req.Method)
		}
		body := decodeBody(t, req)
		if body["model"] != "test-model" {
			t.Errorf("model = %v", body["model"])
		}
		if body["stream"] != false || body["format"] == nil {
			t.Errorf("want non-streaming structured output, got stream=%v format=%v", body["stream"], body["format"])
		}
		msgs := body["messages"].([]any)
		msg := msgs[0].(map[string]any)
		if !strings.Contains(msg["content"].(string), "itm001") || !strings.HasSuffix(msg["content"].(string), "Identify the product in the attached photo.") {
			t.Errorf("prompt = %q", msg["content"])
		}
		if imgs := msg["images"].([]any); len(imgs) != 1 || imgs[0] != "AQ==" {
			t.Errorf("photo not sent base64: %v", msg["images"])
		}
		content, _ := json.Marshal(IdentifyResult{Matches: []Candidate{
			{ItemID: "itm001", Confidence: "high"},
			{ItemID: "made-up", Confidence: "low"},
		}})
		return jsonResponse(200, map[string]any{
			"message": map[string]any{"role": "assistant", "content": string(content)},
		})
	})

	// Trailing slash trimmed, as in core.
	svc := New(Config{Provider: "ollama", Endpoint: "http://ollama.lan:11434/", Model: "test-model"})
	res, err := svc.Identify([]byte{1}, "image/jpeg",
		[]CatalogItem{{ID: "itm001", SKU: "S1", Name: "Milk"}},
		[]RefImage{{ItemID: "itm001", MediaType: "image/jpeg", Data: []byte{2}}})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Matches) != 1 || res.Matches[0].ItemID != "itm001" {
		t.Fatalf("hallucinated id must be filtered; got %+v", res.Matches)
	}
}

// Identify's own guards, and every ollama identify failure mode, must come
// back as plain errors the plugin treats as "identification unavailable" —
// never a panic, never a sale blocker.
func TestIdentifyGuards(t *testing.T) {
	var disabled *Service
	if _, err := disabled.Identify([]byte{1}, "image/jpeg", nil, nil); err == nil {
		t.Fatal("nil service must error")
	}
	if _, err := New(Config{}).Identify([]byte{1}, "image/jpeg", nil, nil); err == nil {
		t.Fatal("disabled service must error")
	}
	svc := New(Config{Provider: "ollama", Endpoint: "http://localhost:1", Model: "m"})
	if _, err := svc.Identify(nil, "image/jpeg", nil, nil); err == nil {
		t.Fatal("empty photo must error")
	}
}

func TestOllamaIdentifyErrorPaths(t *testing.T) {
	identify := func(t *testing.T, resp plugin.HTTPResponse) error {
		t.Helper()
		useServer(t, func(*testing.T, plugin.HTTPRequest) plugin.HTTPResponse { return resp })
		svc := New(Config{Provider: "ollama", Endpoint: "http://localhost:11434", Model: "m"})
		_, err := svc.Identify([]byte{1}, "image/jpeg", nil, nil)
		return err
	}

	err := identify(t, rawResponse(500, "model not found"))
	if err == nil || !strings.Contains(err.Error(), "model not found") {
		t.Fatalf("http error must include the server's message, got %v", err)
	}

	err = identify(t, rawResponse(200, "not json"))
	if err == nil || !strings.Contains(err.Error(), "parse ollama response") {
		t.Fatalf("want envelope parse error, got %v", err)
	}

	err = identify(t, rawResponse(200, `{"message":{"role":"assistant","content":""}}`))
	if err == nil || !strings.Contains(err.Error(), "empty model response") {
		t.Fatalf("want empty-response error, got %v", err)
	}

	err = identify(t, rawResponse(200, `{"message":{"role":"assistant","content":"not json"}}`))
	if err == nil || !strings.Contains(err.Error(), "parse model response") {
		t.Fatalf("want inner parse error, got %v", err)
	}

	// Unreachable endpoint → transport error surfaces as an error too.
	useDownServer(t)
	svc := New(Config{Provider: "ollama", Endpoint: "http://127.0.0.1:1", Model: "m"})
	if _, err := svc.Identify([]byte{1}, "image/jpeg", nil, nil); err == nil {
		t.Fatal("unreachable endpoint must error")
	}
}

func TestOllamaIdentifyMalformedEndpoint(t *testing.T) {
	h := useServer(t, func(t *testing.T, _ plugin.HTTPRequest) plugin.HTTPResponse {
		t.Error("a malformed endpoint must never reach the host")
		return rawResponse(200, "{}")
	})
	svc := New(Config{Provider: "ollama", Endpoint: "http://bad url", Model: "m"})
	if _, err := svc.Identify([]byte{1}, "image/jpeg", nil, nil); err == nil {
		t.Fatal("malformed endpoint must error")
	}
	if h.Calls["http_request"] != 0 {
		t.Fatalf("http_request called %d times", h.Calls["http_request"])
	}
}

// The response cap: the host truncates a body past 256 KiB, so a truncated
// envelope is a parse error, never a partial answer read as whole.
func TestOllamaTruncatedBodyIsParseError(t *testing.T) {
	useServer(t, func(*testing.T, plugin.HTTPRequest) plugin.HTTPResponse {
		return rawResponse(200, `{"message":{"role":"assistant","content":"{\"matches\":[`)
	})
	svc := New(Config{Provider: "ollama", Endpoint: "http://localhost:11434", Model: "m"})
	if _, err := svc.Identify([]byte{1}, "image/jpeg", nil, nil); err == nil {
		t.Fatal("truncated body must error")
	}
}

func TestIdentifyPromptCarriesCatalog(t *testing.T) {
	p, err := identifyPrompt([]CatalogItem{{ID: "itm1", SKU: "S", Name: "Tea"}})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"You identify retail products for a point-of-sale till. ",
		"at most 3 matches. ",
		"Only return item_id values that exist in the catalog. ",
		"\n\nCatalog:\n" + `[{"id":"itm1","sku":"S","name":"Tea"}]`,
	} {
		if !strings.Contains(p, want) {
			t.Errorf("prompt lost %q:\n%s", want, p)
		}
	}
}
