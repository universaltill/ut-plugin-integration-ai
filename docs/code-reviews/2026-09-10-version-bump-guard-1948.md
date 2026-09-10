# Code review: manifest.json version-bump guard (ut-docs#1948)

**PR:** [#6](https://github.com/universaltill/ut-plugin-integration-ai/pull/6) —
merged as `86a160c0` into `main`.
**Card:** ut-docs#1948 (rollout of ut-docs#1940's guard; this repo is one
slice of that card's `ut-plugin-*` rollout, which stays open for the
remaining scope below).

## What shipped

Ports the "manifest.json version-bump guard" already shipped in
`ut-plugin-language-de`, `ut-plugin-language-es`, `ut-plugin-theme-midnight`,
`ut-plugin-theme-screen-top` and `ut-plugin-theme-buttons-left` to this
config-only plugin (`runtime: "none"`, no assets/src — just
`manifest.json` + `README.md` + `LICENSE`). Fails CI on any PR that
changes a shipped file without also bumping `manifest.json`'s `version`,
preventing the silent-never-ships failure `auto-tag-release.yml`'s design
otherwise allows (ut-docs#1940).

**The one real adaptation**: this repo's `scripts/package.sh` has no
`entries=(...)` array (every prior repo in the rollout has one) — it
hardcodes the bundle directly: `tar -czf "$OUT" manifest.json README.md
LICENSE`. So `SHIPPED_PATTERNS` is a plain 3-item list, and the self-test's
cross-check ("tar line mirror") parses that `tar` line's real arguments
instead of an `entries=(...)` array.

Files added/changed: `scripts/check-version-bump.sh`,
`scripts/check-version-bump.test.sh`, `.github/workflows/ci.yml` (new —
this repo had no PR-triggered CI at all before this change), `CLAUDE.md`.

## Independent review

Fresh-context **Opus** subagent (per `complexity:medium` → Opus review),
isolated `git worktree` (`.claude/worktrees/agent-a18689e5a2d0ded1b`,
removed after the review completed). **Verdict: SAFE TO MERGE**, no
blocking findings.

It actually ran the suite rather than just reading it: `scripts/check-
version-bump.test.sh` (11/11 pass), `shellcheck` v0.10.0 downloaded fresh
(0 findings across both new scripts), `scripts/validate.sh` +
`scripts/package.sh` + `tar -tzf` on the real artifact (members exactly
`manifest.json`, `README.md`, `LICENSE`), and independently re-derived the
TDD claim on real scratch commits on top of the PR branch (each reverted
with `git reset --hard` back to the guard commit before anything else) —
README edited without a bump correctly FAILs naming the file and the
unchanged version; the same edit plus a bump correctly PASSes.

It also mutation-tested the "tar line mirror" self-test 8 different ways
and found the deliberate asymmetry explicitly: the check verifies
*bundle ⊆ SHIPPED_PATTERNS*, never the reverse, so a bundle that **shrinks**
(e.g. `package.sh` drops `LICENSE`) still passes — over-strict but
fail-safe, not a miss of the failure mode this guard exists to prevent
(a bundle that **grows** without a matching pattern correctly fails).

### Findings and outcome

No blockers. Six non-blocking nits, five fixed in a same-PR follow-up
commit (`78d488d`), one deliberately deferred:

| # | Finding | Outcome |
|---|---|---|
| N1 | The self-test's pattern grep scanned the *whole* script, not just `SHIPPED_PATTERNS=(...)` — a stray single-quoted `.` (from `IFS='.'` elsewhere in the script) could spuriously satisfy the mirror check. Reviewer proved it live: mutating `package.sh`'s bundle to a bare `.` made the self-test wrongly pass. | **Fixed.** Grep scoped to the `SHIPPED_PATTERNS=(...)` block via `sed -n '/^SHIPPED_PATTERNS=(/,/^)/p'` first. Re-verified: the same `.` mutation now correctly FAILs. |
| N2 | This repo had **no `ci.yml` at all** before this PR — no PR ever ran `scripts/validate.sh`, only the tag-triggered `release.yml` did. `auto-tag-release.yml`'s own comment ("validate.sh already enforces this shape on every PR (ci.yml)") was already false and this PR's new `ci.yml` would have kept it false. | **Fixed.** Added a `validate` job (mirrors the sibling theme/language-pack repos' `ci.yml`) so a PR breaking `manifest.json` is caught before release time. |
| N3 | `if: always()` on the self-test step is a no-op (first step after checkout — nothing upstream to override). | **Fixed.** Removed. |
| N4 | The `version-bump` job's `if: github.event_name == 'pull_request'` guard was redundant with `on: pull_request` being the only trigger, and its comment described push/schedule/workflow_dispatch triggers that didn't exist. | **Fixed, and now genuinely load-bearing**: adding the `validate` job's `push` trigger (N2) means the guard is no longer redundant; comment updated to match reality. |
| N5 | No `permissions:` block, unlike the other three workflows in this repo (each declares at least `contents: write`/`actions: write` for what they need). | **Fixed.** Added `permissions: contents: read` — least privilege, and consistent with house style. |
| N6 | A **version downgrade** (e.g. 1.2.0 → 1.1.0) still passes the guard — it checks inequality, not ordering. Requires deliberately picking a lower version; the separate already-tagged check catches the common accidental-reuse case. | **Deferred**, deliberately. This is a pre-existing limitation inherited unchanged from every other repo in the rollout (`-de`, `-es`, all three theme repos) — fixing it here alone would make this repo's guard behave differently from its five siblings for no benefit. Worth a follow-up applied to all six together, tracked as future work on ut-docs#1948's remaining scope, not a one-repo fix. |

Also verified: `bundle_body="${tar_line#*\$OUT\"}"` parsing degrades safely
(loud FAIL, not silent wrong-answer) under every plausible rewrite tried
(`"${OUT}"`, a quoted filename, an extra option, an empty bundle) — no
`set -u` crash on the empty case. `CLAUDE.md`'s new paragraph checked
accurate against the real shipped-file list and consistent with the
file's existing ADR-0009 framing. No client/shop names, no literal
secrets in any new file.

## What was verified beyond automated tests (post-fix, before merge)

- Re-ran `scripts/check-version-bump.test.sh` (11/11), `shellcheck
  scripts/*.sh` (0 findings), `scripts/validate.sh`, `scripts/package.sh`
  (then discarded `dist/`) after applying the N1–N5 fixes.
- Re-ran the N1 mutation (bare `.` bundle) — now correctly FAILs.
- Re-ran the fail-safe-direction mutation (drop `LICENSE`) — still
  correctly passes, confirming the fix didn't accidentally make the check
  bidirectional (which would have been a behavior change, not a bug fix).
- CI green on the PR head (`78d488d`): `version-bump`, `validate`,
  `authors`.
- **Merge verified against unrelated-content-loss** (ut-docs#1876-class
  check): captured `main`'s pre-merge SHA (`5696515`), diffed it against
  the post-merge SHA (`86a160c`), and confirmed that diff is byte-for-byte
  identical to the PR branch's own diff relative to its merge-base — the
  merge introduced exactly this PR's changes and nothing else.
- CI green on the resulting `main` commit (`validate` succeeded,
  `version-bump` correctly `skipped` on a push event per its own PR-only
  guard); `auto-tag-release.yml` correctly no-opped (no version change in
  this PR).

## Safe-to-merge verdict

**Yes.** Independent Opus review found no blockers; all five actionable
non-blocking nits were fixed and re-verified in the same PR; CI is green
on both the PR head and the resulting `main` commit; the merge introduced
exactly the PR's own diff.

## A note on this PR's own process (not a code finding)

While this PR was open, a concurrent pipeline cycle's stale-PR sweep
(step 0c) found the first commit (`b492a6a`) with no review-record
evidence yet in its diff and posted a claim comment believing this PR was
a dead cycle's abandoned work — a reasonable read of the state at that
exact moment, since the independent review and its fix-up commit hadn't
landed yet. This PR was not actually abandoned; the review had already
started (and later reported SAFE TO MERGE) in the same session that
opened it. Left a comment on the PR clarifying this before merging, and
confirmed `main` hadn't moved (no independent/duplicate merge occurred)
immediately before merging. No board/label action was needed since this
PR's linked card (ut-docs#1948) stays open regardless (see below) — but
this is exactly the collision the ecosystem's PR-claim-marker convention
(`PR-SWEEP.md`) exists to make visible rather than silent.

## Remaining scope (unchanged, tracked on ut-docs#1948 — not this PR)

Design variant still needed before the Go/WASM plugins (`tax-de`,
`tax-uk`, `payment-{sumup,qrpay,stripe,demo}`, `button-nosale`,
`integration-webhook`) are safe to roll this guard out to — their
`package.sh` bundles a gitignored `bin/` build artifact, invisible to
this guard's git-diff-based detection. `ut-plugin-integration-ai`'s
non-array `package.sh` convention (the gap this PR closes) is now done.

## Addendum (same-day follow-up, a separate small PR)

A second concurrent Opus review pass (run independently, in its own
isolated worktree, unaware of this record until after the fact) caught
one more nit neither this record's pass nor N1–N6 above did:

| # | Finding | Severity | Disposition |
|---|---|---|---|
| N7 | `CLAUDE.md`'s exemption sentence ("A PR touching only `docs/`, `.github/` or `scripts/` is exempt") is an incomplete restatement of the real allowlist-inverse rule — this PR's own `CLAUDE.md` edit wasn't covered by its own wording | nit | **Fixed** in a same-day follow-up PR — reworded to "A PR that touches none of those three files is exempt (`docs/`, `.github/`, `scripts/`, `CLAUDE.md`, …)". That follow-up also adds `.claude/worktrees/` to `.gitignore` (this was the first agent-review worktree ever created in this repo — same hygiene fix already applied in `ut-plugin-theme-{midnight,screen-top,buttons-left}`). |

Two independent review passes landing on the same PR within minutes of
each other, each catching something the other didn't, is worth noting
plainly rather than quietly reconciling away: it's the concrete case
for running the review as a genuinely fresh, independent pass rather
than trusting a single verdict.
