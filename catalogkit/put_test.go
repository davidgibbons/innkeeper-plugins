package catalogkit

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"image"
	"image/color"
	"math"
	"math/rand/v2"
	"slices"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/davidgibbons/innkeeper/protocol"
)

func testCard(name, description string) json.RawMessage {
	b, _ := json.Marshal(map[string]any{"spec": "chara_card_v3", "spec_version": "3.0", "data": map[string]any{
		"name": name, "creator": "ann", "description": description, "tags": []string{"Fantasy", " elf "},
		"creator_notes": "A test card."}})
	return b
}

func testImage() image.Image {
	img := image.NewRGBA(image.Rect(0, 0, 400, 600))
	for y := range 600 {
		for x := range 400 {
			img.Set(x, y, color.RGBA{uint8(x), uint8(y), 128, 255})
		}
	}
	return img
}

var t0 = time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)

func TestPutVersions(t *testing.T) {
	ctx := context.Background()
	s := openTest(t)
	e := Entry{ID: "a.json", Kind: protocol.KindCard, Data: testCard("Ann", "One."), Image: testImage(), Updated: t0}

	steps := []struct {
		name string
		edit func()
		want Change
	}{
		{"new", func() {}, Added},
		{"same data", func() { e.Attrs = map[string][]string{"dir": {"x"}} }, Unchanged},
		{"changed data", func() { e.Data = testCard("Ann", "Two."); e.Updated = t0.Add(time.Hour) }, Updated},
	}
	for _, st := range steps {
		st.edit()
		got, err := s.Put(ctx, e)
		if err != nil {
			t.Fatalf("%s: %v", st.name, err)
		}
		if got != st.want {
			t.Fatalf("%s: change = %v, want %v", st.name, got, st.want)
		}
	}

	var name, author, latest string
	var tags []string
	var thumb []byte
	var phash *int64
	err := s.pool.QueryRow(ctx, `SELECT name, author, tags, latest_version, thumbnail, image_phash FROM item WHERE id = 'a.json'`).
		Scan(&name, &author, &tags, &latest, &thumb, &phash)
	if err != nil {
		t.Fatal(err)
	}
	if name != "Ann" || author != "ann" || latest != "2" || len(tags) != 2 || tags[0] != "fantasy" || tags[1] != "elf" {
		t.Fatalf("item = %q %q %v %q", name, author, tags, latest)
	}
	if len(thumb) == 0 || len(thumb) > protocol.MaxThumbnail || phash == nil {
		t.Fatalf("thumbnail %d bytes, phash %v", len(thumb), phash)
	}
	cfg, _, err := image.DecodeConfig(bytesReader(thumb))
	if err != nil || cfg.Width != 160 {
		t.Fatalf("thumbnail is %d wide, err %v", cfg.Width, err)
	}
}

func TestPutRefusesBadData(t *testing.T) {
	s := openTest(t)
	_, err := s.Put(context.Background(), Entry{ID: "x", Kind: protocol.KindCard, Data: json.RawMessage(`{"spec":"chara_card_v3"}`), Updated: t0})
	if !errors.Is(err, ErrBadData) {
		t.Fatalf("err = %v, want ErrBadData", err)
	}
}

func TestMarkRemovedExcept(t *testing.T) {
	ctx := context.Background()
	s := openTest(t)
	for _, id := range []string{"a", "b"} {
		if _, err := s.Put(ctx, Entry{ID: id, Kind: protocol.KindCard, Data: testCard(id, id), Updated: t0}); err != nil {
			t.Fatal(err)
		}
	}
	if n, err := s.MarkRemovedExcept(ctx, []string{"a"}); err != nil || n != 1 {
		t.Fatalf("marked %d, err %v; want 1", n, err)
	}
	// Putting a removed item back is a change even with the same data.
	if c, err := s.Put(ctx, Entry{ID: "b", Kind: protocol.KindCard, Data: testCard("b", "b"), Updated: t0}); err != nil || c != Updated {
		t.Fatalf("change = %v, err %v; want Updated", c, err)
	}
	// An empty keep list removes everything.
	if n, err := s.MarkRemovedExcept(ctx, nil); err != nil || n != 2 {
		t.Fatalf("marked %d, err %v; want 2", n, err)
	}
}

// Hashes within MaxDistance bits share a band; one further apart may not.
func TestBands(t *testing.T) {
	h := int64(0x0123456789abcdef)
	var near int64 = h
	for b := range MaxDistance {
		near ^= 1 << (7 * b) // one bit in each of the first 8 bands
	}
	if !shareBand(lsh(&h, nil), lsh(&near, nil)) {
		t.Fatal("hashes 8 bits apart share no band")
	}
	far := near ^ 1<<(7*MaxDistance) // and one in the last band
	if shareBand(lsh(&h, nil), lsh(&far, nil)) {
		t.Fatal("hashes with a bit off in every band share a band")
	}
	if shareBand(lsh(&h, nil), lsh(nil, &h)) {
		t.Fatal("a simhash and a phash share a band")
	}
}

