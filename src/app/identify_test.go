//go:build !wasip1

package app

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/universaltill/universal-till/sdk/plugin"
)

// The catalog.identify job (ut-docs#4032): core's /api/pos/identify flow
// (internal/pages/ai_api.go) moved behind the till's camera-identify seam.

const photoToken = "0123456789abcdef0123456789abcdef"

var (
	jpegPhoto = append([]byte{0xFF, 0xD8, 0xFF, 0xE0}, bytes.Repeat([]byte{7}, 64)...)
	pngPhoto  = append([]byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1A, '\n'}, bytes.Repeat([]byte{7}, 64)...)
)

func photoPayload() map[string]any {
	return map[string]any{
		"upload_handles": []map[string]any{{"field": "photo", "handle": photoToken, "filename": "capture.jpg", "size": len(jpegPhoto), "content_type": "image/jpeg"}},
		"locale":         "en",
		"job_id":         "9f0c0000000000000000000000000000",
	}
}

type catalogRow struct {
	ID     string `json:"id"`
	SKU    string `json:"sku"`
	Name   string `json:"name"`
	Active bool   `json:"active"`
}

// catalogViews answers catalog.items.v1 pages from rows, recording each
// call's args. catalog.photos.v1 is unknown (-1), as on a till before
// ut-docs#4094; photoViews adds it.
func catalogViews(t *testing.T, rows []catalogRow, calls *[]map[string]int) func(string, json.RawMessage) (json.RawMessage, error) {
	return func(name string, args json.RawMessage) (json.RawMessage, error) {
		if name == photosView {
			return nil, plugin.ErrNotFound
		}
		if name != "catalog.items.v1" {
			t.Errorf("identify queried view %q", name)
			return nil, plugin.ErrNotFound
		}
		var a map[string]int
		if err := json.Unmarshal(args, &a); err != nil {
			t.Fatalf("view args %s: %v", args, err)
		}
		if calls != nil {
			*calls = append(*calls, a)
		}
		off, lim := a["offset"], a["limit"]
		if off > len(rows) {
			off = len(rows)
		}
		end := min(off+lim, len(rows))
		return json.Marshal(rows[off:end])
	}
}

// ollamaAnswering is an Ollama /api/chat that answers identify with res.
func ollamaAnswering(t *testing.T, res map[string]any, gotPrompt *string) func(plugin.HTTPRequest) (plugin.HTTPResponse, error) {
	return func(req plugin.HTTPRequest) (plugin.HTTPResponse, error) {
		if !strings.HasSuffix(req.URL, "/api/chat") {
			t.Errorf("url = %s", req.URL)
		}
		var body struct {
			Messages []struct {
				Content string `json:"content"`
			} `json:"messages"`
		}
		_ = json.Unmarshal(req.Body, &body)
		if gotPrompt != nil && len(body.Messages) > 0 {
			*gotPrompt = body.Messages[0].Content
		}
		content, _ := json.Marshal(res)
		b, _ := json.Marshal(map[string]any{"message": map[string]any{"role": "assistant", "content": string(content)}})
		return plugin.HTTPResponse{Status: 200, Body: b}, nil
	}
}

func ollamaSettings() map[string]string {
	return map[string]string{"provider": "self_hosted", "endpoint": "http://localhost:11434"}
}

