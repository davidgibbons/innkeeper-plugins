package catalogkit

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"image"
	"image/color"
	"testing"
	"time"

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
