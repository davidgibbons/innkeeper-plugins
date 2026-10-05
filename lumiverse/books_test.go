package main

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/davidgibbons/innkeeper/protocol"
)

func lorebook(entries ...map[string]any) json.RawMessage {
	list := []any{}
	for _, e := range entries {
		list = append(list, e)
	}
	raw, _ := json.Marshal(map[string]any{"name": "Forge lore", "description": "Smithing.", "scan_depth": 6.0,
		"extensions": map[string]any{"x": 1.0}, "entries": list})
	return raw
}

func entry(id float64, name, content string, order float64) map[string]any {
	return map[string]any{"id": id, "name": name, "keys": []any{name}, "content": content, "enabled": true,
		"insertion_order": order, "position": "after_char", "extensions": map[string]any{}}
}

func putBook(t *testing.T, p *plugin, in protocol.TargetPutLorebookParams) string {
	t.Helper()
	res, err := p.putLorebook(context.Background(), mustJSON(t, in))
	if err != nil {
		t.Fatal(err)
	}
	return res.(protocol.TargetPutResult).RemoteID
}

func getBook(t *testing.T, p *plugin, id string) map[string]any {
	t.Helper()
	res, err := p.getLorebook(context.Background(), mustJSON(t, protocol.TargetGetParams{RemoteID: id}))
	if err != nil {
		t.Fatal(err)
	}
	var book map[string]any
	_ = json.Unmarshal(res.(protocol.TargetGetLorebookResult).Lorebook, &book)
	return book
}

func TestCreateAndReadWorldBook(t *testing.T) {
	f := newFake(t)
	p := newPlugin(t, f, "owner", fakePassword)
	// Equal insertion orders, which Lumiverse may return in either order.
	id := putBook(t, p, protocol.TargetPutLorebookParams{Key: "k",
		Lorebook: lorebook(entry(7, "anvil", "It rings.", 5), entry(3, "forge", "It burns.", 5), entry(9, "tongs", "They grip.", 1))})
	meta := f.books[id]["metadata"].(map[string]any)
	if meta["sync_key"] != "k" || meta["source"] != "innkeeper" || len(f.entries[id]) != 3 {
		t.Fatalf("book = %v, %d entries", f.books[id], len(f.entries[id]))
	}
	book := getBook(t, p, id)
	if book["scan_depth"] != 6.0 || fmt.Sprint(book["extensions"]) != "map[x:1]" {
		t.Fatalf("book-level fields = %v", book)
	}
	entries := book["entries"].([]any)
	var names []any
	for _, e := range entries {
		e := e.(map[string]any)
		names = append(names, e["name"])
		if e["position"] != "after_char" || object(e["extensions"])[innkeeperKey] != nil {
			t.Fatalf("entry = %v", e)
		}
	}
	if fmt.Sprint(names) != "[anvil forge tongs]" || entries[0].(map[string]any)["id"] != 7.0 {
		t.Fatalf("entries = %v", entries)
	}
}

func TestReplaceWorldBookKeepsItsID(t *testing.T) {
	f := newFake(t)
	p := newPlugin(t, f, "owner", fakePassword)
	id := putBook(t, p, protocol.TargetPutLorebookParams{Key: "k",
		Lorebook: lorebook(entry(1, "a", "A", 1), entry(2, "b", "B", 2), entry(3, "c", "C", 3))})
	if again := putBook(t, p, protocol.TargetPutLorebookParams{RemoteID: id, Lorebook: lorebook(entry(1, "d", "D", 1))}); again != id {
		t.Fatalf("replace made %s", again)
	}
	if len(f.entries[id]) != 1 || f.entries[id][0]["content"] != "D" || f.books[id]["metadata"].(map[string]any)["sync_key"] != "k" {
		t.Fatalf("after replace: %v %v", f.entries[id], f.books[id])
	}
	if _, err := p.putLorebook(context.Background(), mustJSON(t, protocol.TargetPutLorebookParams{RemoteID: "gone", Lorebook: lorebook()})); !notFound(err) {
		t.Fatalf("put to a missing book: %v", err)
	}
	if _, err := p.getLorebook(context.Background(), mustJSON(t, protocol.TargetGetParams{RemoteID: "gone"})); !notFound(err) {
		t.Fatalf("get of a missing book: %v", err)
	}
}

// A re-run after a crash partway through the entries finishes that book.
func TestPutWithAKnownKeyFinishesTheBook(t *testing.T) {
	f := newFake(t)
	p := newPlugin(t, f, "owner", fakePassword)
	id := putBook(t, p, protocol.TargetPutLorebookParams{Key: "k", Lorebook: lorebook(entry(1, "a", "A", 1), entry(2, "b", "B", 2))})
	f.entries[id] = f.entries[id][:1]
	if again := putBook(t, p, protocol.TargetPutLorebookParams{Key: "k",
		Lorebook: lorebook(entry(1, "a", "A", 1), entry(2, "b", "B", 2))}); again != id || len(f.books) != 1 {
		t.Fatalf("re-run made %s; %d books", again, len(f.books))
	}
	if len(f.entries[id]) != 2 {
		t.Fatalf("book has %d entries, want 2", len(f.entries[id]))
	}
}

func TestDescribeIsValid(t *testing.T) {
	if err := describe().Validate(); err != nil {
		t.Fatal(err)
	}
}