func TestIdentify_SuggestsMatchesAsAddToBasket(t *testing.T) {
	h := newHost(t, ollamaSettings())
	h.InJob = true
	h.Uploads[photoToken] = jpegPhoto
	h.Views = catalogViews(t, []catalogRow{
		{ID: "itm1", SKU: "MILK", Name: "Milk 1L", Active: true},
		{ID: "itm2", SKU: "OAT", Name: "Oat <Drink>", Active: true},
		{ID: "itm3", SKU: "OLD", Name: "Discontinued", Active: false},
	}, nil)
	var prompt string
	h.HTTP = ollamaAnswering(t, map[string]any{
		"matches":        []map[string]any{{"item_id": "itm2", "confidence": "high"}, {"item_id": "itm1", "confidence": "low"}, {"item_id": "itm3", "confidence": "medium"}},
		"suggested_name": "",
	}, &prompt)

	answer, err := dispatch(t, "catalog.identify", photoPayload())
	if err != nil {
		t.Fatal(err)
	}
	assertOwnKeys(t, answer)
	cs := components(t, answer)
	sugg := ofType(cs, "suggestions")
	if len(sugg) != 1 {
		t.Fatalf("want one suggestions component, got %v", cs)
	}
	items := sugg[0]["items"].([]any)
	// itm3 is inactive: core's catalog is SearchActiveItems, so the model
	// never sees it and the hallucination filter drops it.
	if len(items) != 2 {
		t.Fatalf("items = %v", items)
	}
	first := items[0].(map[string]any)
	if first["label"].(map[string]any)["literal"] != "Oat <Drink>" {
		t.Fatalf("label = %v", first["label"])
	}
	if first["detail"].(map[string]any)["key"] != "integration_ai.identify.confidence.high" {
		t.Fatalf("detail = %v", first["detail"])
	}
	effect := first["effect"].(map[string]any)["add_to_basket"].(map[string]any)
	if effect["sku"] != "OAT" || effect["qty"] != float64(1) {
		t.Fatalf("effect = %v", effect)
	}
	if items[1].(map[string]any)["detail"].(map[string]any)["key"] != "integration_ai.identify.confidence.low" {
		t.Fatalf("second detail = %v", items[1])
	}
	if len(ofType(cs, "notice")) != 0 {
		t.Fatalf("no notice expected with matches: %v", cs)
	}
	if strings.Contains(prompt, "itm3") || !strings.Contains(prompt, `{"id":"itm1","sku":"MILK","name":"Milk 1L"}`) {
		t.Fatalf("catalog in prompt wrong: %s", prompt)
	}
	// Ollama gets no reference photos, so none are opened.
	if h.Calls["item_image_open"] != 0 {
		t.Fatalf("item_image_open called %d times for ollama", h.Calls["item_image_open"])
	}
	if h.Calls["upload_close"] != 1 {
		t.Fatalf("upload not closed: %v", h.Calls)
	}
	if len(h.Progress) == 0 {
		t.Fatal("no job progress reported")
	}
	own := enKeys(t)
	for _, p := range h.Progress {
		if _, ok := own[p.Key]; !ok {
			t.Errorf("progress key %q not in the bundle", p.Key)
		}
	}
}

func TestIdentify_NoMatchNoticeAndSuggestedName(t *testing.T) {
	h := newHost(t, ollamaSettings())
	h.Uploads[photoToken] = pngPhoto
	h.Views = catalogViews(t, []catalogRow{{ID: "itm1", SKU: "MILK", Name: "Milk", Active: true}}, nil)
	h.HTTP = ollamaAnswering(t, map[string]any{"matches": []any{}, "suggested_name": "Oat Milk"}, nil)

	answer, err := dispatch(t, "catalog.identify", photoPayload())
	if err != nil {
		t.Fatal(err)
	}
	assertOwnKeys(t, answer)
	cs := components(t, answer)
	if len(ofType(cs, "suggestions")) != 0 {
		t.Fatalf("no suggestions expected: %v", cs)
	}
	// No no-match notice of our own: the till's overlay already says "No
	// match found" for a document without suggestions (tester finding D1).
	keys := textKeys(cs)
	if len(keys) != 1 || keys[0] != "integration_ai.identify.suggested" {
		t.Fatalf("keys = %v", keys)
	}
	if literals(cs) != "Oat Milk" {
		t.Fatalf("suggested name literal = %q", literals(cs))
	}
	for _, c := range cs {
		if c["type"] != "notice" && c["type"] != "text" {
			t.Fatalf("seam allows only text/notice/suggestions, got %v", c["type"])
		}
	}
}

