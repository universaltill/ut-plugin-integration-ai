//go:build !wasip1

package app

import (
	"testing"

	"github.com/universaltill/ut-plugin-integration-ai/src/ai"
)

// Ported from universal-till internal/pages/ai_resolve_test.go. The plugin
// is installed and active by definition when it runs, so only the
// settings branch (aiPluginConfig) carries over; the UT_AI_* env fallback
// stays core's own developer override.

func TestResolve_UnsetOrSelfHostedProviderKeepsOllama(t *testing.T) {
	for _, provider := range []string{"", "self_hosted"} {
		settings := map[string]string{"endpoint": "http://ollama.local:11434"}
		if provider != "" {
			settings["provider"] = provider
		}
		newHost(t, settings)
		cfg, ok := ResolveConfig()
		want := ai.Config{Provider: "ollama", Endpoint: "http://ollama.local:11434", Model: "llama3.2-vision", AskModel: "llama3.2"}
		if !ok || cfg != want {
			t.Fatalf("provider %q: got %+v ok=%v, want %+v", provider, cfg, ok, want)
		}
	}
}

func TestResolve_SelfHostedModelsFromSettings(t *testing.T) {
	newHost(t, map[string]string{"endpoint": " http://ollama.local:11434 ", "vision_model": "llava", "ask_model": "qwen2.5"})
	cfg, ok := ResolveConfig()
	want := ai.Config{Provider: "ollama", Endpoint: "http://ollama.local:11434", Model: "llava", AskModel: "qwen2.5"}
	if !ok || cfg != want {
		t.Fatalf("got %+v ok=%v, want %+v", cfg, ok, want)
	}
}

func TestResolve_SelfHostedWithoutEndpointIsNotConfigured(t *testing.T) {
	newHost(t, map[string]string{"provider": "self_hosted", "endpoint": ""})
	if _, ok := ResolveConfig(); ok {
		t.Fatal("self-hosted without an endpoint must be not configured")
	}
	newHost(t, nil) // no endpoint row at all
	if _, ok := ResolveConfig(); ok {
		t.Fatal("no endpoint setting must be not configured")
	}
}

func TestResolve_ClaudeWithKeySelectsClaude(t *testing.T) {
	newHost(t, map[string]string{"provider": "claude", "api_key": "sk-ant-x", "endpoint": "http://ollama.local:11434", "ask_model": "llama3.2"})
	cfg, ok := ResolveConfig()
	want := ai.Config{Provider: "claude", APIKey: "sk-ant-x", Model: ai.DefaultClaudeModel}
	if !ok || cfg != want {
		t.Fatalf("got %+v ok=%v, want %+v", cfg, ok, want)
	}
	newHost(t, map[string]string{"provider": "claude", "api_key": "k", "vision_model": "claude-sonnet-4-5"})
	if cfg, _ := ResolveConfig(); cfg.Model != "claude-sonnet-4-5" {
		t.Fatalf("vision_model doubles as the Claude model, got %q", cfg.Model)
	}
}

func TestResolve_WhitespacePaddedClaudeStillSelectsHosted(t *testing.T) {
	newHost(t, map[string]string{"provider": "  claude  ", "api_key": " k "})
	cfg, ok := ResolveConfig()
	if !ok || cfg.Provider != "claude" || cfg.APIKey != "k" {
		t.Fatalf("got %+v ok=%v", cfg, ok)
	}
}

// The shop chose a hosted vendor but has not entered its key: "not
// configured" — never "use the leftover Ollama endpoint instead".
func TestResolve_HostedWithoutKeyIsNotConfiguredNotOllama(t *testing.T) {
	for _, provider := range []string{"claude", "openai"} {
		newHost(t, map[string]string{"provider": provider, "endpoint": "http://ollama.local:11434"})
		if cfg, ok := ResolveConfig(); ok {
			t.Fatalf("%s without a key resolved to %+v", provider, cfg)
		}
	}
}

// ADR-0085 Decision 2 fail-safe: only the exact values claude/openai select
// a hosted vendor; a typo, other case or other vendor name is self-hosted,
// never a paid API with whatever key happens to be stored.
func TestResolve_UnrecognizedProviderNeverSelectsHosted(t *testing.T) {
	for _, provider := range []string{"Claude", "CLAUDE", "anthropic", "OpenAI", "gpt", "claud", "self-hosted"} {
		newHost(t, map[string]string{"provider": provider, "api_key": "k", "endpoint": "http://ollama.local:11434"})
		cfg, ok := ResolveConfig()
		if !ok || cfg.Provider != "ollama" || cfg.APIKey != "" {
			t.Fatalf("provider %q resolved to %+v ok=%v, want self-hosted", provider, cfg, ok)
		}
	}
}

func TestResolve_OpenAIWithKeySelectsOpenAI(t *testing.T) {
	newHost(t, map[string]string{"provider": "openai", "api_key": "sk-x"})
	cfg, ok := ResolveConfig()
	want := ai.Config{Provider: "openai", APIKey: "sk-x", Model: ai.DefaultOpenAIModel, AskModel: ai.DefaultOpenAIModel}
	if !ok || cfg != want {
		t.Fatalf("got %+v ok=%v, want %+v", cfg, ok, want)
	}
	newHost(t, map[string]string{"provider": "openai", "api_key": "sk-x", "vision_model": "gpt-4o", "ask_model": "gpt-4.1"})
	cfg, _ = ResolveConfig()
	if cfg.Model != "gpt-4o" || cfg.AskModel != "gpt-4.1" {
		t.Fatalf("models not taken from settings: %+v", cfg)
	}
}

// A settings read the host refuses counts as unset: the fail-safe
// direction (self-hosted, or not configured), never a hosted provider.
func TestResolve_SettingsReadFailureIsUnset(t *testing.T) {
	h := newHost(t, map[string]string{"provider": "openai", "api_key": "k", "endpoint": "http://ollama.local:11434"})
	h.Deny["settings_get"] = true
	if cfg, ok := ResolveConfig(); ok {
		t.Fatalf("unreadable settings resolved to %+v", cfg)
	}
}
