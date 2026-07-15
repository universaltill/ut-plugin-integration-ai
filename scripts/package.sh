#!/usr/bin/env bash
# Config-only plugin (runtime "none", no assets): the artifact is the
# manifest + docs. One universal archive, no "./" members.
set -euo pipefail
cd "$(dirname "$0")/.."
scripts/validate.sh
ID=$(python3 -c "import json;print(json.load(open('manifest.json'))['id'])")
VERSION=$(python3 -c "import json;print(json.load(open('manifest.json'))['version'])")
OUT="dist/${ID}_${VERSION}_universal.tar.gz"
mkdir -p dist
# COPYFILE_DISABLE stops macOS tar shipping AppleDouble ._* junk (the
# marketplace bundle-hygiene gate rejects it).
COPYFILE_DISABLE=1 tar -czf "$OUT" manifest.json README.md LICENSE
if command -v sha256sum >/dev/null 2>&1; then sha256sum "$OUT" > "${OUT}.sha256"; else shasum -a 256 "$OUT" > "${OUT}.sha256"; fi
echo "packaged $OUT"