// A match whose item has no SKU cannot be added through the scan path —
// it is left out; with nothing left the till shows its own "No match found".
func TestIdentify_MatchWithoutSKUIsSkipped(t *testing.T) {
	h := newHost(t, ollamaSettings())
	h.Uploads[photoToken] = jpegPhoto
	h.Views = catalogViews(t, []catalogRow{{ID: "itm1", SKU: "", Name: "Loose apples", Active: true}}, nil)
	h.HTTP = ollamaAnswering(t, map[string]any{"matches": []map[string]any{{"item_id": "itm1", "confidence": "high"}}, "suggested_name": ""}, nil)
	answer, err := dispatch(t, "catalog.identify", photoPayload())
	if err != nil {
		t.Fatal(err)
	}
	cs := components(t, answer)
	if len(cs) != 0 {
		t.Fatalf("components = %v, want none", cs)
	}
}

func TestIdentify_NotConfiguredAnswersNoticeWithoutCalls(t *testing.T) {
	h := newHost(t, map[string]string{"provider": "claude"}) // no key
	h.Uploads[photoToken] = jpegPhoto
	answer, err := dispatch(t, "catalog.identify", photoPayload())
	if err != nil {
		t.Fatal(err)
	}
	assertOwnKeys(t, answer)
	cs := components(t, answer)
	if keys := textKeys(cs); len(keys) != 1 || keys[0] != "integration_ai.identify.not_configured" {
		t.Fatalf("keys = %v", keys)
	}
	if h.Calls["http_request"]+h.Calls["view_query"]+h.Calls["upload_open"] != 0 {
		t.Fatalf("unexpected host calls: %v", sortedKeys(h.Calls))
	}
}

func TestIdentify_BadPhotoAnswersNotice(t *testing.T) {
	cases := map[string]func(h *plugin.FakeHost) map[string]any{
		"gif": func(h *plugin.FakeHost) map[string]any {
			h.Uploads[photoToken] = []byte("GIF89a....")
			return photoPayload()
		},
		"webp": func(h *plugin.FakeHost) map[string]any {
			h.Uploads[photoToken] = []byte("RIFF\x00\x00\x00\x00WEBPVP8 ")
			return photoPayload()
		},
		"too large": func(h *plugin.FakeHost) map[string]any {
			h.Uploads[photoToken] = append(append([]byte{}, jpegPhoto...), make([]byte, maxPhotoBytes)...)
			return photoPayload()
		},
		"empty": func(h *plugin.FakeHost) map[string]any {
			h.Uploads[photoToken] = []byte{}
			return photoPayload()
		},
		"no handle": func(h *plugin.FakeHost) map[string]any {
			return map[string]any{"upload_handles": []any{}, "job_id": "x"}
		},
		"consumed token": func(h *plugin.FakeHost) map[string]any {
			return photoPayload() // nothing staged under the token
		},
	}
	for name, setup := range cases {
		t.Run(name, func(t *testing.T) {
			h := newHost(t, ollamaSettings())
			h.HTTP = func(plugin.HTTPRequest) (plugin.HTTPResponse, error) {
				t.Error("no model call for a bad photo")
				return plugin.HTTPResponse{}, nil
			}
			answer, err := dispatch(t, "catalog.identify", setup(h))
			if err != nil {
				t.Fatal(err)
			}
			if keys := textKeys(components(t, answer)); len(keys) != 1 || keys[0] != "integration_ai.identify.bad_photo" {
				t.Fatalf("keys = %v", keys)
			}
		})
	}
}

