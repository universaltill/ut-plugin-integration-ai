//go:build !wasip1

package app

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/universaltill/universal-till/sdk/plugin"
)

// ut-docs#4128: catalog tools for the Ask panel. Reads go through the
// catalog.gaps.v1 / catalog.items.v1 core views; the one write tool only
// PROPOSES (action_propose), and the panel shows core's confirm card.

func toolByName(t *testing.T, c *collector, name string) func(map[string]any) (any, error) {
	t.Helper()
	for _, tl := range askToolsWith(c) {
		if tl.Name == name {
			return tl.Run
		}
	}
	t.Fatalf("no tool %q", name)
	return nil
}

func TestListItemsWithGaps_QueriesTheViewAndRecords_4128(t *testing.T) {
	h := newHost(t, ollamaSettings())
	var got []string
	h.Views = func(name string, args json.RawMessage) (json.RawMessage, error) {
		got = append(got, name+string(args))
		return json.RawMessage(`[{"item_id":"a","sku":"S1","name":"Tea","category":"","price_minor":100,"has_barcode":false,"has_image":true},` +
			`{"item_id":"b","sku":"","name":"Cup","category":"Ware","price_minor":0,"has_barcode":false,"has_image":false},` +
			`{"item_id":"c","sku":"","name":"Jug","category":"Ware","price_minor":5,"has_barcode":false,"has_image":false}]`), nil
	}
	c := &collector{}
	run := toolByName(t, c, "list_items_with_gaps")
	out, err := run(map[string]any{"missing": []any{"barcode", "image"}, "limit": float64(2)})
	if err != nil {
		t.Fatal(err)
	}
	// limit 2 + 1 probe row; mask barcode|image = 3.
	if want := `catalog.gaps.v1{"limit":3,"missing":3,"offset":0}`; len(got) != 1 || got[0] != want {
		t.Fatalf("view calls = %v, want %s", got, want)
	}
	raw, _ := json.Marshal(out)
	var res struct {
		Items   []map[string]any `json:"items"`
		HasMore bool             `json:"has_more"`
	}
	if err := json.Unmarshal(raw, &res); err != nil || len(res.Items) != 2 || !res.HasMore {
		t.Fatalf("result = %s (%v)", raw, err)
	}
	if strings.Join(c.ids, ",") != "a,b" || c.mask != 3 {
		t.Fatalf("collector = %+v, want ids a,b mask 3 (the shown rows only)", c)
	}
}

