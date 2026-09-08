# AI Assistant (`com.universaltill.integration-ai`)

Installing this plugin **switches on** Universal Till's AI features:

- 📷 **Camera identify** on the sale screen (barcode failed? photograph
  the product),
- 💬 **Ask your till** on the Reports page (plain-language questions
  about sales and stock).

By default both run against **your own [Ollama](https://ollama.com)
server** — on the till, a machine in the store, or your homelab. Nothing
leaves your infrastructure, there is no account and no metering. Uninstall
(or disable) the plugin and every AI feature disappears again.

A shop that would rather use a hosted provider can opt in with its own
API key — see [Hosted provider (opt-in)](#hosted-provider-opt-in) below.

## Settings (Plugins → AI Assistant → Settings)

| key | default | meaning |
|---|---|---|
| `provider` | `self_hosted` | `self_hosted` (Ollama, the default), `claude` or `openai` (both hosted, need `api_key`). Any other value behaves as `self_hosted`. |
| `endpoint` | `http://localhost:11434` | your Ollama server URL (self-hosted only) |
| `vision_model` | `llama3.2-vision` | model for camera identify — with a hosted `provider` this names that provider's model instead (blank = the host's default: `claude-haiku-4-5` for `claude`, `gpt-4o-mini` for `openai`) |
| `ask_model` | `llama3.2` | tool-capable model for Ask your till — self-hosted, and (as of this version) `openai` too; `claude` has no ask loop yet so this setting has no effect there (blank = `gpt-4o-mini` for `openai`) |
| `api_key` | *(empty)* | your own API key for the hosted provider. Stored masked and encrypted at rest; only used when `provider = claude` or `provider = openai`. |

On the Ollama machine: `ollama pull llama3.2-vision && ollama pull llama3.2`.

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
  with no key leaves AI disabled (not silently Ollama).
- **Today's coverage.** `provider = openai` runs both camera identify and
  Ask your till. `provider = claude` runs camera identify only — Ask your
  till has no Claude implementation yet, so it hides itself instead of
  erroring while `provider = claude`.
- **Switching back.** Set `provider` to `self_hosted` (or clear it); the
  Ollama settings above apply again immediately.

## How it works

`runtime: "none"` — this plugin ships no code. The till's built-in AI
engine reads the plugin's presence and settings (docs repo:
`architecture/ai-plugin.md`, ADR-0085 for the hosted opt-in); `UT_AI_*`
environment variables remain the low-level developer override.
