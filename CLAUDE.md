# ut-plugin-integration-ai — rules for working in this repo

WASM plugin (ADR-0009 one-repo-per-plugin, ADR-0001, ADR-0121) written in
Go on the official guest SDK `github.com/universaltill/universal-till/sdk/plugin`
— never hand-rolled `//go:wasmimport` lines. Since 2.0.0 (ut-docs#4032) it
carries the AI engine itself: `src/ai` (Ollama / OpenAI / Claude providers,
identify prompt, Ask tool loop — ported from the till's `internal/ai`),
`src/app` (the `catalog.identify` job, the `reports.panels` Ask panel, its
action and `<id>.ask` job, settings resolution), `src/main.go`
(`plugin.Run`). Docs repo: `architecture/ai-plugin.md`. The till's own
engine stays until ut-docs#2851 deletes it — keep prompts, schemas, limits
and per-provider behaviour identical to it until then, and say so in a
comment wherever the plugin must differ.

- **Settings are a contract with installed shops.** The id
  `com.universaltill.integration-ai` and the keys `provider`, `endpoint`
  (`type: "endpoint"`), `vision_model`, `ask_model`, `api_key`
  (`type: "secret"`, ADR-0082) and their defaults never change
  (`scripts/validate.sh` enforces it). Read them with `plugin.SettingsGet`
  per event, never cached.
- **Self-hosted stays the DEFAULT** (ADR-0126, superseding ADR-0085): a
  shop may pick ANY provider with its OWN key; today `provider = claude`
  or `provider = openai`. Only those exact values select a hosted vendor —
  anything else is self-hosted (fail-safe), a hosted provider without a
  key is "not configured", never Ollama. A hosted provider is never the
  default, never on a Universal Till account. `provider` stays a
  plain-text setting — no new manifest "select" field type. New vendors
  arrive as `src/ai` adapters reusing the same settings; a second key
  setting only with the per-capability adapter work ADR-0126 §6 requires.
- **Egress only through the till** (`plugin.HTTP`): `net:@setting:endpoint`
  + `http:lan` for the shop's own server, exact `net:` hosts for vendors.
  No `net:*`. A 401/403 body never reaches an error message (it can echo
  the key).
- **Core data only through core read views** (`plugin.ViewQuery`): every
  view in `views_used` with its `view:<class>` permission. Views are
  strict, so clamp tool arguments to their bounds first (core's `intArg`).
- **View documents** follow ut-docs `reference/plugin-views.md`: every
  TEXT is a key from `locales/*.json` or a literal ≤ 4 KiB; the identify
  seam allows only `text`, `notice`, `suggestions`. A new key goes into
  every `locales/*.json` (`scripts/guard-plugin-i18n.sh`).
- **Tests run natively on `plugin.FakeHost`** (`*_test.go` are
  `//go:build !wasip1`). TDD: failing test first.

## Before committing

`gofmt -l .` (no output), `go vet ./...`, `GOOS=wasip1 GOARCH=wasm go vet
./...`, `go test ./...`, `scripts/build.sh`, `scripts/validate.sh`,
`scripts/guard-plugin-i18n.sh`, `shellcheck scripts/*.sh`,
`scripts/check-version-bump.test.sh`. Feature branch; review record in
`docs/code-reviews/<date>-<topic>.md`; then merge.

## Releases

Release = tag `v<version>` matching `manifest.json`, cut by
`auto-tag-release.yml` on merge. **Any PR that changes what ships —
`manifest.json`, `README.md`, `LICENSE`, `locales/`, anything under `src/`,
`go.mod`/`go.sum` or `scripts/build.sh` (they produce `bin/plugin.wasm`) —
must bump `manifest.json`'s `version` in the same PR**, or it lands on
`main` and silently never ships (ut-docs#1940). Enforced by
`scripts/check-version-bump.sh` (CI job `version-bump`, PRs only); `docs/`,
`.github/`, other `scripts/` and `CLAUDE.md` are exempt.
