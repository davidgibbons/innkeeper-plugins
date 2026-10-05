package main

import (
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/davidgibbons/innkeeper-plugins/card"
	"github.com/davidgibbons/innkeeper/protocol"
)

// TestLive runs against a real SillyTavern server. It pushes SillyTavern's
// Seraphina sample, renamed so runs don't collide, with her lorebook as a
// world file, reads both back, pushes both again, and lists them. The plugin
// can't delete, so each run leaves its character and world file behind.
//
//	SILLYTAVERN_URL=http://localhost:18000 SILLYTAVERN_AUTH=basic SILLYTAVERN_USERNAME=… SILLYTAVERN_PASSWORD=… \
//	  go test -run TestLive -v ./sillytavern/
//
// SILLYTAVERN_AUTH is none, basic, or account, and defaults to none.
func TestLive(t *testing.T) {
	base := os.Getenv("SILLYTAVERN_URL")
	if base == "" {
		t.Skip("set SILLYTAVERN_URL, and SILLYTAVERN_AUTH, SILLYTAVERN_USERNAME, SILLYTAVERN_PASSWORD")
	}
	ctx := context.Background()
	p := &plugin{newClient(base, cmp.Or(os.Getenv("SILLYTAVERN_AUTH"), "none"),
		os.Getenv("SILLYTAVERN_USERNAME"), os.Getenv("SILLYTAVERN_PASSWORD"))}
	stamp := time.Now().Format("20060102-150405.000")
	name := "Innkeeper Live Test " + stamp

	file, err := os.ReadFile("../card/testdata/v3-seraphina.png")
	if err != nil {
		t.Fatal(err)
	}
	raw, img, err := card.Decode(file)
	if err != nil {
		t.Fatal(err)
	}
	var in struct {
		Data map[string]any `json:"data"`
	}
	if err := json.Unmarshal(raw, &in); err != nil {
		t.Fatal(err)
	}
	data := in.Data
	data["name"] = name
	book := object(data["character_book"])
	book["name"] = name + " lore"
	delete(data, "character_book")
	sum := sha256.Sum256(img)
	avatar := filepath.Join(t.TempDir(), hex.EncodeToString(sum[:]))
	if err := os.WriteFile(avatar, img, 0o644); err != nil {
		t.Fatal(err)
	}

	// The lorebook first, as Innkeeper pushes it, then the card linked to it.
	bookKey, cardKey := "live:"+stamp+":book", "live:"+stamp+":card"
	bookID := putBook(t, p, bookKey, book)
	cardIn := protocol.TargetPutParams{Key: cardKey, Avatar: avatar, LorebookRemoteIDs: []string{bookID},
		Card: mustJSON(t, map[string]any{"spec": "chara_card_v3", "spec_version": "3.0", "data": data})}
	id := put(t, p, cardIn)
	t.Logf("character %s, world file %s", id, bookID)

	// What get returns: the card without SillyTavern's own extensions.
	want := normal(t, data).(map[string]any)
	ext := object(want["extensions"])
	delete(ext, "fav")
	delete(ext, "world")
	check := func(when string) {
		t.Helper()
		if got := getBook(t, p, bookID); !reflect.DeepEqual(got, normal(t, book)) {
			t.Errorf("%s: lorebook read back as %s, want %s", when, mustJSON(t, got), mustJSON(t, book))
		}
		if got := liveCard(t, p, id); !reflect.DeepEqual(got, want) {
			t.Errorf("%s: card read back as %s, want %s", when, mustJSON(t, got), mustJSON(t, want))
		}
		c, err := p.read(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		if w := object(object(c["data"])["extensions"])["world"]; w != bookID {
			t.Errorf("%s: character links world %v, want %s", when, w, bookID)
		}
	}
	check("after create")

	// A second push updates both in place, by remote ID and by key.
	if again := putBookRemote(t, p, bookID, book); again != bookID {
		t.Errorf("lorebook update made %s, want %s", again, bookID)
	}
	if again := putBook(t, p, bookKey, book); again != bookID {
		t.Errorf("lorebook re-run with the key made %s, want %s", again, bookID)
	}
	cardIn.RemoteID = id
	if again := put(t, p, cardIn); again != id {
		t.Errorf("card update made %s, want %s", again, id)
	}
	cardIn.RemoteID = ""
	if again := put(t, p, cardIn); again != id {
		t.Errorf("card re-run with the key made %s, want %s", again, id)
	}
	check("after update")

	chars, err := p.list(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(chars.Items, protocol.TargetItem{RemoteID: id, Name: name}) {
		t.Errorf("%s missing from %d listed characters", id, len(chars.Items))
	}
	books, err := p.listLorebooks(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(books.Items, protocol.TargetItem{RemoteID: bookID, Name: name + " lore"}) {
		t.Errorf("%s missing from listed world files %v", bookID, books.Items)
	}
}

func putBookRemote(t *testing.T, p *plugin, id string, book map[string]any) string {
	t.Helper()
	got, err := putBookErr(p, protocol.TargetPutLorebookParams{RemoteID: id, Lorebook: mustJSON(t, book)})
	if err != nil {
		t.Fatal(err)
	}
	return got
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
	if err := json.Unmarshal(res.(protocol.TargetGetResult).Card, &c); err != nil {
		t.Fatal(err)
	}
	return c.Data
}
