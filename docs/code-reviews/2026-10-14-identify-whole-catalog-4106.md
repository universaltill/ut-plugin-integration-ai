# Review — identify: photographed items past name 500 are matchable (ut-docs#4106)

**Shipped (2.4.2).** `catalog.identify` with a provider that sends reference
photos (Claude, OpenAI) now:
- reads the whole active catalog (`catalog.items.v1`, at most 20 pages ×
  500 rows, inactive rows included) instead of stopping at 500 active items;
- reads `catalog.photos.v1` as one page of 500 rows (the view's maximum),
  not 120. The host's 64 opens per event bound the walk, so one page is
  always enough;
- sends the model the first 500 names **plus** each referenced item past
  them (`promptCatalog`, at most +60). A photographed item past name 500 is
  therefore opened, sent, listed and suggestible.

Ollama and openai_compatible still read and send core's first 500 names
(one page), unchanged. Matching an **unphotographed** item past name 500
is a prompt-size and cost trade-off, split out to ut-docs#4288.

**Review.** Independent review by a different model (Fable; the author was
Opus 5.5). No blocker or major findings.
- minor: "reads the whole catalog" is really at most 10,000 rows → README
  wording fixed; `plugin.Logf` when the page cap is reached. **Fixed.**
- minor: the paging bound wasn't pinned by a test →
  `TestIdentify_HostedCatalogReadIsBounded` (21 view calls, which is 20
  pages plus the photo view, under the 64 cap). **Fixed.**
- nit: the comment called `promptCatalog` "the one place" the plugin
  differs from core, but #4094's photo order is another → reworded.
  **Fixed.**
- nit: up to 20 view calls at the 15 % progress step on a 10k-item
  catalog. Accepted: it's a paged SQLite read and took about 60 ms on
  FakeHost.

**Verified.**
- TDD: with origin/main's `identify.go`,
  `TestIdentify_PhotographedItemPastFiveHundredIsMatchable` fails (photo
  view `limit` 120). With only the limit bumped to 500 it still fails ("sent
  1 images"), so the test pins the catalog walk and not just the constant.
  It passes on the branch.
- Slice aliasing in `promptCatalog` (three-index slice; `append`
  reallocates), and each ref's item is in the prompt list.
  `Identify`'s hallucination filter and `identifyDocument` use the same
  list.
- Gate: gofmt, go vet (native + wasip1), go test, build.sh, validate.sh,
  guard-plugin-i18n.sh, check-version-bump.test.sh all clean. shellcheck
  isn't installed in the cloud container; no script changed.
- No UI surface changed (the identify overlay is the till's), so there was
  no driven UI run. The behaviour is covered on `plugin.FakeHost` with the
  real view bounds.

**Verdict:** safe to merge. **Deferred:** ut-docs#4288.