func TestListItemsWithGaps_BoundsAndBadInput_4128(t *testing.T) {
	h := newHost(t, ollamaSettings())
	var got []string
	h.Views = func(name string, args json.RawMessage) (json.RawMessage, error) {
		got = append(got, string(args))
		return json.RawMessage(`[]`), nil
	}
	run := toolByName(t, &collector{}, "list_items_with_gaps")
	for name, args := range map[string]map[string]any{
		"none":        {},
		"empty":       {"missing": []any{}},
		"unknown gap": {"missing": []any{"colour"}},
		"not a list":  {"missing": "barcode"},
	} {
		if _, err := run(args); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	if len(got) != 0 {
		t.Fatalf("a bad call reached the view: %v", got)
	}
	// An oversized limit falls back to the default instead of reaching the
	// strict view.
	if _, err := run(map[string]any{"missing": []any{"price"}, "limit": float64(9999), "offset": float64(-4)}); err != nil {
		t.Fatal(err)
	}
	if want := `{"limit":11,"missing":4,"offset":0}`; len(got) != 1 || got[0] != want {
		t.Fatalf("view args = %v, want %s", got, want)
	}
}

func TestFindItems_FiltersTheCatalogView_4128(t *testing.T) {
	h := newHost(t, ollamaSettings())
	h.Views = func(name string, args json.RawMessage) (json.RawMessage, error) {
		if name != "catalog.items.v1" {
			t.Errorf("view = %s", name)
		}
		var a map[string]int
		_ = json.Unmarshal(args, &a)
		if a["offset"] > 0 {
			return json.RawMessage(`[]`), nil
		}
		return json.RawMessage(`[{"id":"1","sku":"OL-1","name":"Oat latte","category_id":"c1","category":"Drinks","price_minor":350,"unit":"each","weighed":false,"active":true},` +
			`{"id":"2","sku":"BR-1","name":"Brownie","category_id":"c2","category":"Cake","price_minor":300,"unit":"each","weighed":false,"active":true},` +
			`{"id":"3","sku":"X","name":"Old latte","category_id":"c1","category":"Drinks","price_minor":1,"unit":"each","weighed":false,"active":false}]`), nil
	}
	c := &collector{}
	out, err := toolByName(t, c, "find_items")(map[string]any{"query": "LATTE"})
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(out)
	if !strings.Contains(string(raw), `"id":"1"`) || strings.Contains(string(raw), `"id":"2"`) || strings.Contains(string(raw), `"id":"3"`) {
		t.Fatalf("result = %s, want only the active matching item", raw)
	}
	if !strings.Contains(string(raw), `"category_id":"c1"`) {
		t.Fatalf("category id missing (the model needs it to recategorise): %s", raw)
	}
	if strings.Join(c.ids, ",") != "1" {
		t.Fatalf("collector ids = %v", c.ids)
	}
	if _, err := toolByName(t, c, "find_items")(map[string]any{}); err == nil {
		t.Error("find_items without a query or category accepted")
	}
}

func TestProposeItemChanges_OnlyProposes_4128(t *testing.T) {
	h := newHost(t, ollamaSettings())
	var name string
	var args json.RawMessage
	h.Propose = func(n string, a json.RawMessage) (string, error) {
		name, args = n, a
		return strings.Repeat("ab", 16), nil
	}
	c := &collector{}
	run := toolByName(t, c, "propose_item_changes")
	out, err := run(map[string]any{"changes": []any{
		map[string]any{"item_id": "a", "name": "Green tea", "price_minor": float64(250), "barcode": "123", "category_id": "c1"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if name != "catalog.item.update.v1" || !strings.Contains(string(args), `"price_minor":250`) {
		t.Fatalf("proposed %s %s", name, args)
	}
	if len(c.proposals) != 1 || c.proposals[0] != strings.Repeat("ab", 16) {
		t.Fatalf("proposals = %v", c.proposals)
	}
	msg := fmt.Sprint(out)
	if !strings.Contains(msg, "nothing has changed") {
		t.Fatalf("the model must be told nothing changed yet: %s", msg)
	}
}

func TestProposeItemChanges_RefusalsAndBounds_4128(t *testing.T) {
	h := newHost(t, ollamaSettings())
	calls := 0
	h.Propose = func(string, json.RawMessage) (string, error) {
		calls++
		return strings.Repeat("cd", 16), nil
	}
	run := toolByName(t, &collector{}, "propose_item_changes")
	bad := map[string]map[string]any{
		"no changes":   {"changes": []any{}},
		"no item":      {"changes": []any{map[string]any{"name": "x"}}},
		"nothing set":  {"changes": []any{map[string]any{"item_id": "a"}}},
		"unknown key":  {"changes": []any{map[string]any{"item_id": "a", "sku": "S"}}},
		"float price":  {"changes": []any{map[string]any{"item_id": "a", "price_minor": 2.5}}},
		"too many":     {"changes": manyChanges(51)},
		"not an array": {"changes": "x"},
	}
	for name, a := range bad {
		if _, err := run(a); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	if calls != 0 {
		t.Fatalf("%d invalid calls reached core", calls)
	}
	// Core refusing (role, grant, ceiling) is an error result for the model,
	// never a crash.
	h.Propose = nil // action_propose answers ErrDenied
	if _, err := run(map[string]any{"changes": []any{map[string]any{"item_id": "a", "name": "x"}}}); err == nil {
		t.Error("a denied proposal looked like success")
	}
	// At most 3 open proposals per turn (core's ceiling), refused locally too.
	h.Propose = func(string, json.RawMessage) (string, error) { return strings.Repeat("ef", 16), nil }
	c := &collector{}
	run = toolByName(t, c, "propose_item_changes")
	for i := 0; i < 3; i++ {
		if _, err := run(map[string]any{"changes": []any{map[string]any{"item_id": "a", "name": "x"}}}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := run(map[string]any{"changes": []any{map[string]any{"item_id": "a", "name": "x"}}}); err == nil {
		t.Error("a 4th proposal in one turn accepted")
	}
}

func manyChanges(n int) []any {
	out := make([]any, n)
	for i := range out {
		out[i] = map[string]any{"item_id": fmt.Sprint("i", i), "name": "x"}
	}
	return out
}

// The job's answer carries core-drawn item_list and confirm components for
// what the tools found and proposed -- ids only, never a word or URL of
// the model's (ut-docs#4128).
func TestAskJob_AnswerCarriesItemListAndConfirm_4128(t *testing.T) {
	h := newHost(t, ollamaSettings())
	h.InJob = true
	proposal := strings.Repeat("0a", 16)
	h.Views = func(name string, args json.RawMessage) (json.RawMessage, error) {
		switch name {
		case "shop.context.v1":
			return json.RawMessage(`[{"store_name":"S","currency_code":"GBP","currency_decimals":2,"locale":"en"}]`), nil
		case "catalog.gaps.v1":
			return json.RawMessage(`[{"item_id":"a","sku":"","name":"Tea","category":"","price_minor":100,"has_barcode":false,"has_image":false}]`), nil
		}
		return json.RawMessage(`[]`), nil
	}
	h.Propose = func(string, json.RawMessage) (string, error) { return proposal, nil }
	round := 0
	h.HTTP = func(req plugin.HTTPRequest) (plugin.HTTPResponse, error) {
		round++
		switch round {
		case 1:
			return plugin.HTTPResponse{Status: 200, Body: []byte(`{"message":{"role":"assistant","content":"","tool_calls":[` +
				`{"function":{"name":"list_items_with_gaps","arguments":{"missing":["barcode"]}}},` +
				`{"function":{"name":"propose_item_changes","arguments":{"changes":[{"item_id":"a","barcode":"4006381333931"}]}}}]}}`)}, nil
		}
		return plugin.HTTPResponse{Status: 200, Body: []byte(`{"message":{"role":"assistant","content":"One item has no barcode."}}`)}, nil
	}
	answer, err := dispatch(t, AskJobEvent, jobPayload("Which items have no barcode?"))
	if err != nil {
		t.Fatal(err)
	}
	assertOwnKeys(t, answer)
	cs := components(t, answer)
	lists := ofType(cs, "item_list")
	if len(lists) != 1 || fmt.Sprint(lists[0]["items"]) != "[a]" || lists[0]["missing"] != float64(1) || len(lists[0]) != 3 {
		t.Fatalf("item_list = %v", lists)
	}
	confirms := ofType(cs, "confirm")
	if len(confirms) != 1 || confirms[0]["proposal"] != proposal || len(confirms[0]) != 2 {
		t.Fatalf("confirm = %v", confirms)
	}
}

// A list that mixes find_items hits with gap results gets no catalog
// filter link: ?missing=N would not show the found items (ut-docs#4128).
func TestCollector_MixedListHasNoFilterLink_4128(t *testing.T) {
	c := &collector{}
	c.addItems(1, "a")
	if got := c.components()[0]["missing"]; got != 1 {
		t.Fatalf("gap-only list missing = %v, want 1", got)
	}
	c.addItems(0, "b")
	if _, has := c.components()[0]["missing"]; has {
		t.Fatal("a mixed list still names a catalog filter")
	}
}
