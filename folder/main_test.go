package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/davidgibbons/innkeeper/protocol"
)

func getParams(id, version string) json.RawMessage {
	b, _ := json.Marshal(protocol.CatalogGetParams{ID: id, Version: version, Avatar: true})
	return b
}

func TestGetAvatar(t *testing.T) {
	e := testEnv(t)
	runIngest(t, e, "")
	const id = "fantasy/elves/seraphina.png"

	res, err := get(context.Background(), e, getParams(id, ""))
	if err != nil {
		t.Fatal(err)
	}
	if res.Avatar == "" || filepath.Dir(res.Avatar) != e.blobTmp {
		t.Fatalf("avatar = %q, want a file in %s", res.Avatar, e.blobTmp)
	}

	// After the file changes, the stored version's avatar is gone.
	later := time.Now().Add(time.Minute)
	if err := os.Chtimes(filepath.Join(e.folder, id), later, later); err != nil {
		t.Fatal(err)
	}
	if res, err = get(context.Background(), e, getParams(id, "")); err != nil || res.Avatar != "" {
		t.Fatalf("avatar = %q, err %v; want none", res.Avatar, err)
	}
}
