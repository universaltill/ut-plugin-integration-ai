#!/usr/bin/env bash
#
# Requires a manifest.json version bump on any PR that touches a shipped
# file (ut-docs#1940, rolled out here per ut-docs#1948).
#
# WHY THIS EXISTS: scripts/package.sh bundles manifest.json, README.md and
# LICENSE into the marketplace release artifact. None of this repo's other
# checks (validate, authors) look at the version field, so a PR that edits
# README.md but forgets to bump manifest.json's version lands on main,
# passes every check, and then SILENTLY NEVER SHIPS: auto-tag-release.yml
# reads the unchanged version, sees v<version> already tagged/released, and
# correctly does nothing. main and the marketplace diverge with no failing
# signal anywhere. This exact failure has already needed hand repair
# multiple times in the language-pack plugins (v1.1.32, v1.1.33->34, and
# twice more on 2026-09-09 alone) — this repo hasn't hit it yet, but the
# mechanism that causes it (package.sh's bundle vs. no CI check on version)
# is identical here, hence the preventive rollout.
#
# WHAT COUNTS AS "SHIPPED": deliberately keyed off the same set
# scripts/package.sh actually bundles, not "any file" -- a docs/code-reviews/
# record or a workflow-only edit must never force a version bump. This repo's
# package.sh has no `entries=(...)` array (unlike the theme/language-pack
# plugins it was ported from) -- it hardcodes the bundled file list directly
# in its `tar -czf "$OUT" ...` line, since this is a config-only plugin with
# no assets/src at all. Keep this list mirrored to that line; nothing
# enforces that automatically, so a change to package.sh's bundle needs a
# matching edit here (and check-version-bump.test.sh's "tar line mirror"
# case exists to catch drift between the two by re-reading package.sh's own
# source).
set -euo pipefail
cd "$(dirname "$0")/.."

SHIPPED_PATTERNS=(
    'manifest.json'
    'README.md'
    'LICENSE'
)

# BASE_SHA/HEAD_SHA follow the same convention as
# .github/workflows/commit-attribution.yml: passed in by the workflow from
# the pull_request event (github.event.pull_request.base.sha / head.sha).
# For a local/manual run, fall back to comparing against origin/main.
BASE_SHA="${BASE_SHA:-}"
HEAD_SHA="${HEAD_SHA:-}"
if [ -z "$BASE_SHA" ]; then
    BASE_SHA=$(git merge-base HEAD origin/main 2>/dev/null || true)
fi
if [ -z "$HEAD_SHA" ]; then
    HEAD_SHA=$(git rev-parse HEAD)
fi
if [ -z "$BASE_SHA" ]; then
    echo "ERROR: could not determine BASE_SHA (set BASE_SHA explicitly, or ensure origin/main is fetched)"
    exit 1
fi

# github.event.pull_request.base.sha is the BASE BRANCH'S TIP at event time,
# not this PR's merge-base -- it advances as main advances. Diffing/reading
# directly against BASE_SHA (two-dot) leaks main's own later commits into
# "what this PR changed", in both directions: once ANY other shipped PR
# merges and bumps the version, every other still-open shipped-file PR
# would start comparing against that higher version and pass unbumped
# (false negative -- exactly the multi-lane scenario in the header above);
# and a docs-only PR whose base.sha happens to sit after an unbumped
# shipped change on main would be told to bump for someone else's change
# (false positive). Compare against the merge-base instead, so only what
# THIS PR itself introduced is ever in scope.
MERGE_BASE=$(git merge-base "$BASE_SHA" "$HEAD_SHA") || {
    echo "ERROR: could not compute merge-base of ${BASE_SHA} and ${HEAD_SHA}"
    exit 1
}

# core.quotePath=false so a shipped path with non-ASCII characters is
# printed as a real UTF-8 path, not C-style-quoted octal escapes that would
# never match SHIPPED_PATTERNS.
changed_files=$(git -c core.quotePath=false diff --name-only "$MERGE_BASE" "$HEAD_SHA" -- .)

