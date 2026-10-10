# AI Assistant (`com.universaltill.integration-ai`)

Installing this plugin **switches on** Universal Till's AI features:

- 📷 **Identify by camera** on the sale screen (barcode failed? photograph
  the product and pick the match),
- 💬 **Ask your till** on the Reports page (plain-language questions
  about sales, stock and till activity),
- ✂️ **Background removal** for item and category photos, run by your own
  [rembg](https://github.com/danielgatis/rembg) service — see
  [Background removal](#background-removal) below. The button on the
  item and category photo pickers arrives with a coming till release
  (ut-docs#3127); until then these settings are stored but unused.

By default identify and Ask run against **your own [Ollama](https://ollama.com)
server** — on the till, a machine in the store, or your homelab. Nothing
leaves your infrastructure, there is no account and no metering. Uninstall
(or disable) the plugin and every AI feature disappears again.

A shop that would rather use a hosted provider can opt in with its own
API key — see [Hosted provider (opt-in)](#hosted-provider-opt-in) below.

## Settings (Plugins → AI Assistant → Settings)

| key | default | meaning |
|---|---|---|
| `provider` | `self_hosted` | `self_hosted` (Ollama, the default), `claude` or `openai` (both hosted, need `api_key`). Any other value behaves as `self_hosted`. |
| `endpoint` | `http://localhost:11434` | your Ollama server URL (self-hosted only). Must be an `http://` or `https://` address; the plugin can connect to this address and no other on your network. Plain `http://` works only for this machine or your local network — a public address needs `https://`. |
| `vision_model` | `llama3.2-vision` | model for camera identify — with a hosted `provider` this names that provider's model instead (blank = `claude-haiku-4-5` for `claude`, `gpt-4o-mini` for `openai`) |
| `ask_model` | `llama3.2` | tool-capable model for Ask your till — self-hosted and `openai`; `claude` has no ask loop yet so this setting has no effect there (blank = `gpt-4o-mini` for `openai`) |
| `api_key` | *(empty)* | your own API key for the hosted provider. Stored masked and encrypted at rest; only used when `provider = claude` or `provider = openai`. |
| `image_provider` | `self_hosted` | provider for background removal. Only `self_hosted` (your own rembg service) exists today; **any other value switches background removal off** — never a hosted call. |
| `image_endpoint` | *(empty)* | your rembg server's base URL, e.g. `http://192.168.1.20:7000`. Empty = background removal off. Must be a plain `http`/`https` URL; the till checks it each time it calls the service and treats anything else as off. |
| `image_model` | `birefnet-general-lite` | the rembg model. Only `birefnet-general-lite` or `u2netp` are accepted; any other value switches background removal off and the settings page says so. |

On the Ollama machine: `ollama pull llama3.2-vision && ollama pull llama3.2`.

Settings changes apply on the next identify, question or background removal; nothing is cached.
Updating from 1.x keeps every value you saved.

## What the plugin may do (permissions)

The till asks you to grant these at install:

| permission | why |
|---|---|
| `events:receive` | answer the camera button and the Ask panel |
| `net:@setting:endpoint`, `http:lan` | reach your Ollama server at the `endpoint` address — on this till or on your shop network — and nothing else there |
| `net:api.openai.com`, `net:api.anthropic.com` | used only when you set `provider` to `openai` or `claude` |
| `view:inventory` | read item names, SKUs and reference photos for identify, and stock levels for Ask |
| `view:sales`, `view:audit` | read sales totals, best sellers, payment totals and till-activity counts for Ask (counts only — never customer data or audit details) |
| `ui:slot:reports.panels`, `ui:page` | draw the Ask your till panel on Reports |

Everything the plugin reads is read-only: it cannot change your catalog,
sales or settings. A suggestion only adds an item to the basket when the
cashier taps it.

Background removal needs no permission here: the till itself calls your
rembg service with the `image_*` settings below; the plugin's code never
sees the photo.

## Background removal

The item and category photo pickers can cut the product out of its
background and make a clean square tile (the picker button ships with a
coming till release, ut-docs#3127). It runs on **your own rembg
service** — a small HTTP server (`rembg s`, Docker image
`danielgatis/rembg`) on the same machine as Ollama or any machine the till
can reach. The photo goes to that service and nowhere else. It is
configured separately from camera identify and Ask your till: you can use
background removal with those off, and the other way round.

1. Run rembg, for example `docker run -d -p 7000:7000 danielgatis/rembg s --host 0.0.0.0 --port 7000`.
2. Set `image_endpoint` to its URL (`http://<that machine>:7000`).
3. Keep `image_model` on an allowed model:

| model | licence of the model weights | when to use |
|---|---|---|
| `birefnet-general-lite` (default) | BiRefNet — [MIT](https://github.com/ZhengPeng7/BiRefNet/blob/main/LICENSE) | best quality; needs a reasonable CPU or a GPU |
| `u2netp` | U²-Net — [Apache-2.0](https://github.com/xuebinqin/U-2-Net/blob/master/LICENSE) | small and fast, for weak CPUs |

> ⚠️ **rembg's own default model, `bria-rmbg`, needs a paid commercial
> licence from BRIA.** The till always tells rembg which model to use, so
> it never runs `bria-rmbg` by accident, and it **refuses** any `bria-*`
> or other unlisted model: background removal then stays off.

The till waits up to 90 seconds for an answer (CPU inference is slow) and
falls back to the plain photo if the service is unreachable or answers
something unusable. Nothing here is on the sale path.

## Hosted provider (opt-in)

Set `provider` to `claude` or `openai` and enter your **own** API key for
that vendor in `api_key`. This is a shop-chosen, shop-paid alternative:

- **What leaves the shop.** With a hosted provider, what the AI features
  work on is sent to that provider's servers (Anthropic or OpenAI, both
  United States): for camera identify, the product photo plus your
  catalog's item names, SKUs and reference photos. With `provider =
  claude`, that's the only thing sent — Ask your till has no Claude
  implementation yet (see below). With `provider = openai`, Ask your till
  is also sent to OpenAI: your questions together with the sales and stock
  figures used to answer them. The settings page shows this notice above
  the key field. Self-hosted keeps all of it on your own hardware.
- **Your account, your cost.** The key is your own account with that
  provider; you pay them directly. Universal Till holds no account with
  any AI vendor and nothing in the product depends on one being set.
- **Fail-safe.** Only the exact values `claude` or `openai` select a
  hosted provider. A typo, a different spelling or any other value falls
  back to self-hosted — never forward to a paid API. `claude`/`openai`
  with no key leaves AI unconfigured (not silently Ollama).
- **Today's coverage.** `provider = openai` runs both camera identify and
  Ask your till. `provider = claude` runs camera identify only — Ask your
  till has no Claude implementation yet, so its panel hides itself while
  `provider = claude`.
- **Switching back.** Set `provider` to `self_hosted` (or clear it); the
  Ollama settings above apply again immediately.

## How it works

Since 2.0.0 the plugin carries the AI engine itself: a WebAssembly module
(`runtime: "wasm"`, written in Go on the official guest SDK
`github.com/universaltill/universal-till/sdk/plugin`) that the till runs
in its sandbox. Before 2.0.0 the plugin shipped no code and only
configured the till's built-in engine.

- **Identify by camera.** The till owns the camera button and the overlay.
  It sends the photo to the plugin as a background job
  (`catalog.identify`); the plugin reads the active catalog
  (`catalog.items.v1`), with a hosted provider also up to 60 reference
  photos, asks the model for at most three matches, and answers them as
  suggestions. Tapping one adds the item exactly as if it had been
  scanned, and the till keeps the photo as that item's newest reference
  photo for next time. The sale never waits on it: barcode scan and search
  stay the primary path. The till shows "Identification failed — scan or
  search instead" when the model can't be reached.
- **Ask your till.** A panel on Reports. Asking starts a background job
  that runs a tool loop: the model may call five read-only tools (daily
  sales, best sellers, payment breakdown, stock levels, till-activity
  counts), each answered by one of the till's read views, for at most six
  rounds and two minutes. The answer appears under the question.
- **While the till still has its own engine** (until ut-docs#2851 removes
  it), the plugin's camera button replaces the built-in one, and Reports
  also shows the till's own Ask card next to this plugin's panel.

Known limits, compared with the till's built-in engine:

- **Each model call must answer within 30 seconds.** The till cuts off a
  plugin's web request whose reply hasn't started after 30 s
  (ut-docs#4035), and Ollama replies only when it has finished. A slow
  vision model on a small machine therefore shows "Identification
  failed" where the built-in engine waited up to 90 s. Until that's fixed,
  use a smaller/faster model, or the till's `UT_AI_*` settings.
- When Ask isn't available (provider `claude`, or AI not set up), the
  panel is hidden by not answering; the till logs one warning line per
  Reports visit for that.

- The assistant learns the shop's name and currency from the till's
  `shop.context.v1` view. On an older till without that view it falls back
  to "this shop" and amounts in minor units with two decimal places, which
  is wrong for a zero-decimal currency such as JPY.
- Reference photos come from the first 64 catalog items the till lets the
  plugin open per identify (the built-in engine looked through the whole
  catalog for 60 photos).
- The till's audit log no longer gets an `ai_identify` / `ai_ask` row per
  use; it still records `ai_identify_confirmed` when a cashier picks a
  suggestion.

## Development

```sh
go test ./...                 # engine + handlers on plugin.FakeHost
scripts/build.sh              # GOOS=wasip1 GOARCH=wasm → bin/plugin.wasm
scripts/validate.sh           # manifest sanity (needs the built module)
scripts/guard-plugin-i18n.sh  # locale bundle drift
scripts/package.sh            # dist/<id>_<version>_universal.tar.gz
```

`src/ai` is the engine (Ollama, OpenAI and Claude providers, ported from
the till's `internal/ai`); `src/app` wires it to the till's events, views
and settings; `src/main.go` is `plugin.Run`. User-facing text lives in
`locales/*.json` (`en` is the base). Release = tag `v<version>` matching
`manifest.json`, cut automatically on merge when the version moved.