func shareBand(a, b []int32) bool {
	for _, x := range a {
		for _, y := range b {
			if x == y {
				return true
			}
		}
	}
	return false
}

func bytesReader(b []byte) *bytes.Reader { return bytes.NewReader(b) }

func TestPutVersionsOnEmbeddedLorebookChange(t *testing.T) {
	ctx := context.Background()
	s := openTest(t)
	e := Entry{ID: "book.json", Kind: protocol.KindCard, Data: testCard("Ann", "One."), Updated: t0}
	if _, err := s.Put(ctx, e); err != nil {
		t.Fatal(err)
	}
	var card map[string]any
	_ = json.Unmarshal(e.Data, &card)
	card["data"].(map[string]any)["character_book"] = map[string]any{"entries": []any{}, "name": "lore"}
	e.Data, _ = json.Marshal(card)
	if c, err := s.Put(ctx, e); err != nil || c != Updated {
		t.Fatalf("change = %v, err %v; want Updated", c, err)
	}
	var latest string
	if err := s.pool.QueryRow(ctx, `SELECT latest_version FROM item WHERE id = 'book.json'`).Scan(&latest); err != nil || latest != "2" {
		t.Fatalf("latest_version = %q, err %v; want 2", latest, err)
	}
}

// Postgres refuses a NUL in text and invalid UTF-8 in json; both are the entry's fault.
func TestPutRefusesDataPostgresRejects(t *testing.T) {
	card := func(name string) json.RawMessage {
		return json.RawMessage(`{"spec":"chara_card_v3","spec_version":"3.0","data":{"name":"` + name + `"}}`)
	}
	for name, data := range map[string]json.RawMessage{
		"NUL escape":    card(`a\u0000b`),
		"invalid UTF-8": card("a\xffb"),
	} {
		t.Run(name, func(t *testing.T) {
			s := openTest(t)
			_, err := s.Put(context.Background(), Entry{ID: "x", Kind: protocol.KindCard, Data: data, Updated: t0})
			if !errors.Is(err, ErrBadData) {
				t.Fatalf("err = %v, want ErrBadData", err)
			}
		})
	}
}

// randomWords returns n distinct-looking words of random letters, which
// neither compress nor stem together.
func randomWords(n int) string {
	var b strings.Builder
	for range n {
		for range 8 {
			b.WriteByte(byte('a' + rand.IntN(26)))
		}
		b.WriteByte(' ')
	}
	return b.String()
}

// A long name is kept, cut short; it would otherwise overflow the name index.
func TestPutTruncatesLongName(t *testing.T) {
	ctx := context.Background()
	s := openTest(t)
	name := randomWords(600) // about 5 KB
	put(t, s, Entry{ID: "long", Kind: protocol.KindCard, Data: testCard(name, "One."), Updated: t0})
	var got string
	if err := s.pool.QueryRow(ctx, `SELECT name FROM item WHERE id = 'long'`).Scan(&got); err != nil {
		t.Fatal(err)
	}
	if n := utf8.RuneCountInString(got); n > 501 || !strings.HasPrefix(name, strings.TrimSuffix(got, "…")) {
		t.Fatalf("name is %d runes: %.40q…", n, got)
	}
}

// A description past Postgres's tsvector limit is the entry's fault, not the ingest's.
func TestPutRefusesHugeDescription(t *testing.T) {
	s := openTest(t)
	_, err := s.Put(context.Background(), Entry{ID: "huge", Kind: protocol.KindCard, Data: testCard("Huge", randomWords(200_000)), Updated: t0})
	if !errors.Is(err, ErrBadData) {
		t.Fatalf("err = %v, want ErrBadData", err)
	}
}

