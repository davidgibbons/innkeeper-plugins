package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/davidgibbons/innkeeper/protocol"
)

// TestLive runs against a real Lumiverse server. It creates one character and
// one world book with a unique name, and deletes both at the end.
//
//	LUMIVERSE_LIVE=1 LUMIVERSE_URL=… LUMIVERSE_EMAIL=… LUMIVERSE_PASSWORD=… go test -run TestLive -v ./lumiverse/
func TestLive(t *testing.T) {
	if os.Getenv("LUMIVERSE_LIVE") != "1" {
		t.Skip("set LUMIVERSE_LIVE=1 and LUMIVERSE_URL, LUMIVERSE_EMAIL, LUMIVERSE_PASSWORD")
	}
	ctx := context.Background()
	p := &plugin{c: newClient(os.Getenv("LUMIVERSE_URL"), os.Getenv("LUMIVERSE_EMAIL"), os.Getenv("LUMIVERSE_PASSWORD")), blobTmp: t.TempDir()}
	stamp := time.Now().Format("20060102-150405")
	name := "Innkeeper Live Test " + stamp

	// A world book, read back unchanged.
	e1 := entry(7, "anvil", "The anvil rings.", 5)
	e1["extensions"] = map[string]any{"probability": 50.0, "custom": "kept"}
	e2 := entry(3, "forge", "The forge burns.", 5)
	e2["position"] = "before_char"
	book := map[string]any{"name": name + " lore", "description": "Live test.", "scan_depth": 6.0, "entries": []any{e1, e2}}
	bookID := putBook(t, p, protocol.TargetPutLorebookParams{Key: "live:" + stamp + ":book", Lorebook: mustJSON(t, book)})
	t.Cleanup(func() { _ = p.c.call(ctx, "DELETE", "/world-books/"+url.PathEscape(bookID), nil, nil) })
	got := getBook(t, p, bookID)
	if !sameJSON(got["entries"], book["entries"]) || got["scan_depth"] != 6.0 {
		t.Errorf("world book read back as %s", mustJSON(t, got))
	}

	// A character linked to it, with an avatar.
	img := filepath.Join(t.TempDir(), "0123abcd")
	var buf bytes.Buffer
	_ = png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 8, 8)))
	if err := os.WriteFile(img, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	key := "live:" + stamp + ":card"
	in := protocol.TargetPutParams{Card: card(map[string]any{"name": name}), Key: key, Avatar: img,
		LorebookRemoteIDs: []string{bookID}}
	id := put(t, p, in)
	t.Cleanup(func() { _ = p.c.call(ctx, "DELETE", "/characters/"+url.PathEscape(id), nil, nil) })
	data := liveCard(t, p, id)
	if data["name"] != name || data["first_mes"] != "Hail." || data["character_version"] != "2" ||
		fmt.Sprint(data["extensions"]) != "map[depth_prompt:map[depth:4 prompt:Grumble.]]" {
		t.Errorf("character read back as %s", mustJSON(t, data))
	}
	var c character
	if err := p.c.call(ctx, "GET", "/characters/"+url.PathEscape(id), nil, &c); err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(c.Extensions["world_book_ids"]) != "["+bookID+"]" {
		t.Errorf("world_book_ids = %v", c.Extensions["world_book_ids"])
	}

	// Reading it back returns the avatar and the links.
	res, err := p.get(ctx, mustJSON(t, protocol.TargetGetParams{RemoteID: id, Avatar: true}))
	if err != nil {
		t.Fatal(err)
	}
	read := res.(protocol.TargetGetResult)
	if !slices.Equal(read.LorebookRemoteIDs, []string{bookID}) {
		t.Errorf("get returned links %v, want [%s]", read.LorebookRemoteIDs, bookID)
	}
	if st, err := os.Stat(read.Avatar); err != nil || st.Size() == 0 || filepath.Dir(read.Avatar) != p.blobTmp {
		t.Errorf("get returned avatar %q (%v), want a non-empty file under %s", read.Avatar, err, p.blobTmp)
	}

	// Both lists hold what the test made, and a listed character reads back.
	chars, err := p.list(ctx, "/characters/summary?sort=name&direction=asc")
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(chars.Items, protocol.TargetItem{RemoteID: id, Name: name}) {
		t.Errorf("%s missing from %d listed characters", id, len(chars.Items))
	} else if data := liveCard(t, p, id); data["name"] != name {
		t.Errorf("listed character read back as %v", data["name"])
	}
	books, err := p.list(ctx, "/world-books?")
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(books.Items, protocol.TargetItem{RemoteID: bookID, Name: name + " lore"}) {
		t.Errorf("%s missing from %d listed world books", bookID, len(books.Items))
	}

	// A re-run with the same key finds the character.
	if again := put(t, p, in); again != id {
		t.Errorf("re-run with the key made %s, want %s", again, id)
	}

	// An edit in Lumiverse shows in the read-back; a push overwrites it.
	if err := p.c.call(ctx, "PUT", "/characters/"+url.PathEscape(id), map[string]any{"first_mes": "Well met."}, nil); err != nil {
		t.Fatal(err)
	}
	if data := liveCard(t, p, id); data["first_mes"] != "Well met." {
		t.Errorf("after an edit in Lumiverse: first_mes = %v", data["first_mes"])
	}
	in.RemoteID = id
	put(t, p, in)
	if data := liveCard(t, p, id); data["first_mes"] != "Hail." {
		t.Errorf("after a push: first_mes = %v", data["first_mes"])
	}

	// Replacing the world book keeps its ID and the link.
	book["entries"] = []any{entry(1, "tongs", "They grip.", 1)}
	if again := putBook(t, p, protocol.TargetPutLorebookParams{RemoteID: bookID, Lorebook: mustJSON(t, book)}); again != bookID {
		t.Errorf("replace made %s", again)
	}
	if got := getBook(t, p, bookID); !sameJSON(got["entries"], book["entries"]) {
		t.Errorf("replaced world book read back as %s", mustJSON(t, got["entries"]))
	}
	// A re-run of the create finds the book by key.
	if again := putBook(t, p, protocol.TargetPutLorebookParams{Key: "live:" + stamp + ":book", Lorebook: mustJSON(t, book)}); again != bookID {
		t.Errorf("re-run with the key made %s", again)
	}
}

func liveCard(t *testing.T, p *plugin, id string) map[string]any {
	t.Helper()
	res, err := p.get(context.Background(), mustJSON(t, protocol.TargetGetParams{RemoteID: id}))
	if err != nil {
		t.Fatal(err)
	}
	var c struct {
		Data map[string]any `json:"data"`
	}
	_ = json.Unmarshal(res.(protocol.TargetGetResult).Card, &c)
	return c.Data
}
