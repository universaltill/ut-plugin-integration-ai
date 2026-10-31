package app

import (
	"encoding/json"
	"fmt"
	"math"
	"slices"
	"strings"

	"github.com/universaltill/universal-till/sdk/plugin"
	"github.com/universaltill/ut-plugin-integration-ai/src/ai"
)

// Catalog tools for the Ask panel (ut-docs#4128, ADR-0153 Decision 3).
//
// Reads go through core's catalog.gaps.v1 and catalog.items.v1 views. The
// one write tool, propose_item_changes, only PROPOSES: core works out the
// before/after rows, draws its own confirm card (the "confirm" component)
// and runs the change only after the operator presses Confirm, under the
// operator's own role. What the tools found and proposed is remembered in
// a collector so the answer can carry core-drawn "item_list" and "confirm"
// components -- ids only; the plugin supplies no thumbnail, URL or wording
// for them.

// Gap bits of catalog.gaps.v1's `missing` argument.
var gapBits = map[string]int{"barcode": 1, "image": 2, "price": 4, "category": 8}

const (
	maxListedItems   = 50 // item_list's cap
	maxProposalsTurn = 3  // core's ceiling on open proposals per turn
	toolDefaultLimit = 10
	toolMaxLimit     = 25
	findPageSize     = 500
	findMaxPages     = 8
)

// collector remembers one turn's catalog findings and proposals.
type collector struct {
	ids       []string
	mask      int
	mixed     bool // ids from find_items too: no single catalog filter matches the list
	proposals []string
}

func (c *collector) addItems(mask int, ids ...string) {
	c.mask |= mask
	if mask == 0 && len(ids) > 0 {
		c.mixed = true
	}
	for _, id := range ids {
		if len(c.ids) < maxListedItems && !slices.Contains(c.ids, id) {
			c.ids = append(c.ids, id)
		}
	}
}

// components are the answer's core-drawn parts: the found items, then one
// confirm card per open proposal.
func (c *collector) components() []component {
	var out []component
	if len(c.ids) > 0 {
		ids := make([]any, len(c.ids))
		for i, id := range c.ids {
			ids[i] = id
		}
		l := component{"type": "item_list", "items": ids}
		// The "same filter" catalog link is offered only when every listed
		// item came from the gap listing.
		if c.mask != 0 && !c.mixed {
			l["missing"] = c.mask
		}
		out = append(out, l)
	}
	for _, p := range c.proposals {
		out = append(out, component{"type": "confirm", "proposal": p})
	}
	return out
}

type gapRow struct {
	ItemID     string `json:"item_id"`
	SKU        string `json:"sku"`
	Name       string `json:"name"`
	Category   string `json:"category"`
	PriceMinor int64  `json:"price_minor"`
	HasBarcode bool   `json:"has_barcode"`
	HasImage   bool   `json:"has_image"`
}

type itemRow struct {
	ID         string `json:"id"`
	SKU        string `json:"sku"`
	Name       string `json:"name"`
	CategoryID string `json:"category_id"`
	Category   string `json:"category"`
	PriceMinor int64  `json:"price_minor"`
	Active     bool   `json:"active"`
}

func catalogTools(c *collector) []ai.AskTool {
	return []ai.AskTool{
		{
			Name: "list_items_with_gaps",
			Description: "List active catalog items that are missing a barcode, a photo, a price (zero) or a category. " +
				"Returns up to `limit` items (default 10, max 25) with has_more when there are further pages (use `offset`). " +
				"The till shows the operator thumbnails, a link to each item's edit page and a link to the catalog filtered the same way by itself: " +
				"do not write links or image addresses. Prices are minor units.",
			Params: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"missing": map[string]any{"type": "array", "items": map[string]any{"type": "string", "enum": []string{"barcode", "image", "price", "category"}},
						"description": "which gaps to look for; an item with any of them is listed"},
					"limit":  map[string]any{"type": "integer", "description": "items per page (1-25)"},
					"offset": map[string]any{"type": "integer", "description": "items to skip, for the next page"},
				},
				"required": []string{"missing"},
			},
			Run: func(args map[string]any) (any, error) { return listItemsWithGaps(c, args) },
		},
		{
			Name: "find_items",
			Description: "Find active catalog items by name or SKU (case-insensitive part of the text) and/or category name. " +
				"Returns id, name, sku, category_id, category and price (minor units); use the ids and category_id with propose_item_changes. " +
				"The till shows the operator thumbnails and links by itself.",
			Params: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"query":    map[string]any{"type": "string", "description": "part of the item name or SKU"},
					"category": map[string]any{"type": "string", "description": "part of the category name"},
					"limit":    map[string]any{"type": "integer", "description": "items to return (1-25)"},
				},
			},
			Run: func(args map[string]any) (any, error) { return findItems(c, args) },
		},
		{
			Name: "propose_item_changes",
			Description: "PROPOSE changes to existing items' name, price (price_minor, minor units), category (category_id) or barcode (only for an item that has none). " +
				"Nothing changes until the operator presses Confirm on the card the till shows; the till works out and shows the exact before and after itself. " +
				"Never say a change is done: say it is waiting for their confirmation. Use item ids from the other tools; at most 50 changes.",
			Params: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"changes": map[string]any{"type": "array", "items": map[string]any{
						"type": "object",
						"properties": map[string]any{
							"item_id":     map[string]any{"type": "string"},
							"name":        map[string]any{"type": "string"},
							"price_minor": map[string]any{"type": "integer"},
							"category_id": map[string]any{"type": "string"},
							"barcode":     map[string]any{"type": "string"},
						},
						"required": []string{"item_id"},
					}},
				},
				"required": []string{"changes"},
			},
			Run: func(args map[string]any) (any, error) { return proposeItemChanges(c, args) },
		},
	}
}

