package catalogkit

import (
	"context"
	"errors"
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