shipped_changed=()
while IFS= read -r f; do
    [ -n "$f" ] || continue
    for pat in "${SHIPPED_PATTERNS[@]}"; do
        # Intentional unquoted glob match ($pat is a pattern, not a literal).
        # shellcheck disable=SC2053
        if [[ "$f" == $pat ]]; then
            shipped_changed+=("$f")
            break
        fi
    done
done <<<"$changed_files"

if [ "${#shipped_changed[@]}" -eq 0 ]; then
    echo "ok: no shipped file changed (${MERGE_BASE:0:9}..${HEAD_SHA:0:9}) -- no version bump required"
    exit 0
fi

read_version() {
    # $1 = git ref. Fails closed (non-empty error) if manifest.json is
    # missing or doesn't parse at that ref, rather than silently treating
    # it as "no version".
    git show "$1:manifest.json" 2>/dev/null | python3 -c '
import json, sys
try:
    print(json.load(sys.stdin)["version"])
except Exception as e:
    print(f"ERROR:{e}", file=sys.stderr)
    sys.exit(1)
'
}

base_version=$(read_version "$MERGE_BASE") || {
    echo "ERROR: could not read manifest.json version at merge-base ${MERGE_BASE:0:9}"
    exit 1
}
head_version=$(read_version "$HEAD_SHA") || {
    echo "ERROR: could not read manifest.json version at head ${HEAD_SHA:0:9}"
    exit 1
}

fail_no_bump() {
    # $1 = the specific reason line (already ends without a period).
    {
        echo "FAIL: ${1}"
        echo ""
        echo "Changed shipped file(s):"
        printf '  - %s\n' "${shipped_changed[@]}"
        echo ""
        echo "Why this fails the build: scripts/package.sh bundles these files into"
        echo "the release artifact, and auto-tag-release.yml only cuts a release when"
        echo "manifest.json's version differs from the last tag. Without a genuinely new"
        echo "version, this change lands on main and then SILENTLY NEVER SHIPS -- the"
        echo "marketplace keeps serving the old artifact with no failing signal anywhere"
        echo "(ut-docs#1940)."
        echo ""
        echo "Fix: bump manifest.json's \"version\" in this PR to ${2}."
    } >&2
    exit 1
}

# Suggest a patch bump as a starting point -- the author picks minor/major
# if the change actually warrants it. Only offered when every segment is a
# plain integer (semver's optional -prerelease/+build metadata, e.g.
# "1.0.0-beta.1", makes `patch` a non-integer string like "0-beta.1" and
# must never be allowed to reach arithmetic -- a version scheme this repo's
# own validate.sh/auto-tag-release.yml already accept must not silently
# defeat this guard).
IFS='.' read -r major minor patch <<<"$head_version"
if [[ "$major" =~ ^[0-9]+$ && "$minor" =~ ^[0-9]+$ && "$patch" =~ ^[0-9]+$ ]]; then
    suggested="a version higher than ${head_version}, e.g. \"${major}.${minor}.$((patch + 1))\""
else
    suggested="a version higher than ${head_version}"
fi

if [ "$base_version" = "$head_version" ]; then
    fail_no_bump "shipped file(s) changed but manifest.json's version is still ${head_version}" "$suggested"
fi

# The version differs from this PR's own base -- but a version that was
# already released elsewhere is just as silent a failure: auto-tag-
# release.yml sees the tag already exists and treats it as nothing to do.
# fetch-depth: 0 in the workflow fetches tags along with full history.
if git rev-parse -q --verify "refs/tags/v${head_version}" >/dev/null 2>&1; then
    fail_no_bump "manifest.json's version changed to ${head_version}, but v${head_version} is ALREADY TAGGED (released)" "$suggested"
fi

echo "ok: manifest.json version bumped ${base_version} -> ${head_version}, covering:"
printf '  - %s\n' "${shipped_changed[@]}"
