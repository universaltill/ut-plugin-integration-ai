# Review — Claude Ask loop + openai_compatible provider (ut-docs#2835)

**Date:** 2026-10-11 · **Plugin:** 2.2.0 → 2.3.0 · **Author:** Opus 5.5
(lane:sdk) · **Reviewer:** Fable (independent subagent, read-only)

## What shipped

- `src/ai/claude_ask.go`: `claudeProvider` implements `asker`. It runs the
  Messages API tool loop: tools become `{name, description, input_schema}`
  (nil schema → `{type: object}`). The assistant turn is echoed back as the
  raw JSON received. All `tool_result`s go in ONE user turn, failures
  flagged `is_error`. `end_turn` → the text blocks joined; `refusal` /
  `max_tokens` → an error. The loop is bounded by `maxToolRounds` +
  `pastDeadline`. A 401/403 body is never quoted. The model is
  `vision_model` (never `ask_model`). Ask your till now shows for `claude`.
- `src/ai/compat.go`: `provider = openai_compatible` wraps
  `openaiProvider`. The base URL is `endpoint`, with `/v1` added when the
  URL has no path. Authorization is sent only when `api_key` is set. It
  sends no reference images, and error strings name `openai_compatible`.
- `src/app/config.go`: only the exact `openai_compatible` selects it.
  Models default like self-hosted; no endpoint means not configured. No
  new setting, permission or ADR (ADR-0126).
- README, CLAUDE.md, manifest description; ut-docs `architecture/ai-plugin.md`.
- Same change set: the core `plugins.settings.ai.hosted_provider_notice`
  (en/ar/fa/tr) and the `web/help/*/plugins.md` AI paragraph
  (en/ar/fa/tr/de); the de/es/pt pack notices.

## Findings

| # | Sev | Finding | Outcome |
|---|---|---|---|
| 1 | major | The echoed assistant turn was re-serialised through a struct, so `thinking`/`redacted_thinking` blocks lost `signature`/`data`. That means a 400 on round 2 for every thinking-by-default Claude model. | **Fixed**: `content` is decoded as `[]json.RawMessage` and echoed unchanged. New test `TestClaudeAskEchoesAssistantContentVerbatim` failed first (it showed the stripped blocks), then passed. |
| 2 | minor | `max_tokens` with partial text was returned as the answer; 2048 was tight with thinking. | **Fixed**: `max_tokens` → error "cut off"; cap raised to 8192. Test case updated (failed first). |
| 3 | minor | Tool errors went back without `is_error`. | **Fixed**: `TestClaudeAskToolErrorFlagged` (failed first). |
| 4 | nit | `compatBaseURL` appended `/v1` after a query string. | **Fixed**: the URL is rebuilt with `url.URL`; test case added (failed first). |
| 5 | nit | A CLAUDE.md line was over width. | **Fixed.** |

The reviewer also checked and found correct: fail-safe resolution (near
misses stay self-hosted, key dropped) and key scoping (Authorization only
to the endpoint host; the till's egress grant is per host and re-checked
on redirect). It confirmed no stale "claude can't Ask" claims remain, and
that the notice/help translations keep the English meaning, with product
names untouched.

## Verified beyond unit tests

- Mutation checks run by the author: forcing the Authorization header for
  compat fails `TestCompatIdentifyNoKeyNoAuthHeaderNoRefs`; not echoing
  the assistant turn fails `TestClaudeAskToolLoop`.
- Full gate: gofmt, vet (native + wasip1), `go test ./...`, build,
  validate, guard-plugin-i18n, shellcheck, version-bump self-test.
- Core: `internal/pages` settings tests, guard-i18n, help-drift,
  help-topics, compliance-claims, competitor-naming. Packs:
  validate + check-key-drift against the branch's `en.json`.
- **Not verified live:** no driven run against a real model. The homelab
  OpenAI-compatible/Ollama hosts (192.168.1.118, .93) were unreachable
  this cycle, and an unattended run may not use an Anthropic key. Host-based
  egress means the `/v1` path needs no new grant (`wasm_egress.go`).
  Accepted gap; the wire shapes are pinned by tests.

## Verdict

Safe to merge.
