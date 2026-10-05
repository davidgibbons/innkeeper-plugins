package main

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"slices"
	"strings"

	"github.com/davidgibbons/innkeeper/protocol"
)

// bookFields are character_book fields Lumiverse doesn't store. The plugin
// keeps them in the world book's metadata.
var bookFields = []string{"scan_depth", "token_budget", "recursive_scanning", "extensions"}

type worldBook struct {
	ID       string         `json:"id"`
	Metadata map[string]any `json:"metadata"`
}

type page[T any] struct {
	Data  []T `json:"data"`
	Total int `json:"total"`
}

func (p *plugin) putLorebook(ctx context.Context, params json.RawMessage) (any, error) {
	var in protocol.TargetPutLorebookParams
	if err := json.Unmarshal(params, &in); err != nil {
		return nil, invalid(err)
	}
	var book map[string]any
	if err := json.Unmarshal(in.Lorebook, &book); err != nil || book == nil {
		return nil, invalid(fmt.Errorf("lorebook is not a JSON object"))
	}
	id, key := in.RemoteID, in.Key
	if id == "" && key != "" {
		found, err := p.findBook(ctx, key)
		if err != nil {
			return nil, err
		}
		id = found.ID
	}
	ccv3 := map[string]any{}
	for _, f := range bookFields {
		if v, ok := book[f]; ok {
			ccv3[f] = v
		}
	}
	name, _ := book["name"].(string)
	description, _ := book["description"].(string)
	info := map[string]any{"name": cmp.Or(name, "Lorebook"), "description": description}
	if id == "" {
		info["metadata"] = map[string]any{"source": "innkeeper", "sync_key": key, "ccv3": ccv3}
		var created worldBook
		if err := p.c.call(ctx, "POST", "/world-books", info, &created); err != nil {
			return nil, err
		}
		id = created.ID
	} else {
		// A replace keeps the book's ID, so characters stay linked.
		var cur worldBook
		if err := p.c.call(ctx, "GET", "/world-books/"+url.PathEscape(id)+"?limit=1", nil, &cur); err != nil {
			return nil, err
		}
		meta := cur.Metadata
		if meta == nil {
			meta = map[string]any{}
		}
		meta["source"], meta["ccv3"] = "innkeeper", ccv3
		if key != "" {
			meta["sync_key"] = key
		}
		info["metadata"] = meta
		if err := p.c.call(ctx, "PUT", "/world-books/"+url.PathEscape(id), info, nil); err != nil {
			return nil, err
		}
		if err := p.clearEntries(ctx, id); err != nil {
			return nil, err
		}
	}
	entries, _ := book["entries"].([]any)
	for i, e := range entries {
		// ponytail: one request per entry, and a failure leaves the book
		// partial until the job's retry replaces it again.
		if err := p.c.call(ctx, "POST", "/world-books/"+url.PathEscape(id)+"/entries", toEntry(object(e), i), nil); err != nil {
			return nil, fmt.Errorf("entry %d of %d: %w", i+1, len(entries), err)
		}
	}
	return protocol.TargetPutResult{RemoteID: id}, nil
}

// findBook returns the world book a put with key created, if any.
//
// ponytail: pages through every world book, since Lumiverse can't filter on
// metadata; fine for hundreds of books.
func (p *plugin) findBook(ctx context.Context, key string) (worldBook, error) {
	for offset := 0; ; offset += 1000 {
		var books page[worldBook]
		if err := p.c.call(ctx, "GET", fmt.Sprintf("/world-books?limit=1000&offset=%d", offset), nil, &books); err != nil {
			return worldBook{}, err
		}
		for _, b := range books.Data {
			if b.Metadata["source"] == "innkeeper" && b.Metadata["sync_key"] == key {
				return b, nil
			}
		}
		if len(books.Data) == 0 || offset+len(books.Data) >= books.Total {
			return worldBook{}, nil
		}
	}
}

// clearEntries deletes every entry of a world book.
func (p *plugin) clearEntries(ctx context.Context, id string) error {
	for {
		var entries page[struct {
			ID string `json:"id"`
		}]
		if err := p.c.call(ctx, "GET", "/world-books/"+url.PathEscape(id)+"/entries?limit=1000", nil, &entries); err != nil {
			return err
		}
		if len(entries.Data) == 0 {
			return nil
		}
		ids := make([]string, len(entries.Data))
		for i, e := range entries.Data {
			ids[i] = e.ID
		}
		if err := p.c.call(ctx, "POST", "/world-books/"+url.PathEscape(id)+"/entries/bulk",
			map[string]any{"action": "delete", "entry_ids": ids}, nil); err != nil {
			return err
		}
	}
}

