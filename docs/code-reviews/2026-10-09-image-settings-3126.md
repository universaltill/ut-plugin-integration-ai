# Review: background-removal settings, 2.1.0 (ut-docs#3126)

- **Date:** 2026-10-09, re-reviewed 2026-10-10 after the rebase onto 2.0.x · **Card:** universaltill/ut-docs#3126
- **Host side:** merged as universal-till#1817. Its review record is `docs/code-reviews/2026-10-09-ai-image-capability-3126.md` there.

## What shipped

- **New settings**, all plain text:
  - `image_provider` (default `self_hosted`; validated host-side, fail-safe).
  - `image_endpoint` (default empty = off).
  - `image_model` (default `birefnet-general-lite`).
- **Why `image_endpoint` is not `type: "endpoint"`** (changed in the 2026-10-10 rebase):
  - Since 2.0.0 the plugin is a WASM module holding `http:lan`.
  - Core grants a plugin with `http:lan` LAN egress, as an exact grant, to the host of every `type: "endpoint"` setting it declares (`endpointSettingHostMatch` and `admitHTTPHop` in universal-till `internal/plugins`).
  - The till calls rembg, the plugin never does, so least privilege leaves the setting untyped.
  - Core validates the URL where it uses it (`validImageEndpoint` → `plugins.ValidEndpointURL`). An invalid value leaves the feature off.
- **Version:** 2.0.2 → 2.1.0. The PR was first cut as 1.2.0 → 1.3.0, before the WASM rewrite.
- **`scripts/validate.sh`:** pins the three keys and their defaults, and fails if any of them gains a `type`.
- **README:**
  - A "Background removal" section covering the rembg setup and the allowed models with their licences. BiRefNet is MIT and U²-Net is Apache-2.0, both checked upstream on 2026-10-09. rembg's default `bria-rmbg` needs a paid commercial licence, and the host refuses it.
  - A note that the picker button arrives with #3127.
  - A permissions note: background removal needs no plugin permission.
- **CLAUDE.md:** the settings-contract rule names the `image_*` keys.

## Review 1 (2026-10-09, Fable, independent)

- `ReconcilePluginSettings` adds the three keys to existing installs, with defaults that resolve to off.
- `provider` stays plain text.
- Safe to merge.

## Review 2 (2026-10-10, Fable, independent, after the rebase)

- **Egress reasoning confirmed** against core: `secret_settings.go` `endpointSettingHostMatch` and `wasm_egress.go` `admitHTTPHop`.
- **Should-fix, fixed:** the README described a picker button that only arrives with #3127.
- **Should-fix, deferred to a core follow-up card:** an invalid `image_endpoint` saves silently. The settings page only hints for `image_model`.
- **Nits, fixed:** the README intro ("both run against Ollama"), the settings-apply sentence, the CLAUDE.md contract, and the placement of the ut-docs `ai-plugin.md` paragraph.
- **Runs:** all passed — `gofmt`, `go vet` (native and wasip1), `go test`, `build.sh`, `validate.sh` (v2.1.0), `guard-plugin-i18n.sh`, `shellcheck`, `check-version-bump.test.sh`.

## Verified beyond the plugin's own tests

- A scratch test in universal-till (not committed) ran core's `plugins.ParseManifest` on this 2.1.0 manifest and accepted it.
- `aiPluginImageConfig` resolves the shipped defaults to "not configured" (env, else off). With an endpoint set, it resolves to `self_hosted` / `birefnet-general-lite`.
- `validate.sh` fails on main's 2.0.2 manifest (keys missing) and on a variant with `image_endpoint` typed `endpoint`. It passes on this manifest.

**Verdict:** safe to merge.
