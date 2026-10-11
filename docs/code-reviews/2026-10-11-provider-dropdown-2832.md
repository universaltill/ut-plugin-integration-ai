# ut-docs#2832: provider dropdown, conditional fields, labels (2.4.0)

- `provider` is `type: "choice"` (self_hosted / openai / claude / openai_compatible) with translated labels. `endpoint` shows only for self_hosted/openai_compatible and `api_key` only for openai/claude/openai_compatible (`show_when`). Every setting has a `label` (+ `help`). A new `locales/de.json` holds all keys, in German terminology aligned with the de pack ("Fragen Sie Ihre Kasse", "Per Kamera erkennen").
- Stored values, keys and defaults are unchanged. The fail-safe is unchanged: an older non-option value runs as self-hosted and stays selectable in core.
- `validate.sh` enforces the choice/options/show_when/labels and a `min_pos_version` that understands `choice`. CLAUDE.md drops the "provider stays plain text" rule (owner 2026-10-11).

Reviewed with the core change (Fable, independent); the full record is universal-till `docs/code-reviews/2026-10-11-plugin-settings-choice-fields-2832.md`. A real driven run used this exact manifest + locales on a throwaway till.

Gates: gofmt, go vet (native + wasip1), go test, build.sh, validate.sh, guard-plugin-i18n.sh, shellcheck, check-version-bump.test.sh.
