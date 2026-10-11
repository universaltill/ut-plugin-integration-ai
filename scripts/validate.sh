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
# provider is a dropdown (ut-docs#2832, owner 2026-10-11): exactly these
# stored values — the plugin's fail-safe treats anything else as
# self-hosted, so an option here must have an adapter in src/ai.
pv = settings.get("provider", {})
if pv.get("type") != "choice": errs.append("provider must be type choice (ut-docs#2832)")
elif [o.get("value") for o in pv.get("options", [])] != ["self_hosted", "openai", "claude", "openai_compatible"]:
    errs.append("provider options must be exactly self_hosted|openai|claude|openai_compatible (src/ai adapters)")
# An older till refuses a manifest with `type: "choice"` (and fails
# signature verification on any field it doesn't know), so the plugin must
# name the first till release that understands it (ut-docs#2832).
mpv = tuple(int(x) for x in re.findall(r'\d+', m.get("min_pos_version", "0"))[:3])
if mpv < (0, 31, 9): errs.append("min_pos_version must be >= 0.31.9 (first till with choice settings, ut-docs#2832)")
# Fields that don't apply to the chosen provider are hidden (show_when).
sw = lambda k: settings.get(k, {}).get("show_when") or {}
if sw("endpoint") != {"key": "provider", "in": ["self_hosted", "openai_compatible"]}: errs.append("endpoint shows for self_hosted/openai_compatible only")
if sw("api_key") != {"key": "provider", "in": ["openai", "claude", "openai_compatible"]}: errs.append("api_key shows for openai/claude/openai_compatible only")
# Every setting carries a translated label (a key in locales/en.json).
en = json.load(open("locales/en.json"))
for k, s in settings.items():
    for f in ("label", "help"):
        if f == "label" and not s.get(f): errs.append(f"setting {k} needs a label")
        if s.get(f) and s[f] not in en: errs.append(f"setting {k} {f} {s[f]!r} missing from locales/en.json")
    for o in s.get("options", []):
        if o.get("label") not in en: errs.append(f"setting {k} option {o.get('value')} label missing from locales/en.json")
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
