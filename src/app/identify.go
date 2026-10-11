package app

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/universaltill/universal-till/sdk/plugin"
	"github.com/universaltill/ut-plugin-integration-ai/src/ai"
)

// Ported from universal-till internal/pages/ai_api.go (POST
// /api/pos/identify): the photo now arrives as an upload handle on the
// catalog.identify job (plugin-views.md "Suggestions and the
// camera-identify seam"), and the answer is a suggestions document.

const (
	// maxPhotoBytes is core's maxIdentifyPhotoBytes.
	maxPhotoBytes = 4 << 20
	// maxCatalogItems is core's SearchActiveItems limit.
	maxCatalogItems = 500
	// catalogPage is catalog.items.v1's largest page; maxCatalogPages bounds
	// the paging (each page is one of the event's 64 view_query calls).
	catalogPage     = 500
	maxCatalogPages = 20
	// maxReferenceImages is core's cap on reference photos per request.
	maxReferenceImages = 60
	// maxImageOpens is the host's item_image_open cap per event (failed
	// opens count): past it every open is -5.
	maxImageOpens = 64
	// photosView lists the active items that have a reference photo,
	// confirmed photos first, newest first (ut-docs#4094). A till without
	// it answers -1; the plugin then walks the catalog in name order.
	photosView = "catalog.photos.v1"
	// photoViewLimit is the one page read from photosView: twice the
	// reference cap, so a few rows outside the active catalog (past its
	// 500-item cap) don't shrink the set much; the open cap bounds the walk.
	// A catalog of over 500 items whose photos sit mostly past the first
	// 500 names can still get fewer than 60 (not a regression).
	photoViewLimit = 2 * maxReferenceImages
	// maxSKUBytes is the add_to_basket SKU bound.
	maxSKUBytes = 128
)

type uploadHandle struct {
	Field       string `json:"field"`
	Handle      string `json:"handle"`
	ContentType string `json:"content_type"`
}

type identifyPayload struct {
	UploadHandles []uploadHandle `json:"upload_handles"`
}

var errBadPhoto = errors.New("photo missing, too large, or not a JPEG/PNG")

func handleIdentify(e plugin.Event) (any, error) {
	var p identifyPayload
	if err := e.Decode(&p); err != nil {
		return nil, err
	}
	svc := service()
	if !svc.Enabled() {
		return document(notice("warn", "integration_ai.identify.not_configured")), nil
	}

	progress(5, "integration_ai.identify.reading_photo")
	photo, mediaType, err := readPhoto(p.UploadHandles)
	if errors.Is(err, errBadPhoto) {
		return document(notice("error", "integration_ai.identify.bad_photo")), nil
	}
	if err != nil {
		return nil, err
	}

	progress(15, "integration_ai.identify.reading_catalog")
	items, err := activeCatalog()
	if err != nil {
		return nil, fmt.Errorf("catalog unavailable: %w", err)
	}

	var refs []ai.RefImage
	if svc.UsesReferenceImages() {
		progress(30, "integration_ai.identify.reading_references")
		refs = loadReferenceImages(items)
	}

	progress(45, "integration_ai.identify.searching")
	res, err := svc.Identify(photo, mediaType, items, refs)
	if err != nil {
		plugin.Logf("ai identify failed: %v", err)
		return nil, err
	}
	return identifyDocument(res, items), nil
}

// readPhoto reads the job's photo (field "photo", else the first upload),
// at most maxPhotoBytes, and sniffs JPEG/PNG from its bytes (core decoded
// it with image.Decode: JPEG or PNG only).
func readPhoto(handles []uploadHandle) ([]byte, string, error) {
	var tok string
	for _, h := range handles {
		if h.Field == "photo" {
			tok = h.Handle
			break
		}
	}
	if tok == "" && len(handles) > 0 {
		tok = handles[0].Handle
	}
	if tok == "" {
		return nil, "", errBadPhoto
	}
	u, err := plugin.UploadOpen(tok)
	if errors.Is(err, plugin.ErrNotFound) || errors.Is(err, plugin.ErrInvalid) {
		return nil, "", errBadPhoto
	}
	if err != nil {
		return nil, "", err
	}
	defer func() { _ = u.Close() }()
	raw, err := io.ReadAll(io.LimitReader(u, maxPhotoBytes+1))
	if err != nil {
		return nil, "", err
	}
	if len(raw) == 0 || len(raw) > maxPhotoBytes {
		return nil, "", errBadPhoto
	}
	switch {
	case bytes.HasPrefix(raw, []byte{0xFF, 0xD8, 0xFF}):
		return raw, "image/jpeg", nil
	case bytes.HasPrefix(raw, []byte("\x89PNG\r\n\x1a\n")):
		return raw, "image/png", nil
	}
	return nil, "", errBadPhoto
}

type catalogItemRow struct {
	ID     string `json:"id"`
	SKU    string `json:"sku"`
	Name   string `json:"name"`
	Active bool   `json:"active"`
}

// activeCatalog pages catalog.items.v1 (every item, inactive included) and
// keeps the first maxCatalogItems active ones — core's
// SearchActiveItems("", 0, 500).
func activeCatalog() ([]ai.CatalogItem, error) {
	items := make([]ai.CatalogItem, 0, 64)
	for page := range maxCatalogPages {
		raw, err := plugin.ViewQuery("catalog.items.v1", map[string]int{"offset": page * catalogPage, "limit": catalogPage})
		if err != nil {
			return nil, err
		}
		var rows []catalogItemRow
		if err := json.Unmarshal(raw, &rows); err != nil {
			return nil, fmt.Errorf("catalog.items.v1: %w", err)
		}
		for _, r := range rows {
			if !r.Active {
				continue
			}
			items = append(items, ai.CatalogItem{ID: r.ID, SKU: r.SKU, Name: r.Name})
			if len(items) == maxCatalogItems {
				return items, nil
			}
		}
		if len(rows) < catalogPage {
			break
		}
	}
	return items, nil
}

