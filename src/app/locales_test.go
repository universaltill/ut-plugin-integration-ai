//go:build !wasip1

package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// packLanguages are the language packs that ship with the till
// (ut-plugin-language-*). The till loads this plugin's locales/<code>.json
// as an overlay for that UI language (core's syncLocales), so a pack
// language without a file shows the Ask panel and the identify notices in
// English (ut-docs#4123).
var packLanguages = []string{"de", "es", "pt"}

// brandOnly are values that legitimately read the same in every language.
var brandOnly = map[string]bool{
	"integration_ai.settings.provider.openai": true, // "ChatGPT (OpenAI)"
	"integration_ai.settings.provider.claude": true, // "Claude (Anthropic)"
}

func TestPackLanguageBundlesAreComplete(t *testing.T) {
	en := enKeys(t)
	for _, lang := range packLanguages {
		t.Run(lang, func(t *testing.T) {
			raw, err := os.ReadFile(filepath.Join(repoRoot(t), "locales", lang+".json"))
			if err != nil {
				t.Fatalf("locales/%s.json: %v", lang, err)
			}
			var got map[string]string
			if err := json.Unmarshal(raw, &got); err != nil {
				t.Fatal(err)
			}
			for k, english := range en {
				v, ok := got[k]
				switch {
				case !ok || v == "":
					t.Errorf("%s: key %q is missing", lang, k)
				case v == english && !brandOnly[k]:
					t.Errorf("%s: key %q is still the English text", lang, k)
				}
			}
			for k := range got {
				if _, ok := en[k]; !ok {
					t.Errorf("%s: key %q is not in en.json", lang, k)
				}
			}
			const example = "http://192.168.1.20:11434"
			if v := got["integration_ai.settings.endpoint.help"]; v != "" && !strings.Contains(v, example) {
				t.Errorf("%s: endpoint.help lost the example address %s", lang, example)
			}
		})
	}
}
