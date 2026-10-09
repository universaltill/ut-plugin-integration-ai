// Package ai is the till's AI engine, ported from universal-till
// internal/ai (ut-docs#4032, ADR-0121): camera identify against a pluggable
// model backend, and the "Ask your till" tool-use loop.
//
// The default backend is SELF-HOSTED (an Ollama server running an open
// vision model on the shop's own hardware/homelab). A shop may pick any
// other provider with its own key (ADR-0126, superseding ADR-0085); today
// the Claude and OpenAI APIs are implemented, and more arrive as adapters.
// Never a Universal Till AI account.
//
// Every request goes out through the till's http_request host function
// (plugin.HTTP), so the till's egress policy decides where the plugin may
// connect: the shop's own endpoint (net:@setting:endpoint, with http:lan
// for a LAN server), api.openai.com and api.anthropic.com. Offline-first is
// binding (ADR-0003): nothing here sits on the checkout path; callers treat
// errors as "feature unavailable", never as a sale blocker.
//
// Prompts, schemas, limits and per-provider behaviour match core's engine
// word for word; where the plugin differs it says so next to the code.
package ai

import (
	"encoding/json"
	"fmt"
	"time"
)

// Config is the resolved backend configuration (src/app's settings
// resolution builds it). With neither an endpoint nor a key the feature set
// is unavailable.
type Config struct {
	Provider string // "ollama" (self-hosted, default), "claude", or "openai" (both optional)
	Endpoint string // Ollama base URL, e.g. http://localhost:11434
	Model    string
	AskModel string // tool-capable model for "Ask your till" (ollama, openai)
	APIKey   string // claude/openai provider only
}

// DefaultClaudeModel is the model the claude provider runs when none is
// configured (core: internal/ai.DefaultClaudeModel).
const DefaultClaudeModel = "claude-haiku-4-5"

// DefaultOpenAIModel is the model the openai provider runs for BOTH vision
// (camera identify) and ask (tool-calling) when the corresponding setting is
// left empty (core: internal/ai.DefaultOpenAIModel).
const DefaultOpenAIModel = "gpt-4o-mini"

// Ollama's defaults when vision_model / ask_model are blank (core:
// internal/ai.FromEnv and internal/pages/ai_resolve.go).
const (
	DefaultOllamaVisionModel = "llama3.2-vision"
	DefaultOllamaAskModel    = "llama3.2"
)

// CatalogItem is the context the model sees for each active product. Only
// item identity leaves the till — no sales figures, no customer data (and
// with the self-hosted provider nothing leaves the shop at all).
type CatalogItem struct {
	ID   string `json:"id"`
	SKU  string `json:"sku"`
	Name string `json:"name"`
}

// RefImage is a reference photo for one item (item_image_open role "ref":
// the item's newest cashier-confirmed photo, else its catalog thumbnail).
type RefImage struct {
	ItemID    string
	MediaType string // image/png or image/jpeg
	Data      []byte
}

// Candidate is one proposed match, best first.
type Candidate struct {
	ItemID     string `json:"item_id"`
	Confidence string `json:"confidence"` // high | medium | low
}

// IdentifyResult is the model's structured answer. SuggestedName is filled
// when the product looks like it isn't in the catalog at all ("ask and add").
type IdentifyResult struct {
	Matches       []Candidate `json:"matches"`
	SuggestedName string      `json:"suggested_name"`
}

// provider is one model backend. Implementations return an error rather
// than block the caller; the job deadline (manifest limits.long_call_s)
// bounds an in-flight request.
type provider interface {
	identify(photo []byte, photoMediaType string, items []CatalogItem, refs []RefImage) (*IdentifyResult, error)
}

// refUser marks a provider that sends reference images (claude, openai).
// Ollama deliberately does not (see ollamaProvider), so the plugin skips
// item_image_open entirely for it.
type refUser interface{ usesRefs() }

// Service is the AI facade. A nil or disabled Service is safe to call
// Enabled() on.
type Service struct {
	p       provider
	enabled bool
}