// A model or catalog failure fails the job: the till then shows its own
// "Identification failed — scan or search instead" (core's 502 path).
func TestIdentify_FailuresFailTheJob(t *testing.T) {
	t.Run("model", func(t *testing.T) {
		h := newHost(t, ollamaSettings())
		h.Uploads[photoToken] = jpegPhoto
		h.Views = catalogViews(t, nil, nil)
		h.HTTP = func(plugin.HTTPRequest) (plugin.HTTPResponse, error) {
			return plugin.HTTPResponse{Status: 500, Body: []byte("model not found")}, nil
		}
		if _, err := dispatch(t, "catalog.identify", photoPayload()); err == nil || !strings.Contains(err.Error(), "model not found") {
			t.Fatalf("want the model error, got %v", err)
		}
	})
	t.Run("catalog", func(t *testing.T) {
		h := newHost(t, ollamaSettings())
		h.Uploads[photoToken] = jpegPhoto
		h.Deny["view_query"] = true
		if _, err := dispatch(t, "catalog.identify", photoPayload()); !errors.Is(err, plugin.ErrDenied) {
			t.Fatalf("want the denied view error, got %v", err)
		}
	})
}

// Core read SearchActiveItems(…, 500): the plugin pages catalog.items.v1
// (inactive items included there) until it has 500 active items or the
// catalog ends.
func TestIdentify_CatalogPagesToFiveHundredActiveItems(t *testing.T) {
	var rows []catalogRow
	for i := range 1200 {
		rows = append(rows, catalogRow{ID: fmt.Sprintf("itm%04d", i), SKU: fmt.Sprintf("S%d", i), Name: fmt.Sprintf("Item %d", i), Active: i%2 == 0})
	}
	h := newHost(t, ollamaSettings())
	h.Uploads[photoToken] = jpegPhoto
	var calls []map[string]int
	h.Views = catalogViews(t, rows, &calls)
	var prompt string
	h.HTTP = ollamaAnswering(t, map[string]any{"matches": []any{}, "suggested_name": ""}, &prompt)
	if _, err := dispatch(t, "catalog.identify", photoPayload()); err != nil {
		t.Fatal(err)
	}
	if len(calls) != 2 || calls[0]["offset"] != 0 || calls[0]["limit"] != 500 || calls[1]["offset"] != 500 {
		t.Fatalf("view calls = %v", calls)
	}
	if n := strings.Count(prompt, `"sku":`); n != 500 {
		t.Fatalf("catalog items in prompt = %d, want 500", n)
	}
	if strings.Contains(prompt, "itm0001") {
		t.Fatal("inactive item in the prompt")
	}
}

func TestIdentify_ShortCatalogStopsAfterOnePage(t *testing.T) {
	h := newHost(t, ollamaSettings())
	h.Uploads[photoToken] = jpegPhoto
	var calls []map[string]int
	h.Views = catalogViews(t, []catalogRow{{ID: "a", SKU: "A", Name: "A", Active: true}}, &calls)
	h.HTTP = ollamaAnswering(t, map[string]any{"matches": []any{}, "suggested_name": ""}, nil)
	if _, err := dispatch(t, "catalog.identify", photoPayload()); err != nil {
		t.Fatal(err)
	}
	if len(calls) != 1 {
		t.Fatalf("view calls = %v", calls)
	}
}

