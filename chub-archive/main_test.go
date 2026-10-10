package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/davidgibbons/innkeeper/protocol"
)

func getParams(id, version string) json.RawMessage {
	b, _ := json.Marshal(protocol.CatalogGetParams{ID: id, Version: version, Avatar: true})
	return b
}

func TestGetAvatar(t *testing.T) {
	e := testEnv(t)
	runIngest(t, e, "", nil)
	ctx := context.Background()

	for _, version := range []string{"", "5"} {
		res, err := get(ctx, e, getParams("chub_character_def:100", version))
		if err != nil {
			t.Fatal(err)
		}
		if res.Avatar == "" || filepath.Dir(res.Avatar) != e.blobTmp {
			t.Fatalf("version %q: avatar = %q, want a file in %s", version, res.Avatar, e.blobTmp)
		}
	}
	// Brin's image isn't in the archive; the lorebook has none.
	for _, id := range []string{"chub_character_def:101", "chub_lorebook_def:200"} {
		if res, err := get(ctx, e, getParams(id, "")); err != nil || res.Avatar != "" {
			t.Fatalf("%s: avatar = %q, err %v; want none", id, res.Avatar, err)
		}
	}

	// An image that can't be read leaves the item without an avatar, not an error.
	broken := t.TempDir()
	if err := os.MkdirAll(filepath.Join(broken, "a", "b", "c", "0000000000000000000000000000d"), 0o755); err != nil {
		t.Fatal(err)
	}
	e.images = []string{broken}
	if res, err := get(ctx, e, getParams("chub_character_def:100", "")); err != nil || res.Avatar != "" {
		t.Fatalf("unreadable image: avatar = %q, err %v; want none", res.Avatar, err)
	}
}
