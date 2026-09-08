# ut-plugin-integration-ai — rules for working in this repo

Config-only plugin (ADR-0009 one-repo-per-plugin): `runtime: "none"`,
no code. Installing it is how a shop opts into the till's AI features;
its settings (provider/endpoint/vision_model/ask_model/api_key) configure
the HOST's `internal/ai` engine — see docs repo `architecture/ai-plugin.md`.
Self-hosted stays the DEFAULT ([[ai-self-hosted-only]], narrowed by
ADR-0085): a hosted provider is a shop's explicit opt-in with its OWN key
(`provider = claude` + `api_key`, `type: "secret"` per ADR-0082) — never
the default, never on a Universal Till account, never a dependency of any
other feature. Release = tag v<version> matching manifest.json.