// claude (and openai) send one reference photo per item — item_image_open
// role "ref", the same bytes core's loadReferenceImages sent — capped at 60,
// and never more than the host's 64 opens per event (failed opens count).
func TestIdentify_HostedProviderSendsReferencePhotos(t *testing.T) {
	var rows []catalogRow
	for i := range 100 {
		rows = append(rows, catalogRow{ID: fmt.Sprintf("itm%03d", i), SKU: fmt.Sprintf("S%d", i), Name: fmt.Sprintf("Item %03d", i), Active: true})
	}
	h := newHost(t, map[string]string{"provider": "claude", "api_key": "sk-ant"})
	h.Uploads[photoToken] = jpegPhoto
	h.Views = catalogViews(t, rows, nil)
	for i := range 100 {
		if i%25 == 3 { // a few items with no photo at all
			continue
		}
		h.ItemImages[fmt.Sprintf("itm%03d", i)] = plugin.ItemImageFiles{Thumb: []byte{byte(i)}}
	}
	var refBlocks int
	h.HTTP = func(req plugin.HTTPRequest) (plugin.HTTPResponse, error) {
		if req.URL != "https://api.anthropic.com/v1/messages" {
			t.Errorf("url = %s", req.URL)
		}
		var body struct {
			Messages []struct {
				Content []map[string]any `json:"content"`
			} `json:"messages"`
		}
		_ = json.Unmarshal(req.Body, &body)
		for _, b := range body.Messages[0].Content {
			if b["type"] == "image" {
				refBlocks++
				src := b["source"].(map[string]any)
				if src["media_type"] != "image/jpeg" {
					t.Errorf("media type = %v", src["media_type"])
				}
			}
		}
		refBlocks-- // the photo itself
		return plugin.HTTPResponse{Status: 200, Body: []byte(`{"content":[{"type":"text","text":"{\"matches\":[{\"item_id\":\"itm005\",\"confidence\":\"medium\"}],\"suggested_name\":\"\"}"}],"stop_reason":"end_turn"}`)}, nil
	}
	answer, err := dispatch(t, "catalog.identify", photoPayload())
	if err != nil {
		t.Fatal(err)
	}
	if refBlocks != maxReferenceImages {
		t.Fatalf("reference photos sent = %d, want %d", refBlocks, maxReferenceImages)
	}
	if h.Calls["item_image_open"] > 64 {
		t.Fatalf("item_image_open called %d times (host cap 64)", h.Calls["item_image_open"])
	}
	items := ofType(components(t, answer), "suggestions")[0]["items"].([]any)
	if items[0].(map[string]any)["effect"].(map[string]any)["add_to_basket"].(map[string]any)["sku"] != "S5" {
		t.Fatalf("items = %v", items)
	}
}

func TestIdentify_ReferencePhotoOpensStopAtHostCap(t *testing.T) {
	var rows []catalogRow
	for i := range 200 {
		rows = append(rows, catalogRow{ID: fmt.Sprintf("itm%03d", i), SKU: "S", Name: "N", Active: true})
	}
	h := newHost(t, map[string]string{"provider": "openai", "api_key": "sk"})
	h.Uploads[photoToken] = jpegPhoto
	h.Views = catalogViews(t, rows, nil) // no item has a photo
	h.HTTP = func(plugin.HTTPRequest) (plugin.HTTPResponse, error) {
		return plugin.HTTPResponse{Status: 200, Body: []byte(`{"choices":[{"message":{"role":"assistant","content":"{\"matches\":[],\"suggested_name\":\"\"}"}}]}`)}, nil
	}
	if _, err := dispatch(t, "catalog.identify", photoPayload()); err != nil {
		t.Fatal(err)
	}
	if got := h.Calls["item_image_open"]; got != maxImageOpens {
		t.Fatalf("item_image_open calls = %d, want %d", got, maxImageOpens)
	}
}

// Without view:inventory the till refuses item_image_open; identify still
// runs on the photo and catalog text.
func TestIdentify_ReferencePhotosDeniedStillIdentifies(t *testing.T) {
	h := newHost(t, map[string]string{"provider": "openai", "api_key": "sk"})
	h.Uploads[photoToken] = jpegPhoto
	h.Views = catalogViews(t, []catalogRow{{ID: "a", SKU: "A", Name: "A", Active: true}, {ID: "b", SKU: "B", Name: "B", Active: true}}, nil)
	h.ItemImages["a"] = plugin.ItemImageFiles{Thumb: []byte{1}}
	h.Deny["item_image_open"] = true
	h.HTTP = func(plugin.HTTPRequest) (plugin.HTTPResponse, error) {
		return plugin.HTTPResponse{Status: 200, Body: []byte(`{"choices":[{"message":{"role":"assistant","content":"{\"matches\":[{\"item_id\":\"a\",\"confidence\":\"high\"}],\"suggested_name\":\"\"}"}}]}`)}, nil
	}
	answer, err := dispatch(t, "catalog.identify", photoPayload())
	if err != nil {
		t.Fatal(err)
	}
	if len(ofType(components(t, answer), "suggestions")) != 1 {
		t.Fatal("identify must still answer")
	}
	if h.Calls["item_image_open"] != 1 {
		t.Fatalf("a denial must stop further opens, got %d", h.Calls["item_image_open"])
	}
}

