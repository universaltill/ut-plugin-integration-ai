# AI Assistant — self-hosted (`com.universaltill.integration-ai`)

Installing this plugin **switches on** Universal Till's AI features:

- 📷 **Camera identify** on the sale screen (barcode failed? photograph
  the product),
- 💬 **Ask your till** on the Reports page (plain-language questions
  about sales and stock).

Both run against **your own [Ollama](https://ollama.com) server** — on
the till, a machine in the store, or your homelab. Nothing leaves your
infrastructure, there is no account and no metering. Uninstall (or
disable) the plugin and every AI feature disappears again.

## Settings (Plugins → AI Assistant → Settings)

| key | default | meaning |
|---|---|---|
| `endpoint` | `http://localhost:11434` | your Ollama server URL |
| `vision_model` | `llama3.2-vision` | model for camera identify |
| `ask_model` | `llama3.2` | tool-capable model for Ask your till |

On the Ollama machine: `ollama pull llama3.2-vision && ollama pull llama3.2`.

## How it works

`runtime: "none"` — this plugin ships no code. The till's built-in AI
engine reads the plugin's presence and settings (docs repo:
`architecture/ai-plugin.md`); `UT_AI_*` environment variables remain the
low-level developer override.
