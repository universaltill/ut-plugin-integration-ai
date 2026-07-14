# ut-plugin-integration-ai — rules for working in this repo

Config-only plugin (ADR-0009 one-repo-per-plugin): `runtime: "none"`,
no code. Installing it is how a shop opts into the till's AI features;
its settings (endpoint/vision_model/ask_model) configure the HOST's
`internal/ai` engine — see docs repo `architecture/ai-plugin.md`.
Self-hosted only ([[ai-self-hosted-only]]): never add a paid-API key
setting here. Release = tag v<version> matching manifest.json.