// Long names and SKUs stay within the view format's limits: a label
// literal is at most 4 KiB, a SKU 1–128 bytes without edge spaces.
func TestIdentify_LabelsAndSKUsWithinFormatLimits(t *testing.T) {
	long := strings.Repeat("é", 3000) // 6000 bytes
	h := newHost(t, ollamaSettings())
	h.Uploads[photoToken] = jpegPhoto
	h.Views = catalogViews(t, []catalogRow{
		{ID: "a", SKU: "A1", Name: long, Active: true},
		{ID: "b", SKU: strings.Repeat("x", 129), Name: "Too long SKU", Active: true},
		{ID: "c", SKU: " C ", Name: "Spaced SKU", Active: true},
		{ID: "d", SKU: "D\u00851", Name: "C1 control in SKU", Active: true},
	}, nil)
	h.HTTP = ollamaAnswering(t, map[string]any{"matches": []map[string]any{{"item_id": "a", "confidence": "high"}, {"item_id": "b", "confidence": "high"}, {"item_id": "c", "confidence": "bogus"}, {"item_id": "d", "confidence": "high"}}, "suggested_name": ""}, nil)
	answer, err := dispatch(t, "catalog.identify", photoPayload())
	if err != nil {
		t.Fatal(err)
	}
	items := ofType(components(t, answer), "suggestions")[0]["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("only the item with a usable SKU may be suggested: %v", items)
	}
	label := items[0].(map[string]any)["label"].(map[string]any)["literal"].(string)
	if len(label) > maxLiteralBytes || !strings.HasPrefix(long, label) {
		t.Fatalf("label not cut at a rune boundary within %d bytes: %d", maxLiteralBytes, len(label))
	}
}

// catalog.identify.confirmed is acknowledged with no answer: the photo is
// already the item's newest ai_ref, which the next identify reads.
func TestIdentifyConfirmed_Acks(t *testing.T) {
	h := newHost(t, ollamaSettings())
	answer, err := dispatch(t, "catalog.identify.confirmed", map[string]any{"job_id": "x", "item_id": "itm1", "sku": "S", "stored": true})
	if err != nil || answer != nil {
		t.Fatalf("answer=%v err=%v", answer, err)
	}
	if len(h.Calls) != 0 {
		t.Fatalf("no host calls expected: %v", sortedKeys(h.Calls))
	}
}

type photoRow struct {
	ItemID  string `json:"item_id"`
	Source  string `json:"source"`
	PhotoAt int64  `json:"photo_at"`
}

// photoViews answers catalog.items.v1 from rows and catalog.photos.v1 from
// photos (already in the view's order), recording the photo view's args.
func photoViews(t *testing.T, rows []catalogRow, photos []photoRow, photoArgs *[]map[string]int) func(string, json.RawMessage) (json.RawMessage, error) {
	items := catalogViews(t, rows, nil)
	return func(name string, args json.RawMessage) (json.RawMessage, error) {
		if name != photosView {
			return items(name, args)
		}
		var a map[string]int
		if err := json.Unmarshal(args, &a); err != nil {
			t.Fatalf("view args %s: %v", args, err)
		}
		if photoArgs != nil {
			*photoArgs = append(*photoArgs, a)
		}
		off := min(a["offset"], len(photos))
		end := min(off+a["limit"], len(photos))
		return json.Marshal(photos[off:end])
	}
}

