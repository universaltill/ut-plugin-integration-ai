//go:build !wasip1

package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"

	"github.com/universaltill/universal-till/sdk/plugin"
)

// newHost is a FakeHost standing in for one event on a till that installed
// this plugin: its own id (event_publish namespace), its declared secret
// setting, and the given settings.
func newHost(t *testing.T, settings map[string]string) *plugin.FakeHost {
	t.Helper()
	h := plugin.NewFakeHost()
	h.PluginID = PluginID
	h.SecretSettings["api_key"] = true
	for k, v := range settings {
		h.Settings[k] = v
	}
	plugin.UseFakeHost(t, h)
	return h
}

// dispatch runs one event through the plugin's real Handlers, exactly as
// plugin.Run does on the till, and decodes the answer ("" = no answer).
func dispatch(t *testing.T, eventType string, payload any) (map[string]any, error) {
	t.Helper()
	p, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(map[string]any{"id": "evt1", "type": eventType, "timestamp": "2026-10-09T12:00:00Z", "payload": json.RawMessage(p)})
	out, err := plugin.Dispatch(Handlers(), raw, "")
	if len(out) == 0 {
		return nil, err
	}
	var m map[string]any
	if jerr := json.Unmarshal(out, &m); jerr != nil {
		t.Fatalf("answer is not JSON: %v: %s", jerr, out)
	}
	return m, err
}

// repoRoot is this repo's root (src/app/../..).
func repoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("no caller")
	}
	return filepath.Join(filepath.Dir(file), "..", "..")
}

// enKeys is the plugin's own en.json bundle.
func enKeys(t *testing.T) map[string]string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(repoRoot(t), "locales", "en.json"))
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]string
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	return m
}

// assertOwnKeys fails when the answer names a TEXT key that is not in the
// plugin's own bundle: the till refuses the whole document for one.
func assertOwnKeys(t *testing.T, answer any) {
	t.Helper()
	own := enKeys(t)
	var walk func(v any)
	walk = func(v any) {
		switch x := v.(type) {
		case map[string]any:
			if k, ok := x["key"].(string); ok {
				if _, found := own[k]; !found {
					t.Errorf("text key %q is not in locales/en.json", k)
				}
			}
			for _, c := range x {
				walk(c)
			}
		case []any:
			for _, c := range x {
				walk(c)
			}
		}
	}
	walk(answer)
}

// components returns the answer document's components.
func components(t *testing.T, answer map[string]any) []map[string]any {
	t.Helper()
	doc, ok := answer["document"].(map[string]any)
	if !ok {
		t.Fatalf("answer has no document: %v", answer)
	}
	if doc["version"] != float64(1) {
		t.Fatalf("document version = %v", doc["version"])
	}
	var out []map[string]any
	for _, c := range doc["components"].([]any) {
		out = append(out, c.(map[string]any))
	}
	return out
}

// ofType filters components by type.
func ofType(cs []map[string]any, typ string) []map[string]any {
	var out []map[string]any
	for _, c := range cs {
		if c["type"] == typ {
			out = append(out, c)
		}
	}
	return out
}

// textKeys lists every {"key": …} under text/notice components, in order.
func textKeys(cs []map[string]any) []string {
	var keys []string
	for _, c := range cs {
		if t, ok := c["text"].(map[string]any); ok {
			if k, ok := t["key"].(string); ok {
				keys = append(keys, k)
			}
		}
	}
	return keys
}

func literals(cs []map[string]any) string {
	var b strings.Builder
	for _, c := range cs {
		if t, ok := c["text"].(map[string]any); ok {
			if l, ok := t["literal"].(string); ok {
				b.WriteString(l)
			}
		}
	}
	return b.String()
}

func sortedKeys(m map[string]int) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

type manifestEntry struct {
	Type  string `json:"type"`
	Key   string `json:"key"`
	Label string `json:"label"`
	Route string `json:"route"`
	View  string `json:"view"`
	Slot  string `json:"slot"`
}

type manifest struct {
	ID          string          `json:"id"`
	Permissions []string        `json:"permissions"`
	ViewsUsed   []string        `json:"views_used"`
	Entries     []manifestEntry `json:"entries"`
	Hooks       []struct {
		Event string `json:"event"`
	} `json:"hooks"`
}

func loadManifest(t *testing.T) manifest {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(repoRoot(t), "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	var m manifest
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	return m
}
