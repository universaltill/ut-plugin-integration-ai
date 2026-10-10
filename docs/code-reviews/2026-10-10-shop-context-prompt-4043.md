# Review — ut-plugin-integration-ai 2.0.2: Ask prompt from shop.context.v1 (ut-docs#4043)

Built by a Sonnet Dev subagent (card is `complexity:easy`); independent
review and fixes by the orchestrating lane on Opus 5.5 (lane:cloud-41).

## What shipped

- `manifest.json` 2.0.2: `shop.context.v1` added to `views_used` (the
  plugin already holds its permission, `view:sales`).
- `src/app/ask.go`: the hard-coded `shop` var is replaced by
  `shopContext()`, read once per Ask job. It starts from the old fallback
  ("this shop", "the shop's currency", 2 decimals) and overrides each part
  the view supplies: trimmed non-empty `store_name` / `currency_code`,
  `currency_decimals` only when present and 0–4, `locale` as given. A view
  error (an older till returns -1 for the unknown view), unparsable JSON or
  anything but exactly one row keeps the whole fallback and logs one line;
  the answer is never blocked.
- `README.md`: the "does not know the shop's name or currency" limit now
  describes the view and the older-till fallback.

## Findings

1. **Test data used a plausible real business name** ("Kissa Tokyo") —
   fixed: `Sample Shop JP` (standing rule: no real shop names).
2. Checked, no change: the extra view call fits the till's 64 view calls
   per event (worst case 6 rounds × 5 tools + 1); `store_name` reaches the
   prompt through `%q`, so quotes/newlines in a shop name can't break out
   of the sentence; `currency_decimals` is parsed as `*int`, so a row
   missing the field keeps 2 rather than silently becoming 0.
3. Checked, no change: `ShopContext.Locale` is filled but, as in core's
   `askSystemPrompt`, not used in the prompt — identical behaviour kept
   (CLAUDE.md: stay identical to core until ut-docs#2851).

## TDD re-verified

With `src/app/ask.go` and `manifest.json` reverted, four tests fail
(`TestManifestDeclaresEveryViewAndHook`, `TestAskJob_PromptUsesShopContext`,
`TestAskJob_EmptyStoreNameKeepsFallbackName`,
`TestAskJob_OutOfRangeDecimalsFallBack`); restored, all pass. The
fallback test (`TestAskJob_ShopContextUnavailableKeepsFallback`: view
error, bad JSON, zero rows, two rows) passes both ways by design.

## Gate

`gofmt -l .` empty; `go vet ./...` and `GOOS=wasip1 GOARCH=wasm go vet
./...` clean; `go test ./...` ok; `scripts/build.sh` built
`bin/plugin.wasm`; `scripts/validate.sh` ok v2.0.2;
`scripts/guard-plugin-i18n.sh` ok; `shellcheck scripts/*.sh` clean;
`scripts/check-version-bump.test.sh` passed.

## Docs

ut-docs `architecture/ai-plugin.md` "Gaps against the core engine" updated
in a companion ut-docs PR.
