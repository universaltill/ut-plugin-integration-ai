# Code review: manifest.json version-bump guard (ut-docs#1948)

**Date:** 2026-09-10
**PR:** #6 (`feat/1948-version-bump-guard`)
**Card:** ut-docs#1948 (`complexity:medium`) — rollout of the ut-docs#1940
guard to the remaining `ut-plugin-*` repos, one repo per slice.

## What shipped

Rolls out the version-bump CI guard already shipped in
`ut-plugin-language-{de,es}` and the three theme repos
(`-midnight`, `-screen-top`, `-buttons-left`) to this repo — a
config-only AI-assistant integration plugin (`runtime: "none"`, no
assets/src).

This repo is a **structural variant**, not a copy-paste: `scripts/
package.sh` has no `entries=(...)` bash array (unlike every repo this
guard has landed in so far) — it hardcodes the bundled file list
directly in its `tar -czf "$OUT" manifest.json README.md LICENSE`
invocation. So:

- `SHIPPED_PATTERNS` in `check-version-bump.sh` is a plain list
  (`manifest.json`, `README.md`, `LICENSE`), no `assets/*` glob.
- The self-test's cross-check (case 8, "tar line mirror") parses that
  `tar -czf` line's real arguments instead of an `entries=(...)` array.
- Already carries the F1 shellcheck-directive fix (bare `# shellcheck
  disable=SC2053` on its own line) that `-de`/`-es` shipped without and
  had to be back-ported — this repo starts clean.

## Independent review (two concurrent passes — see note below)

Fresh-context Opus subagent, isolated worktree (`isolation: "worktree"`),
per `complexity:medium` routing. Did not write this code.

**Verdict: SAFE WITH NITS, no blockers.**