type photoViewRow struct {
	ItemID string `json:"item_id"`
}

// photoOrder returns the active catalog items that have a reference photo,
// in catalog.photos.v1's order (confirmed photos first, newest first), so
// no open is spent on an item without one (ut-docs#4094). ok is false when
// the view is unavailable — an older till (-1), no grant, an error — and
// the caller falls back to the catalog's name order.
func photoOrder(items []ai.CatalogItem) (_ []ai.CatalogItem, ok bool) {
	raw, err := plugin.ViewQuery(photosView, map[string]int{"offset": 0, "limit": photoViewLimit})
	if err != nil {
		if !errors.Is(err, plugin.ErrNotFound) {
			plugin.Logf("ai identify: %s unavailable, using catalog order: %v", photosView, err)
		}
		return nil, false
	}
	var rows []photoViewRow
	if err := json.Unmarshal(raw, &rows); err != nil {
		plugin.Logf("ai identify: %s: %v", photosView, err)
		return nil, false
	}
	byID := make(map[string]ai.CatalogItem, len(items))
	for _, it := range items {
		byID[it.ID] = it
	}
	out := make([]ai.CatalogItem, 0, len(rows))
	for _, r := range rows {
		if it, found := byID[r.ItemID]; found {
			out = append(out, it)
			delete(byID, r.ItemID) // a repeated row costs no second open
		}
	}
	return out, true
}

// loadReferenceImages picks one reference photo per item — role "ref", the
// item's newest cashier-confirmed photo if it decodes, else its thumbnail:
// the same choice and bytes core's loadReferenceImages sent — capped at
// maxReferenceImages. The items come in photoOrder when the till has
// catalog.photos.v1, so a photographed item is sent whatever its name
// (ut-docs#4094); otherwise in catalog name order, where the host's
// maxImageOpens opens per event (failed opens included) reach only the
// first 64 items. A denial (no view:inventory), the quota or busy handles
// stop the walk; identify then runs on the photo and catalog text alone.
func loadReferenceImages(items []ai.CatalogItem) []ai.RefImage {
	if ordered, ok := photoOrder(items); ok {
		items = ordered
	}
	refs := make([]ai.RefImage, 0, maxReferenceImages)
	for i, it := range items {
		if len(refs) >= maxReferenceImages || i >= maxImageOpens {
			break
		}
		img, err := plugin.ItemImageOpen(it.ID, plugin.ItemImageRef)
		if err != nil {
			if errors.Is(err, plugin.ErrDenied) || errors.Is(err, plugin.ErrQuota) || errors.Is(err, plugin.ErrBusy) {
				break
			}
			continue // no photo for this item, or an id the host refuses
		}
		data, err := io.ReadAll(img)
		if err != nil || len(data) == 0 {
			continue
		}
		refs = append(refs, ai.RefImage{ItemID: it.ID, MediaType: "image/jpeg", Data: data})
	}
	return refs
}

// confidenceKeys maps the model's confidence to the suggestion detail.
var confidenceKeys = map[string]string{
	"high":   "integration_ai.identify.confidence.high",
	"medium": "integration_ai.identify.confidence.medium",
	"low":    "integration_ai.identify.confidence.low",
}

// validSKU is the add_to_basket SKU rule: 1–128 bytes of valid UTF-8, no
// control characters (C0 or C1), no leading or trailing space.
func validSKU(s string) bool {
	if s == "" || len(s) > maxSKUBytes || strings.TrimSpace(s) != s {
		return false
	}
	// Mirrors the till's hasControl (pluginview/suggestions.go): one SKU it
	// refuses voids the whole answer, so skip that candidate here instead.
	return utf8.ValidString(s) && strings.IndexFunc(s, unicode.IsControl) < 0
}

// identifyDocument turns the model's answer into the seam's document: one
// add_to_basket suggestion per match (best first), or a no-match notice,
// plus the model's suggested name for a product not in the catalog.
func identifyDocument(res *ai.IdentifyResult, items []ai.CatalogItem) map[string]any {
	byID := make(map[string]ai.CatalogItem, len(items))
	for _, it := range items {
		byID[it.ID] = it
	}
	var suggestions []map[string]any
	for _, m := range res.Matches {
		it, ok := byID[m.ItemID]
		if !ok || !validSKU(it.SKU) || len(suggestions) == 20 {
			continue
		}
		s := map[string]any{
			"label":  literal(truncate(it.Name, maxLiteralBytes)),
			"effect": map[string]any{"add_to_basket": map[string]any{"sku": it.SKU, "qty": 1}},
		}
		if k, ok := confidenceKeys[m.Confidence]; ok {
			s["detail"] = key(k)
		}
		suggestions = append(suggestions, s)
	}
	var cs []component
	// Without suggestions the till's overlay says "No match found" itself;
	// a notice of our own would say it twice.
	if len(suggestions) > 0 {
		cs = append(cs, component{"type": "suggestions", "items": suggestions})
	}
	if name := strings.TrimSpace(res.SuggestedName); name != "" {
		cs = append(cs, text(key("integration_ai.identify.suggested")), text(literal(truncate(name, maxLiteralBytes))))
	}
	return document(cs...)
}
