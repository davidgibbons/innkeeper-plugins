package catalogkit

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/davidgibbons/innkeeper/cardhash"
	"github.com/davidgibbons/innkeeper/protocol"
)

func put(t *testing.T, s *Store, e Entry) {
	t.Helper()
	if _, err := s.Put(context.Background(), e); err != nil {
		t.Fatal(err)
	}
}

func wantCode(t *testing.T, err error, code int) {
	t.Helper()
	var rpc *protocol.Error
	if !errors.As(err, &rpc) || rpc.Code != code {
		t.Fatalf("err = %v, want code %d", err, code)
	}
}

func TestGet(t *testing.T) {
	ctx := context.Background()
	s := openTest(t)
	put(t, s, Entry{ID: "a", Kind: protocol.KindCard, Data: testCard("Ann", "One."), Updated: t0})
	put(t, s, Entry{ID: "a", Kind: protocol.KindCard, Data: testCard("Ann", "Two."), Updated: t0.Add(time.Hour)})

	res, err := s.Get(ctx, "a", "")
	if err != nil {
		t.Fatal(err)
	}
	if res.Version != "2" || len(res.Versions) != 2 || res.Versions[0].Version != "2" ||
		res.Versions[1].Created != "2026-01-02T03:04:05Z" {
		t.Fatalf("got version %q of %+v", res.Version, res.Versions)
	}
	data, _, _ := cardhash.CardData(res.Data)
	if hash, _ := cardhash.ContentHash(data); hash != res.Item.Hashes.ContentHash {
		t.Fatal("latest data doesn't hash to the item's content_hash")
	}

	old, err := s.Get(ctx, "a", "1")
	if err != nil || old.Version != "1" {
		t.Fatalf("version 1: %v, %v", old.Version, err)
	}
	_, err = s.Get(ctx, "a", "9")
	wantCode(t, err, protocol.CodeRemoteNotFound)
	_, err = s.Get(ctx, "missing", "")
	wantCode(t, err, protocol.CodeRemoteNotFound)
}
func searchIDs(t *testing.T, s *Store, p protocol.CatalogSearchParams) []string {
	t.Helper()
	var ids []string
	for {
		res, err := s.Search(context.Background(), p)
		if err != nil {
			t.Fatal(err)
		}
		for _, it := range res.Items {
			ids = append(ids, it.ID)
		}
		if res.Next == "" {
			return ids
		}
		p.Page.Cursor = res.Next
	}
}

func seed(t *testing.T) *Store {
	s := openTest(t)
	put(t, s, Entry{ID: "c1", Kind: protocol.KindCard, Data: testCard("Bram the Smith", "A dwarf blacksmith."),
		Attrs: map[string][]string{"dir": {"dwarves"}}, Updated: t0})
	put(t, s, Entry{ID: "c2", Kind: protocol.KindCard, Data: testCard("Aria", "An elf ranger who hunts with a smith's bow."),
		Attrs: map[string][]string{"dir": {"elves"}}, Updated: t0.Add(48 * time.Hour)})
	put(t, s, Entry{ID: "c3", Kind: protocol.KindCard, Data: testCard("Cole", "A sailor."), Updated: t0.Add(24 * time.Hour)})
	put(t, s, Entry{ID: "l1", Kind: protocol.KindLorebook,
		Data: json.RawMessage(`{"name":"Forge lore","description":"Anvils.","entries":[]}`), Updated: t0})
	return s
}

func TestSearch(t *testing.T) {
	s := seed(t)
	tests := []struct {
		name string
		p    protocol.CatalogSearchParams
		want []string
	}{
		{"all, newest first", protocol.CatalogSearchParams{}, []string{"c2", "c3", "l1", "c1"}},
		{"name", protocol.CatalogSearchParams{Sort: "name"}, []string{"c2", "c1", "c3", "l1"}},
		{"relevance ranks a name match first", protocol.CatalogSearchParams{Q: "smith"}, []string{"c1", "c2"}},
		{"kind", protocol.CatalogSearchParams{Kind: protocol.KindLorebook}, []string{"l1"}},
		{"tag", protocol.CatalogSearchParams{Filters: map[string]json.RawMessage{"tag": json.RawMessage(`"FANTASY"`)}}, []string{"c2", "c3", "c1"}},
		{"attrs", protocol.CatalogSearchParams{Filters: map[string]json.RawMessage{"dir": json.RawMessage(`"elves"`)}}, []string{"c2"}},
		{"date range", protocol.CatalogSearchParams{Filters: map[string]json.RawMessage{
			"changed": json.RawMessage(`{"from":"2026-01-03","to":"2026-01-03"}`)}}, []string{"c3"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for _, size := range []int{0, 1} { // one page, then pages of one
				tt.p.Page.Size = size
				if got := searchIDs(t, s, tt.p); !slices.Equal(got, tt.want) {
					t.Fatalf("size %d: got %v, want %v", size, got, tt.want)
				}
			}
		})
	}
}

func TestSearchRefuses(t *testing.T) {
	s := seed(t)
	ctx := context.Background()
	_, err := s.Search(ctx, protocol.CatalogSearchParams{Filters: map[string]json.RawMessage{"nope": json.RawMessage(`"x"`)}})
	wantCode(t, err, protocol.CodeInvalidParams)

	res, err := s.Search(ctx, protocol.CatalogSearchParams{Sort: "name", Page: protocol.CatalogPage{Size: 1}})
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.Search(ctx, protocol.CatalogSearchParams{Sort: "newest", Page: protocol.CatalogPage{Size: 1, Cursor: res.Next}})
	wantCode(t, err, protocol.CodeInvalidParams)
	_, err = s.Search(ctx, protocol.CatalogSearchParams{Page: protocol.CatalogPage{Cursor: "garbage"}})
	wantCode(t, err, protocol.CodeInvalidParams)
}

func TestSearchSkipsRemoved(t *testing.T) {
	s := seed(t)
	if _, err := s.MarkRemovedExcept(context.Background(), []string{"c1"}); err != nil {
		t.Fatal(err)
	}
	if got := searchIDs(t, s, protocol.CatalogSearchParams{}); !slices.Equal(got, []string{"c1"}) {
		t.Fatalf("got %v", got)
	}
}