// claudeRefBytes is a Claude endpoint that records which reference photos it
// was sent (base64 of their test bytes; the camera photo is last) and
// matches nothing.
func claudeRefBytes(t *testing.T, got *[]string) func(plugin.HTTPRequest) (plugin.HTTPResponse, error) {
	return func(req plugin.HTTPRequest) (plugin.HTTPResponse, error) {
		var body struct {
			Messages []struct {
				Content []map[string]any `json:"content"`
			} `json:"messages"`
		}
		if err := json.Unmarshal(req.Body, &body); err != nil {
			t.Fatal(err)
		}
		for _, b := range body.Messages[0].Content {
			if b["type"] == "image" {
				*got = append(*got, b["source"].(map[string]any)["data"].(string))
			}
		}
		return plugin.HTTPResponse{Status: 200, Body: []byte(`{"content":[{"type":"text","text":"{\"matches\":[],\"suggested_name\":\"\"}"}],"stop_reason":"end_turn"}`)}, nil
	}
}

// ut-docs#4094, the owner's case: a 230-item catalog where the item at
// alphabetical position 183 has the newest photo. With catalog.photos.v1
// the plugin opens only photographed items, newest first, so that photo is
// sent; items without a photo cost no open.
func TestIdentify_ReferencePhotosFollowThePhotoView(t *testing.T) {
	var rows []catalogRow
	for i := range 230 {
		rows = append(rows, catalogRow{ID: fmt.Sprintf("itm%03d", i), SKU: fmt.Sprintf("S%d", i), Name: fmt.Sprintf("Item %03d", i), Active: true})
	}
	h := newHost(t, map[string]string{"provider": "claude", "api_key": "sk-ant"})
	h.Uploads[photoToken] = jpegPhoto
	// The view's order: the newest photo (item 183) first, then 99 older
	// thumbnails on items 100..198 (item 183 excepted), and one row for an
	// item outside the active catalog, which must be skipped unopened.
	photos := []photoRow{{ItemID: "itm183", Source: "thumb", PhotoAt: 2000}, {ItemID: "gone", Source: "thumb", PhotoAt: 1999}}
	h.ItemImages["itm183"] = plugin.ItemImageFiles{Thumb: []byte("ITEM-183")}
	for i := 100; i < 200; i++ {
		if i == 183 {
			continue
		}
		id := fmt.Sprintf("itm%03d", i)
		photos = append(photos, photoRow{ItemID: id, Source: "thumb", PhotoAt: int64(1000 + i)})
		h.ItemImages[id] = plugin.ItemImageFiles{Thumb: []byte{byte(i)}}
	}
	var photoArgs []map[string]int
	h.Views = photoViews(t, rows, photos, &photoArgs)
	var sent []string
	h.HTTP = claudeRefBytes(t, &sent)
	if _, err := dispatch(t, "catalog.identify", photoPayload()); err != nil {
		t.Fatal(err)
	}
	if len(photoArgs) != 1 || photoArgs[0]["offset"] != 0 || photoArgs[0]["limit"] != photoViewLimit {
		t.Fatalf("photo view calls = %v, want one page of %d", photoArgs, photoViewLimit)
	}
	refs := sent[:len(sent)-1] // the camera photo comes last
	if len(refs) != maxReferenceImages {
		t.Fatalf("reference photos sent = %d, want %d", len(refs), maxReferenceImages)
	}
	if want := base64.StdEncoding.EncodeToString([]byte("ITEM-183")); refs[0] != want {
		t.Fatalf("first reference photo = %q, want item 183's (%q)", refs[0], want)
	}
	if got := h.Calls["item_image_open"]; got != maxReferenceImages {
		t.Fatalf("item_image_open calls = %d, want %d (only photographed, active items)", got, maxReferenceImages)
	}
}

