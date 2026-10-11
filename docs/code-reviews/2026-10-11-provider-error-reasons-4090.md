# Review — tell the shop why a hosted AI provider failed (ut-docs#4090)

- **Date:** 2026-10-11
- **Version:** 2.3.0 → 2.3.1
- **Built by:** Claude Opus 5.5 (inline, lane:cloudsession)
- **Reviewed by:** Claude Fable 5.1 (independent subagent, own worktree)
- **Card size:** medium

## What shipped

- **`src/ai/errors.go`: `ProviderError{Kind}`.** The kinds are other,
  unreachable, key rejected, no credit and rate limited. `Error()` is the
  same log text as before.
  - openai, openai_compatible, claude and ollama all build their non-200
    errors through `statusError`. A 401/403 body never enters the error;
    Ollama follows that rule now too.
  - A transport error from `plugin.HTTP` is wrapped as unreachable.
  - "No credit" is OpenAI's structured `insufficient_quota` code or type,
    Anthropic's "credit balance is too low", or a 402. 429 and 529 mean
    rate limited.
- **`Service.Hosted()`** is true for claude and openai.
- **`src/app/errors.go` `failureKey`:** key rejected, no credit and rate
  limited get their own notice for every provider. Unreachable and other
  get one only for a hosted provider. Self-hosted keeps
  `integration_ai.ask.error` for Ask, and a failed job (core's
  "Identification failed") for identify, as before.
- **Ask** answers the notice and logs `ai ask failed (<kind>): …`.
  **Identify** answers a document holding the error notice instead of
  failing the job.
- **Five keys** `integration_ai.error.*`, in en, tr, fa and ar.
- **Core counterpart** (universal-till): the identify overlay no longer adds
  "No match found" under an error notice, and the manual paragraph is
  updated.

## Findings

| # | Severity | Finding | Outcome |
|---|---|---|---|
| 1 | major | A loose `"billing"` marker classified OpenAI's ordinary 429 `rate_limit_exceeded` body as "no credit", because that body links the billing page to raise limits. The shop would be told to top up instead of waiting a minute. | **Fixed.** Classification now uses the structured `insufficient_quota` code/type and Anthropic's exact phrase. The real rate-limit body is a test case, plus a 400 that only mentions billing. **Verified:** the old marker logic fails both cases. |
| 2 | major | Core's identify overlay showed the error notice *and* "No match found". | **Fixed in core** (`identifyResult.Failed`, template). A new core test failed first. |
| 3 | minor | The help wording said key/credit/rate reasons were only for hosted providers, and that identify shows every reason. | **Fixed** (core `web/help/en/reports.md`; the Ask section exists only in English). |
| 4 | minor | `provider_failed` sent a mistyped model name to "check your account". | **Fixed.** The text now also says to check the model names in the plugin's settings. |
| 5 | minor | A 403 is not always a bad key (OpenAI region block, Anthropic permission error). | **Accepted.** The 403 body has been suppressed since 2.x; the notice still points at the key, which is the common cause. |
| 6 | nit | The marker scan also runs on self-hosted 4xx bodies. | **Accepted.** It is harmless. |
| 7 | nit | The "unreachable" test relies on FakeHost mapping a hook error to -3. | **Accepted.** |
| 8 | nit | A failed identify document still parks the photo in core's pending-confirm slot until its TTL. | **Accepted.** No reference photo is stored without a pick. |

The reviewer also checked:
- **Key leakage:** no key fragment reaches the operator or the log. The
  operator only ever sees a key-only notice.
- **Seam:** the document is valid for the identify seam.
- **Regressions:** none for self-hosted or openai_compatible.
- **Translations:** tr, fa and ar are accurate and consistent with the
  existing terms.
- **TDD:** with `failureKey` forced to `""`, the Ask, identify and
  self-hosted tests fail.

## Verified beyond automated tests

- **The owner's real failure body** (OpenAI 429 `insufficient_quota`,
  "You have no credits remaining") is a test case: it maps to
  `integration_ai.error.no_credit`.
- **Gate:** `go vet` (native and wasip1), `go test ./...`,
  `scripts/build.sh`, `validate.sh`, `guard-plugin-i18n.sh` and
  `check-version-bump.test.sh` all pass. shellcheck is not installed in
  this container; no scripts changed.
- **Not driven on a real till:** that needs a signed plugin build. The
  tests run the SDK's real dispatch and wire path on `FakeHost`. The core
  rendering is covered by the core handler test.

**Verdict:** safe to merge.
