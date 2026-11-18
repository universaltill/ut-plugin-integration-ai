# Review: AI Assistant catalog tools, plugin side (ut-docs#4128)

Branch `feat/4128-ai-catalog-tools`, version 2.5.0. Independent review by
Opus 5.5 (read-only, ran vet/tests/validate).

## What shipped
Ask tools `list_items_with_gaps` (catalog.gaps.v1), `find_items`
(catalog.items.v1) and `propose_item_changes` (action_propose ->
catalog.item.update.v1). The write tool only proposes: the answer carries
core's `confirm` card; found items are shown through core's `item_list`
(ids only). Strict argument validation before core sees anything, max 3
proposals per turn, errors go back to the model as text. Manifest declares
`ai.agent`, `ai.send.{sales,catalog,stock,staff}`,
`ai.tools.write:catalog`, view `catalog.gaps.v1`; SDK bumped to v1.1.0
(first with ActionPropose).

## Findings and disposition
- BLOCKER manifest view map in ask_test - fixed; permissions and
  min_pos_version now pinned by that test.
- MAJOR min_pos_version stayed 0.31.9 (older till = Ask answer rejected on
  the unknown component) - set to 0.36.0: OWNER MUST CONFIRM the core
  release number; merge the core PR first and publish this after.
- MAJOR mixed find/gap list gave a wrong catalog link - fixed + test.
- Minor: README grant/cashier wording fixed. Accepted: `ai.agent` makes the
  existing Ask views subject to `ai.send.*` and the operator's role
  (`till_activity_summary` needs user_management); `find_items` pages up to
  8 views reads per call.
- Not done by design: `revokes_on_select` for hosted providers (core has no
  such manifest field yet, ADR-0153 Decision 1); image find/edit.

## Verified
vet (native + wasip1), `go test ./...`, build, validate.sh,
guard-plugin-i18n, version-bump test. NOT verified against a real till or a
real model; the loop is exercised on FakeHost with scripted model replies.

## Verdict
Safe to merge only after the core release that ships #4128 exists.