// A photo view row whose open fails (the file no longer decodes) costs one
// open and is skipped; the walk never passes the host's 64 opens.
func TestIdentify_PhotoViewOpensStopAtHostCap(t *testing.T) {
	var rows []catalogRow
	var photos []photoRow
	for i := range 200 {
		id := fmt.Sprintf("itm%03d", i)
		rows = append(rows, catalogRow{ID: id, SKU: "S", Name: "N", Active: true})
		photos = append(photos, photoRow{ItemID: id, Source: "thumb", PhotoAt: int64(500 - i)})
	}
	h := newHost(t, map[string]string{"provider": "claude", "api_key": "sk-ant"})
	h.Uploads[photoToken] = jpegPhoto
	h.Views = photoViews(t, rows, photos, nil) // listed, but no file opens
	var sent []string
	h.HTTP = claudeRefBytes(t, &sent)
	if _, err := dispatch(t, "catalog.identify", photoPayload()); err != nil {
		t.Fatal(err)
	}
	if got := h.Calls["item_image_open"]; got != maxImageOpens {
		t.Fatalf("item_image_open calls = %d, want %d", got, maxImageOpens)
	}
	if len(sent) != 1 {
		t.Fatalf("sent %d images, want only the camera photo", len(sent))
	}
}

// A denied photo view (no grant) falls back to the catalog walk.
func TestIdentify_PhotoViewDeniedFallsBackToCatalogOrder(t *testing.T) {
	h := newHost(t, map[string]string{"provider": "claude", "api_key": "sk-ant"})
	h.Uploads[photoToken] = jpegPhoto
	items := catalogViews(t, []catalogRow{{ID: "a", SKU: "A", Name: "A", Active: true}}, nil)
	h.Views = func(name string, args json.RawMessage) (json.RawMessage, error) {
		if name == photosView {
			return nil, plugin.ErrDenied
		}
		return items(name, args)
	}
	h.ItemImages["a"] = plugin.ItemImageFiles{Thumb: []byte("A")}
	var sent []string
	h.HTTP = claudeRefBytes(t, &sent)
	if _, err := dispatch(t, "catalog.identify", photoPayload()); err != nil {
		t.Fatal(err)
	}
	if len(sent) != 2 || sent[0] != base64.StdEncoding.EncodeToString([]byte("A")) {
		t.Fatalf("sent = %v, want the camera photo and item a's", sent)
	}
}

// A repeated photo view row costs no second open; an unreadable answer
// falls back to the catalog walk.
func TestIdentify_PhotoViewDuplicateRowsAndBadAnswer(t *testing.T) {
	rows := []catalogRow{{ID: "a", SKU: "A", Name: "A", Active: true}, {ID: "b", SKU: "B", Name: "B", Active: true}}

	h := newHost(t, map[string]string{"provider": "claude", "api_key": "sk-ant"})
	h.Uploads[photoToken] = jpegPhoto
	h.Views = photoViews(t, rows, []photoRow{{ItemID: "b", Source: "thumb", PhotoAt: 2}, {ItemID: "b", Source: "thumb", PhotoAt: 2}}, nil)
	h.ItemImages["b"] = plugin.ItemImageFiles{Thumb: []byte("B")}
	var sent []string
	h.HTTP = claudeRefBytes(t, &sent)
	if _, err := dispatch(t, "catalog.identify", photoPayload()); err != nil {
		t.Fatal(err)
	}
	if got := h.Calls["item_image_open"]; got != 1 || len(sent) != 2 {
		t.Fatalf("duplicate row: %d opens, %d images; want 1 open, 1 reference", got, len(sent)-1)
	}

	h = newHost(t, map[string]string{"provider": "claude", "api_key": "sk-ant"})
	h.Uploads[photoToken] = jpegPhoto
	items := catalogViews(t, rows, nil)
	h.Views = func(name string, args json.RawMessage) (json.RawMessage, error) {
		if name == photosView {
			return json.RawMessage(`{`), nil
		}
		return items(name, args)
	}
	h.ItemImages["b"] = plugin.ItemImageFiles{Thumb: []byte("B")}
	sent = nil
	h.HTTP = claudeRefBytes(t, &sent)
	if _, err := dispatch(t, "catalog.identify", photoPayload()); err != nil {
		t.Fatal(err)
	}
	if got := h.Calls["item_image_open"]; got != 2 || len(sent) != 2 || sent[0] != base64.StdEncoding.EncodeToString([]byte("B")) {
		t.Fatalf("bad answer: %d opens, sent %v; want the name-order walk (2 opens, item b's photo)", got, sent)
	}
}