func listItemsWithGaps(c *collector, args map[string]any) (any, error) {
	list, ok := args["missing"].([]any)
	if !ok || len(list) == 0 || len(list) > len(gapBits) {
		return nil, fmt.Errorf("missing must list one or more of barcode, image, price, category")
	}
	mask := 0
	for _, g := range list {
		s, _ := g.(string)
		bit, known := gapBits[s]
		if !known {
			return nil, fmt.Errorf("unknown gap %q: use barcode, image, price or category", s)
		}
		mask |= bit
	}
	limit := intArg(args, "limit", toolDefaultLimit, 1, toolMaxLimit)
	offset := intArg(args, "offset", 0, 0, 1_000_000)
	// One probe row says whether another page exists.
	raw, err := plugin.ViewQuery("catalog.gaps.v1", map[string]int{"missing": mask, "offset": offset, "limit": limit + 1})
	if err != nil {
		return nil, err
	}
	var rows []gapRow
	if err := json.Unmarshal(raw, &rows); err != nil {
		return nil, fmt.Errorf("catalog.gaps.v1: %w", err)
	}
	more := len(rows) > limit
	if more {
		rows = rows[:limit]
	}
	ids := make([]string, len(rows))
	for i, r := range rows {
		ids[i] = r.ItemID
	}
	c.addItems(mask, ids...)
	return map[string]any{"items": rows, "has_more": more}, nil
}

func findItems(c *collector, args map[string]any) (any, error) {
	q, _ := args["query"].(string)
	cat, _ := args["category"].(string)
	q, cat = strings.ToLower(strings.TrimSpace(q)), strings.ToLower(strings.TrimSpace(cat))
	if q == "" && cat == "" {
		return nil, fmt.Errorf("give a query (name or SKU) and/or a category")
	}
	limit := intArg(args, "limit", toolDefaultLimit, 1, toolMaxLimit)
	var found []itemRow
	more := false
scan:
	for page := 0; page < findMaxPages; page++ {
		raw, err := plugin.ViewQuery("catalog.items.v1", map[string]int{"offset": page * findPageSize, "limit": findPageSize})
		if err != nil {
			return nil, err
		}
		var rows []itemRow
		if err := json.Unmarshal(raw, &rows); err != nil {
			return nil, fmt.Errorf("catalog.items.v1: %w", err)
		}
		for _, r := range rows {
			if !r.Active {
				continue
			}
			if q != "" && !strings.Contains(strings.ToLower(r.Name), q) && !strings.Contains(strings.ToLower(r.SKU), q) {
				continue
			}
			if cat != "" && !strings.Contains(strings.ToLower(r.Category), cat) {
				continue
			}
			if len(found) == limit {
				more = true
				break scan
			}
			found = append(found, r)
		}
		if len(rows) < findPageSize {
			break
		}
	}
	ids := make([]string, len(found))
	for i, r := range found {
		ids[i] = r.ID
	}
	c.addItems(0, ids...)
	return map[string]any{"items": found, "has_more": more}, nil
}

var changeKeys = []string{"item_id", "name", "price_minor", "category_id", "barcode"}

func proposeItemChanges(c *collector, args map[string]any) (any, error) {
	list, ok := args["changes"].([]any)
	if !ok || len(list) == 0 || len(list) > 50 {
		return nil, fmt.Errorf("changes must list 1 to 50 items")
	}
	if len(c.proposals) >= maxProposalsTurn {
		return nil, fmt.Errorf("at most %d proposals per question: ask the operator to confirm these first", maxProposalsTurn)
	}
	out := make([]map[string]any, 0, len(list))
	for i, e := range list {
		m, ok := e.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("changes[%d] must be an object", i)
		}
		ch := map[string]any{}
		for k, v := range m {
			if !slices.Contains(changeKeys, k) {
				return nil, fmt.Errorf("changes[%d]: unknown field %q", i, k)
			}
			if k == "price_minor" {
				f, isNum := v.(float64)
				if !isNum || f != math.Trunc(f) || f < 0 || f > 1e11 {
					return nil, fmt.Errorf("changes[%d].price_minor must be a whole number of minor units", i)
				}
				ch[k] = int64(f)
				continue
			}
			s, isStr := v.(string)
			if !isStr {
				return nil, fmt.Errorf("changes[%d].%s must be text", i, k)
			}
			ch[k] = s
		}
		if id, _ := ch["item_id"].(string); id == "" || len(ch) < 2 {
			return nil, fmt.Errorf("changes[%d] needs item_id and at least one field to change", i)
		}
		out = append(out, ch)
	}
	id, err := plugin.ActionPropose("catalog.item.update.v1", map[string]any{"changes": out})
	if err != nil {
		plugin.Logf("propose_item_changes refused: %v", err)
		return nil, fmt.Errorf("the till did not accept the proposal (%v): the change may be invalid, already done, or not allowed for this operator", err)
	}
	c.proposals = append(c.proposals, id)
	return "Proposed. The operator now sees a confirmation card with the exact before and after; nothing has changed yet and nothing changes unless they press Confirm.", nil
}