What it actually ran (not just read): `bash scripts/
check-version-bump.test.sh` (all 11 cases, each verified for the claimed
reason via grep on the actual failure text, not just exit code);
`shellcheck scripts/*.sh` (0 findings, confirmed the SC1072/SC1073
malformed-directive bug this rollout hit before is genuinely absent);
~8 hand-reproduced scenarios against the real tagged history
(v1.0.0…v1.2.0) — unbumped shipped-file change fails naming the file and
suggesting the next patch version; bump to an untagged version passes;
bump to an already-tagged version fails on the tag check; docs/`.github`-
only change is exempt; a pre-release suffix (`1.3.0-rc.1`) degrades to a
generic suggestion instead of crashing the bump-suggestion arithmetic; a
deleted `manifest.json` at head fails closed with a real error; the
merge-base logic holds in both the false-negative direction (an
independent bump on `main` doesn't cover for this PR) and the
false-positive direction (an unrelated unbumped change on `main` doesn't
force a bump on a docs-only PR). It also deliberately broke the "tar line
mirror" self-test by dropping a tracked file from `package.sh`'s bundle
(`SHIPPED_PATTERNS` over-claiming) and found the self-test doesn't catch
that direction — logged as a one-directional nit, fail-safe not fail-open.

**Note on the concurrent second pass.** While this review was in flight,
a second, independent session pushed a further commit
(`78d488d`, "Address independent review findings on the version-bump
guard") directly onto this same PR branch — its own independent Opus
review, run against an earlier state of the diff. Rather than being a
duplicate, it went one level deeper on the exact same self-test area:
**it found a real bug, not just an asymmetry, in the "tar line mirror"
check.** The old grep (`grep -oE "'[^']*'" "$REAL_SCRIPT"`) scanned the
*entire* script for single-quoted strings, which also matches the bare
`'.'` literal from `IFS='.'` — I independently reproduced this myself
(mutated `package.sh`'s bundle to include a literal `.` path; against
the *old* grep logic it appears in the extracted pattern set and would
have spuriously satisfied the mismatch check, silently defeating this
exact self-test case). `78d488d` fixes it by scoping the grep to the
`SHIPPED_PATTERNS=(...)` block specifically, and I re-verified that fix:
the same mutation now correctly fails (`FAIL [tar line mirror]:
package.sh bundles '.' but SHIPPED_PATTERNS ... has no matching entry`).
It also independently fixed the same `if: always()` dead-code nit, added
the same missing `validate` job (mirroring the sibling theme/language-
pack repos' `ci.yml` shape — I diffed it directly against
`ut-plugin-theme-midnight`'s merged `ci.yml` to confirm it's a faithful
port), and added a `permissions: contents: read` block for parity with
this repo's other three workflows.

### Findings and final disposition

| # | Finding | Severity | Disposition |
|---|---|---|---|
| N1 | "Tar line mirror" self-test's unscoped grep could be spuriously satisfied by the bare `'.'` from `IFS='.'`, silently defeating the mismatch check | **real bug**, not just an asymmetry | **Fixed** (`78d488d`) — grep scoped to the `SHIPPED_PATTERNS=(...)` block. Independently reproduced the failure mode and re-verified the fix myself. |
| N2 | `ci.yml` (a **brand-new file** — this repo had no CI workflow before this PR) shipped with only the `version-bump` job, omitting the `validate` job every sibling repo's `ci.yml` carries | should-fix | **Fixed** (`78d488d`) — added, matching `ut-plugin-theme-midnight`'s `ci.yml` exactly. Re-ran `validate.sh`/`package.sh` locally, both clean. |
| N3 | `if: always()` on the self-test step removes the "previous step succeeded" gate — a checkout failure would still run the (missing) test script and mask the real error | nit (inherited from every sibling repo) | **Fixed** (`78d488d`) — dropped. (This diverges this repo from the sibling repos' still-unfixed copies; worth carrying the same fix back to them in an ecosystem-wide follow-up.) |
| N4 | Job-level `if: github.event_name == 'pull_request'` was dead code while `ci.yml` only had a `pull_request` trigger | — | Made meaningful: `78d488d` added the `push` trigger `validate` needs, and updated the guard's comment to match. |
| N5 | No `permissions:` block, unlike this repo's other three workflows | nit | **Fixed** (`78d488d`) — added `contents: read`. |
| N6 | Both `CLAUDE.md` and the script's header comment describe the release trigger as "when the version differs from the last tag" — `auto-tag-release.yml`'s actual rule is "when no tag matches THIS version" (a downgrade to an untagged lower value would still cut a release) | nit | **Deferred** — pre-existing prose inherited identically across all 6 shipped repos; the guard's own code (`refs/tags/v${head_version}` check) already matches the workflow's true semantics, only the prose is imprecise. Out of this guard's remit; not fixed here to avoid a one-repo wording divergence — worth an ecosystem-wide prose fix. |
| N7 | `CLAUDE.md`'s exemption sentence ("A PR touching only `docs/`, `.github/` or `scripts/` is exempt") is an incomplete restatement of the real allowlist-inverse rule — this PR's own `CLAUDE.md` change isn't covered by its own wording | nit | **Fixed** (this commit) — reworded to "A PR that touches none of those three files is exempt (`docs/`, `.github/`, `scripts/`, `CLAUDE.md`, …)". |
| N8 | Residual same-version race: two PRs independently bump to the same version; whichever merges second carries a stale-green check from before the first's tag existed, and silently never ships | should-fix (rollout-level, not this PR) | Not fixed here — identical exposure in all 6 shipped repos; the actual fix (require branches up-to-date before merge) is a repo/org setting outside this PR's scope. Recorded here for whoever picks up the rollout's remaining Go/WASM-plugin slice. |
| N9 | Mirror self-test still only checks `package.sh` entries ⊆ `SHIPPED_PATTERNS`, not the reverse (an over-claiming `SHIPPED_PATTERNS` after a bundle shrink still passes) | nit | Deferred — fail-safe direction only (produces a spurious bump requirement, never a missed real change). |
| N10 | No test case covers the `BASE_SHA` local-fallback path (`git merge-base HEAD origin/main`) | nit | Deferred — low risk, CI always supplies `BASE_SHA` explicitly. |

Also independently confirmed: no real client/shop name anywhere in the
fixtures (generic `com.universaltill.integration-ai`, "AI Assistant",
`test@example.com`); no secret-shaped literal added by this diff;
`fetch-depth: 0` does fetch what the script needs (tags included); both
new scripts are committed executable (`100755`).

## What I (Scrum Master, closing this out) additionally verified myself

- Re-ran the full self-test suite, `validate.sh`, and `package.sh`
  against the final diff (both commits combined) — all green.
- `shellcheck scripts/*.sh` — 0 findings.
- `python3 -c "import yaml; yaml.safe_load(open('.github/workflows/ci.yml'))"`
  — `ci.yml` parses as valid YAML.
- Independently reproduced N1's failure mode (see above) rather than
  taking the concurrent session's fix on its word.
- Diffed the `validate` job against `ut-plugin-theme-midnight`'s merged
  `ci.yml` directly (cloned fresh) to confirm it's a faithful port.

## Safe to merge

Yes — no blockers. N1/N2/N3/N5/N7 fixed across the two review-fix
commits on this PR; N6/N8/N9/N10 are either inherited-and-deferred for
cross-repo parity (same convention this rollout's earlier slices used)
or genuinely out of this guard's scope.

**Remaining scope on ut-docs#1948** (unchanged): design variant needed
before the Go/WASM plugins (`tax-de`, `tax-uk`, `payment-{sumup,qrpay,
stripe,demo}`, `button-nosale`, `integration-webhook`) are safe to roll
this guard out to — their `package.sh` bundles a gitignored `bin/` build
artifact, invisible to this guard's git-diff-based detection.
