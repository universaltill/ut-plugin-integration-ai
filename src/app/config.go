package app

import (
	"strings"

	"github.com/universaltill/universal-till/sdk/plugin"
	"github.com/universaltill/ut-plugin-integration-ai/src/ai"
)

// setting reads one of the plugin's own settings, trimmed, per event (no
// caching: a settings change applies on the next event). An unset or
// unreadable setting is "" — the fail-safe direction below.
func setting(key string) string {
	v, err := plugin.SettingsGet(key)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(v)
}

// ResolveConfig builds the backend config from the plugin's settings —
// ported from universal-till internal/pages/ai_resolve.go aiPluginConfig
// (ADR-0085, ADR-0126). ok=false means "not usably configured":
// self-hosted with no endpoint, or a hosted provider with no key.
func ResolveConfig() (ai.Config, bool) {
	provider := setting("provider")
	endpoint := setting("endpoint")
	visionModel := setting("vision_model")
	askModel := setting("ask_model")
	apiKey := setting("api_key")

	// ADR-0085 Decision 2, fail-safe direction (ADR-0126): ONLY the exact
	// values "claude" or "openai" select a hosted vendor. Unset,
	// "self_hosted", a typo, a different case, or any other value all take
	// the self-hosted branch below, so a misconfiguration can only ever
	// fall back to the shop's own hardware, never forward to a paid API
	// with whatever key happens to be stored.
	switch provider {
	case "claude":
		if apiKey == "" {
			// The shop chose a hosted vendor but hasn't entered its key:
			// "not configured" — never "use the leftover Ollama endpoint".
			return ai.Config{}, false
		}
		if visionModel == "" {
			visionModel = ai.DefaultClaudeModel
		}
		// vision_model doubles as the Claude model name. No AskModel: the
		// claude provider has no ask loop yet, so Ask your till hides
		// itself (Service.CanAsk).
		return ai.Config{Provider: "claude", APIKey: apiKey, Model: visionModel}, true
	case "openai":
		if apiKey == "" {
			return ai.Config{}, false
		}
		if visionModel == "" {
			visionModel = ai.DefaultOpenAIModel
		}
		if askModel == "" {
			askModel = ai.DefaultOpenAIModel
		}
		return ai.Config{Provider: "openai", APIKey: apiKey, Model: visionModel, AskModel: askModel}, true
	}
	cfg := ai.Config{Provider: "ollama", Endpoint: endpoint, Model: visionModel, AskModel: askModel}
	if cfg.Model == "" {
		cfg.Model = ai.DefaultOllamaVisionModel
	}
	if cfg.AskModel == "" {
		cfg.AskModel = ai.DefaultOllamaAskModel
	}
	return cfg, cfg.Endpoint != ""
}

// service is the per-event AI service: nil when not configured.
func service() *ai.Service {
	cfg, ok := ResolveConfig()
	if !ok {
		return nil
	}
	return ai.New(cfg)
}
