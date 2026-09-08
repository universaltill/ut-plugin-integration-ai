# Code review: auto-tag-release.yml (release-on-merge automation rollout)

**Date:** 2026-09-08
**Card:** ut-docs#1700 (mechanical rollout, continued from ut-docs#1694)
**Author:** scrum-master pipeline (cloud cycle, `lane:cloud-41`), on behalf of Farshid Mirza

## What changed

Added `.github/workflows/auto-tag-release.yml`, copied byte-for-byte from
the canonical, independently-reviewed copy in `ut-plugin-tax-de`
(`docs/code-reviews/2026-09-07-auto-tag-release-workflow-1694.md`). No
repo-specific deviation was needed or made.

## Live drift this repo is actually in — real end-to-end proof expected

`manifest.json`'s `version` is `1.1.0`, but the newest tag reachable on
`main` is only `v1.0.2` — this repo is live in exactly the state
ut-docs#1694/#1700 exist to fix. Merging this workflow is expected to
create tag `v1.1.0` and dispatch a real `release.yml` run on the first
push to `main` that carries it. DevOps must verify this actually happens.

## Independent review (fresh-context Sonnet subagent, per `complexity:easy` routing)

Verdict: **SAFE TO MERGE**, no blocking findings. The subagent
independently ran `diff` against the canonical source (byte-identical),
parsed the copied YAML (clean), confirmed `manifest.json` at repo root
with the valid semver `version` above, confirmed `release.yml` is
tag-triggered with `workflow_dispatch` inputs named exactly `channel`/
`publish` (no adaptation needed), confirmed no conflicting automation and
no `pull_request_target` exposure, and confirmed `main` is this repo's
actual default/integration branch.

**Non-blocking observation:** this repo has no `ci.yml` (the other repos
in this batch do), so `manifest.json`'s semver shape isn't enforced by a
separate PR gate here the way the canonical file's own comment describes
as "defense in depth" for repos that have one. Not a blocker — the new
workflow's own regex guard (`^[0-9]+\.[0-9]+\.[0-9]+...`) still protects
it directly — but worth a separate, narrower backlog note if this repo's
CI coverage gets picked up later; not filing a new card for it here since
it's pre-existing and out of scope for this rollout.

## Verification performed

- `diff` against canonical source: byte-identical.
- `python3 -c "import yaml; yaml.safe_load(open('.github/workflows/auto-tag-release.yml'))"` — parses.
- Confirmed `manifest.json` version and `release.yml` dispatch input names via direct file inspection.
- DevOps to confirm the live effect after merge: new tag `v1.1.0` created
  on `main`, and a `release.yml` run dispatched and green.

## Non-goals confirmed out of scope

- Adding `ci.yml` to this repo (pre-existing gap, noted above, not this card's scope).
- The remaining `ut-plugin-*` repos in ut-docs#1700's scope (tracked on that issue).
