package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/inkandale/innkeeper/protocol"
)

func getParams(id, version string) json.RawMessage {
	b, _ := json.Marshal(protocol.CatalogGetParams{ID: id, Version: version, Avatar: true})
	return b
}

func TestGetAvatar(t *testing.T) {
	e := testEnv(t)
	runIngest(t, e, "", nil)
	e.images = nil // The takeout is unmounted after ingest.
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

	// An image missing from the store leaves the item without an avatar, not an error.
	if err := os.Remove(findStored(e.imageStore, fixtureHash)); err != nil {
		t.Fatal(err)
	}
	if res, err := get(ctx, e, getParams("chub_character_def:100", "")); err != nil || res.Avatar != "" {
		t.Fatalf("missing image: avatar = %q, err %v; want none", res.Avatar, err)
	}
}

func TestConfigImages(t *testing.T) {
	if got, err := configImages(map[string]any{"images": []any{"/a", "", "/b"}}); err != nil || !slices.Equal(got, []string{"/a", "/b"}) {
		t.Fatalf("images = %v, %v", got, err)
	}
	if got, err := configImages(map[string]any{}); err != nil || got != nil {
		t.Fatalf("no images = %v, %v", got, err)
	}
	for _, bad := range []any{"/a", []any{"/a", 3}} {
		if _, err := configImages(map[string]any{"images": bad}); err == nil || !strings.Contains(err.Error(), "images") {
			t.Errorf("images %v: err = %v, want one naming images", bad, err)
		}
	}
}
