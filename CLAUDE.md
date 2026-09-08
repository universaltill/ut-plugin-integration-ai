# ut-plugin-integration-ai — rules for working in this repo

Config-only plugin (ADR-0009 one-repo-per-plugin): `runtime: "none"`,
no code. Installing it is how a shop opts into the till's AI features;
its settings (provider/endpoint/vision_model/ask_model/api_key) configure
the HOST's `internal/ai` engine — see docs repo `architecture/ai-plugin.md`.
Self-hosted stays the DEFAULT ([[ai-self-hosted-only]], narrowed by
ADR-0085, extended to a second vendor by ut-docs#1791): a hosted provider
is a shop's explicit opt-in with its OWN key (`provider = claude` or
`provider = openai` + `api_key`, `type: "secret"` per ADR-0082) — never
the default, never on a Universal Till account, never a dependency of any
other feature. `provider` stays a plain-text setting validated host-side
(fail-safe: unrecognized falls back to self-hosted) — do not add a new
manifest "select" field type or a second `api_key` setting for this; a
future third vendor reuses the same two settings the same way. Release =
tag v<version> matching manifest.json.
