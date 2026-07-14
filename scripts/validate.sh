#!/usr/bin/env bash
# Manifest sanity for this config-only integration plugin.
set -euo pipefail
cd "$(dirname "$0")/.."
python3 - <<'PY'
import json, sys
m = json.load(open("manifest.json"))
errs = []
if m.get("canonical_type") != "integration": errs.append("canonical_type must be 'integration'")
if m.get("runtime") != "none": errs.append("runtime must be 'none'")
if m.get("entries"): errs.append("config-only plugin must have no entries")
keys = {s["key"] for s in m.get("settings", [])}
if not {"endpoint", "vision_model", "ask_model"} <= keys:
    errs.append("settings must include endpoint/vision_model/ask_model")
for f in ("id","name","version","description"):
    if not m.get(f): errs.append(f"missing {f}")
if errs:
    print("FAIL: " + "; ".join(errs)); sys.exit(1)
print(f"ok {m['id']} v{m['version']}")
PY
