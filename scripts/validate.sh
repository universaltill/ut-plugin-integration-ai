#!/usr/bin/env bash
# Manifest sanity for the AI Assistant WASM plugin (ut-docs#4032):
# marketplace-required fields, the wasm runtime and its built module, and the
# settings existing installs already carry (same keys, so an update from the
# config-only 1.x keeps the shop's provider/endpoint/models/key).
set -euo pipefail
cd "$(dirname "$0")/.."
python3 - <<'PY'
import json, os, re, sys
m = json.load(open("manifest.json"))
errs = []
if m.get("id") != "com.universaltill.integration-ai": errs.append("id must stay com.universaltill.integration-ai (existing installs)")
for f in ("name", "version", "description"):
    if not m.get(f): errs.append(f"missing {f}")
if not re.match(r'^\d+\.\d+\.\d+', m.get("version", "")): errs.append("bad version")
if m.get("canonical_type") != "integration": errs.append("canonical_type must be 'integration'")
if m.get("device_arch") != "any": errs.append("device_arch must be 'any'")
if m.get("runtime") != "wasm": errs.append("runtime must be 'wasm' (ADR-0001)")
ep = (m.get("entrypoint") or "").lstrip("./")
if not ep.endswith(".wasm"): errs.append("wasm runtime needs a .wasm entrypoint")
elif not os.path.isfile(ep): errs.append(f"module not found: {ep} (run scripts/build.sh)")
if not m.get("permissions"): errs.append("missing permissions")
if not m.get("locales"): errs.append("missing locales")
settings = {s["key"]: s for s in m.get("settings", [])}
want = {"provider": "self_hosted", "endpoint": "http://localhost:11434",
        "vision_model": "llama3.2-vision", "ask_model": "llama3.2", "api_key": ""}
for k, dv in want.items():
    if k not in settings:
        errs.append(f"setting {k} missing (existing installs carry it)")
    elif settings[k].get("default_value") != dv:
        errs.append(f"setting {k} default changed ({settings[k].get('default_value')!r} != {dv!r})")
if settings.get("api_key", {}).get("type") != "secret": errs.append("api_key must be type secret (ADR-0082)")
if settings.get("endpoint", {}).get("type") != "endpoint": errs.append("endpoint must be type endpoint (ADR-0121 §2)")
if settings.get("provider", {}).get("type"): errs.append("provider stays a plain-text setting (ADR-0126)")
# Background removal (ut-docs#3126): the till calls the shop's rembg itself,
# so these settings are plain text. A `type: "endpoint"` image_endpoint would
# give this WASM plugin's http:lan egress to the rembg host it never needs
# (least privilege; core validates the URL where it uses it).
want_img = {"image_provider": "self_hosted", "image_endpoint": "", "image_model": "birefnet-general-lite"}
for k, dv in want_img.items():
    if k not in settings:
        errs.append(f"setting {k} missing (the till reads it for background removal)")
    elif settings[k].get("default_value") != dv:
        errs.append(f"setting {k} default changed ({settings[k].get('default_value')!r} != {dv!r})")
    elif settings[k].get("type"):
        errs.append(f"{k} stays plain text: the till, not the plugin, calls rembg (no plugin egress to it)")
hooks = {h.get("event") for h in m.get("hooks", [])}
job = m["id"] + ".ask"
for ev in ("catalog.identify", "catalog.identify.confirmed", "ui.view.ask", "ui.action.ask", job):
    if ev not in hooks: errs.append(f"missing hook {ev}")
for h in hooks:
    if h.startswith(m["id"] + ".") and not re.match(r'^[a-z0-9_-]+(\.[a-z0-9_-]+)+$', h):
        errs.append(f"job event {h} must be lower-case dotted segments")
perms = set(m.get("permissions", []))
if any(p in perms for p in ("net:*", "tcp:*")): errs.append("no wildcard egress: the endpoint is setting-bound (net:@setting:endpoint)")
if errs:
    print("FAIL: " + "; ".join(errs)); sys.exit(1)
print(f"ok {m['id']} v{m['version']}")
PY
