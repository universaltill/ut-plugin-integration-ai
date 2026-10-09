package app

import (
	"strings"
	"unicode/utf8"
)

// View-document builders (ut-docs reference/plugin-views.md, v1). Every
// TEXT is a key from this plugin's own locales/*.json or a literal the
// till escapes.

// maxLiteralBytes keeps every string under the view format's 4 KiB limit.
const maxLiteralBytes = 4000

type component = map[string]any

func key(k string) map[string]any { return map[string]any{"key": k} }

func literal(s string) map[string]any { return map[string]any{"literal": s} }

func document(components ...component) map[string]any {
	if components == nil {
		components = []component{}
	}
	return map[string]any{"document": map[string]any{"version": 1, "components": components}}
}

func text(t map[string]any) component { return component{"type": "text", "text": t} }

func notice(level, k string) component {
	return component{"type": "notice", "level": level, "text": key(k)}
}

// truncate cuts s to at most n bytes at a rune boundary.
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	cut := n
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut]
}

// chunks splits s into pieces of at most n bytes, preferring a line break,
// never splitting a rune. Joined, the pieces are s.
func chunks(s string, n int) []string {
	var out []string
	for len(s) > n {
		cut := truncate(s, n)
		if i := strings.LastIndexByte(cut, '\n'); i > 0 {
			cut = cut[:i+1]
		}
		out = append(out, cut)
		s = s[len(cut):]
	}
	if s != "" {
		out = append(out, s)
	}
	return out
}