// positions maps CCv3 positions to Lumiverse's.
var positions = map[string]int{"before_char": 0, "after_char": 1}

// toEntry maps a CCv3 entry to a Lumiverse entry, as Lumiverse's own import
// does. extensions.innkeeper keeps what Lumiverse would lose: the entry's
// id, name, position, and place in the book.
func toEntry(e map[string]any, index int) map[string]any {
	ext := map[string]any{}
	for k, v := range object(e["extensions"]) {
		ext[k] = v
	}
	ext[innkeeperKey] = map[string]any{"index": index, "id": e["id"], "name": e["name"], "position": e["position"]}
	comment, _ := e["comment"].(string)
	name, _ := e["name"].(string)
	enabled, ok := e["enabled"].(bool)
	content, _ := e["content"].(string)
	order := e["insertion_order"]
	if order == nil {
		order = index
	}
	out := map[string]any{
		"key":            listOr(e["keys"]),
		"keysecondary":   listOr(e["secondary_keys"]),
		"content":        content,
		"comment":        cmp.Or(comment, name),
		"disabled":       ok && !enabled,
		"order_value":    order,
		"position":       position(e["position"]),
		"selective":      e["selective"] == true,
		"constant":       e["constant"] == true,
		"case_sensitive": e["case_sensitive"] == true,
		"extensions":     ext,
	}
	if v, ok := e["priority"]; ok {
		out["priority"] = v
	}
	if v, ok := ext["depth"]; ok {
		out["depth"] = v
	}
	return out
}

func position(v any) any {
	switch p := v.(type) {
	case float64:
		return p
	case string:
		if n, ok := positions[strings.ToLower(strings.TrimSpace(p))]; ok {
			return n
		}
	}
	return 0
}

func (p *plugin) getLorebook(ctx context.Context, params json.RawMessage) (any, error) {
	var in protocol.TargetGetParams
	if err := json.Unmarshal(params, &in); err != nil {
		return nil, invalid(err)
	}
	path := "/world-books/" + url.PathEscape(in.RemoteID)
	var cur worldBook
	if err := p.c.call(ctx, "GET", path+"?limit=1", nil, &cur); err != nil {
		return nil, err
	}
	var book map[string]any
	if err := p.c.call(ctx, "GET", path+"/export?format=character_book", nil, &book); err != nil {
		return nil, err
	}
	for k, v := range object(cur.Metadata["ccv3"]) {
		book[k] = v
	}
	entries, _ := book["entries"].([]any)
	for _, e := range entries {
		fromEntry(object(e))
	}
	// Lumiverse orders entries by insertion order; restore the book's order.
	// Entries added in Lumiverse go last.
	slices.SortStableFunc(entries, func(a, b any) int { return cmp.Compare(entryIndex(a), entryIndex(b)) })
	for _, e := range entries {
		delete(object(object(e)["extensions"]), innkeeperKey)
	}
	book["entries"] = entries
	raw, err := json.Marshal(book)
	if err != nil {
		return nil, err
	}
	return protocol.TargetGetLorebookResult{Lorebook: raw}, nil
}

// fromEntry restores the id, name, and position a pushed entry had. A
// position changed in Lumiverse stays as Lumiverse reports it.
func fromEntry(e map[string]any) {
	stash := object(object(e["extensions"])[innkeeperKey])
	if len(stash) == 0 {
		return
	}
	for _, k := range []string{"id", "name"} {
		if v, ok := stash[k]; ok && v != nil {
			e[k] = v
		} else {
			delete(e, k)
		}
	}
	if pos, ok := stash["position"]; ok && fmt.Sprint(position(pos)) == fmt.Sprint(e["position"]) {
		if pos == nil {
			delete(e, "position")
		} else {
			e["position"] = pos
		}
	}
}

func entryIndex(e any) float64 {
	i, ok := object(object(object(e)["extensions"])[innkeeperKey])["index"].(float64)
	if !ok {
		return 1 << 50
	}
	return i
}
