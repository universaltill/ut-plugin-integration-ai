# ut-plugin-integration-ai — rules for working in this repo

Config-only plugin (ADR-0009 one-repo-per-plugin): `runtime: "none"`,
no code. Installing it is how a shop opts into the till's AI features;
its settings (provider/endpoint/vision_model/ask_model/api_key) configure
the HOST's `internal/ai` engine — see docs repo `architecture/ai-plugin.md`.
Self-hosted stays the DEFAULT (ADR-0126, which supersedes ADR-0085): a
shop may pick ANY provider with its OWN key — today the host implements
`provider = claude` or `provider = openai` + `api_key` (`type: "secret"`
per ADR-0082); more vendors arrive as host-side `internal/ai` adapters,
not as new settings here. A hosted provider is never the default, never
on a Universal Till account, never a dependency of any other feature.
`provider` stays a plain-text setting validated host-side (fail-safe:
unrecognized falls back to self-hosted) — do not add a new manifest
"select" field type for this; a future vendor reuses the same two
settings the same way. A second key setting arrives only together with the
host-side per-capability adapter work ADR-0126 §6 requires, never ad hoc
here. Release =
tag v<version> matching manifest.json.

**Any PR that touches a shipped file — `manifest.json`, `README.md`,
`LICENSE` — must bump `manifest.json`'s `version` in the same PR.**
`scripts/package.sh` bundles exactly those files into the release
artifact, and `auto-tag-release.yml` only cuts a release when the version
differs from the last tag; without a bump the change lands on `main` and
then silently never ships (ut-docs#1940). Enforced by
`scripts/check-version-bump.sh` (CI job `version-bump`, PRs only). A PR
that touches none of those three files is exempt (`docs/`, `.github/`,
`scripts/`, `CLAUDE.md`, …).