// New builds the Service for cfg; a provider without its endpoint or key is
// disabled, never silently another provider.
func New(cfg Config) *Service {
	switch cfg.Provider {
	case "ollama":
		if cfg.Endpoint == "" {
			return &Service{}
		}
		return &Service{p: newOllamaProvider(cfg.Endpoint, cfg.Model, cfg.AskModel), enabled: true}
	case "claude":
		if cfg.APIKey == "" {
			return &Service{}
		}
		return &Service{p: newClaudeProvider(cfg.APIKey, cfg.Model), enabled: true}
	case "openai":
		if cfg.APIKey == "" {
			return &Service{}
		}
		visionModel := cfg.Model
		if visionModel == "" {
			visionModel = DefaultOpenAIModel
		}
		askModel := cfg.AskModel
		if askModel == "" {
			askModel = DefaultOpenAIModel
		}
		return &Service{p: newOpenAIProvider(cfg.APIKey, visionModel, askModel), enabled: true}
	default:
		return &Service{}
	}
}

// Enabled reports whether a backend is configured.
func (s *Service) Enabled() bool { return s != nil && s.enabled }

// UsesReferenceImages reports whether Identify sends reference photos, so
// the caller loads them only when they are used.
func (s *Service) UsesReferenceImages() bool {
	if !s.Enabled() {
		return false
	}
	_, ok := s.p.(refUser)
	return ok
}

// Core bounded identify at 90 s with a context deadline. The plugin cannot
// cancel an in-flight http_request, and the till's plugin HTTP client gives
// up on a response whose headers take over 30 s (ut-docs#4035) — so today a
// self-hosted vision call slower than 30 s fails where core waited 90 s.
// The job deadline (limits.long_call_s, 120 s) bounds the whole job.

// now is the clock, a var so tests can move it.
var now = time.Now

// identifySchema constrains the response so parsing can't fail on prose.
// Ollama ("format"), the Claude API (output_config.format) and OpenAI
// (response_format.json_schema) all accept it.
var identifySchema = map[string]any{
	"type": "object",
	"properties": map[string]any{
		"matches": map[string]any{
			"type": "array",
			"items": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"item_id":    map[string]any{"type": "string"},
					"confidence": map[string]any{"type": "string", "enum": []string{"high", "medium", "low"}},
				},
				"required":             []string{"item_id", "confidence"},
				"additionalProperties": false,
			},
		},
		"suggested_name": map[string]any{"type": "string"},
	},
	"required":             []string{"matches", "suggested_name"},
	"additionalProperties": false,
}

// identifyPrompt is the shared task description; the catalog rides along as
// JSON so the model can only pick real items.
func identifyPrompt(items []CatalogItem) (string, error) {
	catalogJSON, err := json.Marshal(items)
	if err != nil {
		return "", err
	}
	return "You identify retail products for a point-of-sale till. " +
		"The shop's catalog of active items follows as JSON (id, sku, name). " +
		"Given the photo taken at the till, return the catalog items most likely " +
		"to be the product in the photo, best match first, at most 3 matches. " +
		"Only return item_id values that exist in the catalog. If the product is clearly " +
		"not in the catalog, return no matches and suggest a short product name in suggested_name; " +
		"otherwise leave suggested_name empty.\n\nCatalog:\n" + string(catalogJSON), nil
}

// Identify asks the configured backend for the top matches for a till photo.
func (s *Service) Identify(photo []byte, photoMediaType string, items []CatalogItem, refs []RefImage) (*IdentifyResult, error) {
	if !s.Enabled() {
		return nil, fmt.Errorf("ai disabled")
	}
	if len(photo) == 0 {
		return nil, fmt.Errorf("photo required")
	}
	out, err := s.p.identify(photo, photoMediaType, items, refs)
	if err != nil {
		return nil, err
	}
	// The schema guarantees shape, not referential integrity — drop ids the
	// model hallucinated outside the catalog.
	valid := make(map[string]bool, len(items))
	for _, it := range items {
		valid[it.ID] = true
	}
	kept := out.Matches[:0]
	for _, m := range out.Matches {
		if valid[m.ItemID] {
			kept = append(kept, m)
		}
	}
	out.Matches = kept
	return out, nil
}