func TestPutOverrides(t *testing.T) {
	ctx := context.Background()
	s := openTest(t)
	early := t0.Add(-24 * time.Hour)
	put(t, s, Entry{ID: "o", Kind: protocol.KindCard, Data: testCard("Ann", "One."), Updated: t0,
		Author: " Zed ", Tags: []string{"Sci-Fi", " "}, Added: t0})
	// A later put with an earlier Added moves the date back; a zero Added keeps it.
	put(t, s, Entry{ID: "o", Kind: protocol.KindCard, Data: testCard("Ann", "One."), Updated: t0, Added: early})
	put(t, s, Entry{ID: "o", Kind: protocol.KindCard, Data: testCard("Ann", "One."), Updated: t0})

	var author string
	var tags []string
	var added time.Time
	if err := s.pool.QueryRow(ctx, `SELECT author, tags, added FROM item WHERE id = 'o'`).Scan(&author, &tags, &added); err != nil {
		t.Fatal(err)
	}
	// The last put had no overrides, so the card's own creator and tags are back.
	if author != "ann" || !slices.Equal(tags, []string{"fantasy", "elf"}) || !added.Equal(early) {
		t.Fatalf("author %q, tags %v, added %v", author, tags, added)
	}

	put(t, s, Entry{ID: "p", Kind: protocol.KindCard, Data: testCard("Bo", "Two."), Updated: t0,
		Author: " Zed ", Tags: []string{"Sci-Fi", " "}})
	if err := s.pool.QueryRow(ctx, `SELECT author, tags FROM item WHERE id = 'p'`).Scan(&author, &tags); err != nil {
		t.Fatal(err)
	}
	if author != "Zed" || !slices.Equal(tags, []string{"sci-fi"}) {
		t.Fatalf("author %q, tags %v", author, tags)
	}
}

// Versions the plugin numbers can arrive out of order; the highest is latest.
func TestPutPluginVersions(t *testing.T) {
	ctx := context.Background()
	s := openTest(t)
	e := func(v int, name string, added time.Time) Entry {
		return Entry{ID: "v", Kind: protocol.KindCard, Data: testCard(name, "Same."), Updated: added, Added: added, Version: v}
	}
	steps := []struct {
		entry Entry
		want  Change
	}{
		{e(5, "Five", t0), Added},
		{e(9, "Nine", t0.Add(time.Hour)), Updated},
		{e(7, "Seven", t0.Add(-time.Hour)), Updated}, // older, arriving late
		{e(9, "Nine", t0.Add(time.Hour)), Unchanged},
		{e(5, "Five", t0), Unchanged},
	}
	for i, st := range steps {
		got, err := s.Put(ctx, st.entry)
		if err != nil {
			t.Fatalf("step %d: %v", i, err)
		}
		if got != st.want {
			t.Fatalf("step %d: change = %v, want %v", i, got, st.want)
		}
	}
	res, err := s.Get(ctx, "v", "")
	if err != nil {
		t.Fatal(err)
	}
	var versions []string
	for _, v := range res.Versions {
		versions = append(versions, v.Version)
	}
	if res.Item.Name != "Nine" || res.Version != "9" || !slices.Equal(versions, []string{"9", "7", "5"}) {
		t.Fatalf("item %q at %q, versions %v", res.Item.Name, res.Version, versions)
	}
	if old, err := s.Get(ctx, "v", "7"); err != nil || !bytes.Contains(old.Data, []byte(`"Seven"`)) {
		t.Fatalf("version 7: data %s, err %v", old.Data, err)
	}
	var added time.Time
	if err := s.pool.QueryRow(ctx, `SELECT added FROM item WHERE id = 'v'`).Scan(&added); err != nil || !added.Equal(t0.Add(-time.Hour)) {
		t.Fatalf("added = %v, %v; want the late version's earlier date", added, err)
	}
	// New data under a stored number changes neither the version nor the item.
	if got, err := s.Put(ctx, e(9, "Ten", t0.Add(time.Hour))); err != nil || got != Unchanged {
		t.Fatalf("reused number: %v, %v", got, err)
	}
	if again, err := s.Get(ctx, "v", ""); err != nil || again.Item.Name != "Nine" {
		t.Fatalf("after reused number: %q, %v", again.Item.Name, err)
	}
	// An older new version of a removed item brings it back.
	if _, err := s.MarkRemovedExcept(ctx, nil); err != nil {
		t.Fatal(err)
	}
	if got, err := s.Put(ctx, e(3, "Three", t0.Add(-2*time.Hour))); err != nil || got != Updated {
		t.Fatalf("older version of removed item: %v, %v", got, err)
	}
	if ids := searchIDs(t, s, protocol.CatalogSearchParams{Sort: "name"}); !slices.Equal(ids, []string{"v"}) {
		t.Fatalf("search after restore: %v", ids)
	}
	if _, err := s.Put(ctx, Entry{ID: "w", Kind: protocol.KindCard, Data: testCard("W", "W."), Updated: t0, Version: math.MaxInt32 + 1}); !errors.Is(err, ErrBadData) {
		t.Fatalf("huge version: err = %v, want ErrBadData", err)
	}
	if _, err := s.Put(ctx, Entry{ID: "w", Kind: protocol.KindCard, Data: testCard("W", "W."), Updated: t0, Version: -1}); !errors.Is(err, ErrBadData) {
		t.Fatalf("negative version: err = %v, want ErrBadData", err)
	}
}
