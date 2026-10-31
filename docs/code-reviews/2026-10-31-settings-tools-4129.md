# Review: settings_describe / settings_propose tools (ut-docs#4129)

- **Branch:** `feat/4129-ai-settings-tools` (ut-plugin-integration-ai)
- **Author:** Sonnet 5.5. **Reviewer:** Opus 5.5 (independent subagent; joint review with the
  core half, see universal-till `docs/code-reviews/2026-10-31-ai-settings-tools-4129.md`).

## Shipped

- Ask tools `settings_describe` (`view_query settings.describe.v1`) and `settings_propose`
  (`action_propose settings.set.v1`); arguments validated before the host call; a till refusal
  (not allow-listed, no grant, cashier, already in that state) is a result the model explains,
  never a proposal; at most 3 proposals per turn; the job appends a core `confirm` component
  per proposal after the answer, with a one-line hint. The tool result says "awaiting
  confirmation" so the model cannot claim the change is done.
- Prompt guidance only appended while the tools are offered (`ShopContext.Extra`); it forbids
  inventing links and changing non-writable settings.
- Manifest 2.5.0: `ai.agent`, the five `ai.send.*` of the views it already reads,
  `view:shop`, `ai.tools.write:settings` (installs ungranted; an admin grants it),
  `settings.describe.v1`, `min_pos_version` 0.35.4; SDK pin bumped to a commit that has
  `ActionPropose`. Locale key `integration_ai.ask.confirm_hint` in all 7 locales.

## Findings

- Reviewer blocker (declaring `ai.agent` withheld the Ask job) is fixed in core; **this plugin
  must be released only after the core release that contains the fix and the new view/action.**
- `revokes_on_select` for hosted providers (ADR-0153 Decision 1) is not part of this slice: a
  hosted provider already received sales totals through Ask before this change; tracked on the
  AI-settings cards (#2837).

## Verified / not verified

`go test ./...`, native `FakeHost` tests; `scripts/build.sh`, `validate.sh`, i18n guard. Not
run against a real till, model or the live marketplace.
