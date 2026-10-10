package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/davidgibbons/innkeeper-plugins/catalogkit"
	"github.com/davidgibbons/innkeeper-plugins/internal/pgtest"
	"github.com/davidgibbons/innkeeper/protocol"
)

var t0 = time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)

// testEnv is a folder holding a PNG card, a JSON card, a lorebook, and a
// file that's neither, in subfolders.
func testEnv(t *testing.T) *env {
	t.Helper()
	return testEnvAt(t, pgtest.URL(t), "test")
}

// testEnvAt is testEnv as instance on the schema at url.
func testEnvAt(t *testing.T, url, instance string) *env {
	t.Helper()
	dir := t.TempDir()
	copyFile(t, "../card/testdata/v2-seraphina.png", filepath.Join(dir, "fantasy/elves/seraphina.png"))
	copyFile(t, "../card/testdata/v3-mirelle.json", filepath.Join(dir, "fantasy/mirelle.json"))
	copyFile(t, "../card/testdata/eldoria.lorebook.json", filepath.Join(dir, "eldoria.json"))
	writeFile(t, filepath.Join(dir, "notes.json"), `{"todo": true}`)
	store, err := catalogkit.Open(context.Background(), url, instance, catalogConfig)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(store.Close)
	return &env{store: store, folder: dir, blobTmp: t.TempDir()}
}

func copyFile(t *testing.T, from, to string) {
	t.Helper()
	b, err := os.ReadFile(from)
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, to, string(b))
}

func writeFile(t *testing.T, path, s string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(s), 0o644); err != nil {
		t.Fatal(err)
	}
}

// runIngest ingests from checkpoint and returns the counts and the last
// checkpoint sent.
func runIngest(t *testing.T, e *env, checkpoint string) (protocol.CatalogIngestResult, string) {
	t.Helper()
	var last string
	notify := func(method string, params any) error {
		if cp, ok := params.(protocol.Checkpoint); ok {
			last = string(cp.Checkpoint)
		}
		return nil
	}
	// An empty RawMessage fails to marshal, so leave it nil.
	ip := protocol.CatalogIngestParams{Run: "r"}
	if checkpoint != "" {
		ip.Checkpoint = json.RawMessage(checkpoint)
	}
	params, _ := json.Marshal(ip)
	res, err := ingest(context.Background(), e, notify, params)
	if err != nil {
		t.Fatal(err)
	}
	return res, last
}

func ids(t *testing.T, e *env, p protocol.CatalogSearchParams) []string {
	t.Helper()
	p.Sort = "name"
	res, err := e.store.Search(context.Background(), p)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, it := range res.Items {
		out = append(out, it.ID)
	}
	return out
}

func TestIngest(t *testing.T) {
	e := testEnv(t)
	ctx := context.Background()

	res, last := runIngest(t, e, "")
	if want := (protocol.CatalogIngestResult{Added: 3, Skipped: 1}); res != want {
		t.Fatalf("first ingest = %+v, want %+v", res, want)
	}
	if last != `{"last":"notes.json"}` {
		t.Fatalf("last checkpoint = %s", last)
	}
	got := ids(t, e, protocol.CatalogSearchParams{Filters: map[string]json.RawMessage{"subdirectory": json.RawMessage(`"fantasy"`)}})
	if slices.Sort(got); !slices.Equal(got, []string{"fantasy/elves/seraphina.png", "fantasy/mirelle.json"}) {
		t.Fatalf("fantasy holds %v", got)
	}

	if res, _ := runIngest(t, e, ""); res != (protocol.CatalogIngestResult{}) {
		t.Fatalf("unchanged re-ingest = %+v, want nothing", res)
	}

	// Change a card, delete the lorebook.
	path := filepath.Join(e.folder, "fantasy/mirelle.json")
	b, _ := os.ReadFile(path)
	var card map[string]any
	_ = json.Unmarshal(b, &card)
	card["data"].(map[string]any)["description"] = "Changed."
	b, _ = json.Marshal(card)
	writeFile(t, path, string(b))
	later := time.Now().Add(time.Minute)
	if err := os.Chtimes(path, later, later); err != nil {
		t.Fatal(err)
	}
	eldoria, _ := os.ReadFile(filepath.Join(e.folder, "eldoria.json"))
	if err := os.Remove(filepath.Join(e.folder, "eldoria.json")); err != nil {
		t.Fatal(err)
	}
	if res, _ := runIngest(t, e, ""); res != (protocol.CatalogIngestResult{Updated: 1, Removed: 1}) {
		t.Fatalf("after a change and a delete = %+v", res)
	}
	changed, err := e.store.Get(ctx, "fantasy/mirelle.json", "")
	if err != nil || changed.Version != "2" || len(changed.Versions) != 2 {
		t.Fatalf("changed card: version %q of %v, err %v", changed.Version, changed.Versions, err)
	}
	if slices.Contains(ids(t, e, protocol.CatalogSearchParams{}), "eldoria.json") {
		t.Fatal("search still lists the deleted lorebook")
	}
	if _, err := e.store.Get(ctx, "eldoria.json", ""); err != nil {
		t.Fatalf("get of a removed item: %v", err)
	}

	// The same file coming back returns the item.
	writeFile(t, filepath.Join(e.folder, "eldoria.json"), string(eldoria))
	if res, _ := runIngest(t, e, ""); res != (protocol.CatalogIngestResult{Updated: 1}) {
		t.Fatalf("after restoring the lorebook = %+v", res)
	}
}

func TestIngestResumes(t *testing.T) {
	e := testEnv(t)
	// Files sort as eldoria.json, fantasy/elves/seraphina.png, fantasy/mirelle.json, notes.json.
	res, _ := runIngest(t, e, `{"last":"fantasy/elves/seraphina.png"}`)
	if want := (protocol.CatalogIngestResult{Added: 1, Skipped: 1}); res != want {
		t.Fatalf("resumed ingest = %+v, want %+v", res, want)
	}
}

func TestIngestNeedsAFolder(t *testing.T) {
	e := testEnv(t)
	e.folder = filepath.Join(e.folder, "missing")
	params, _ := json.Marshal(protocol.CatalogIngestParams{Run: "r"})
	_, err := ingest(context.Background(), e, func(string, any) error { return nil }, params)
	var rpc *protocol.Error
	if !errors.As(err, &rpc) || rpc.Code != protocol.CodeInvalidParams {
		t.Fatalf("err = %v, want -32602", err)
	}
}

// Two instances share the plugin's schema; one's ingest leaves the other's items alone.
func TestIngestKeepsInstancesApart(t *testing.T) {
	url := pgtest.URL(t)
	a, b := testEnvAt(t, url, "a"), testEnvAt(t, url, "b")
	// Same mtimes, so a file row shared between instances would pass b's files as unchanged.
	for _, e := range []*env{a, b} {
		err := filepath.WalkDir(e.folder, func(p string, _ os.DirEntry, err error) error {
			return errors.Join(err, os.Chtimes(p, t0, t0))
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	runIngest(t, a, "")
	if err := os.Remove(filepath.Join(b.folder, "eldoria.json")); err != nil {
		t.Fatal(err)
	}
	if res, _ := runIngest(t, b, ""); res != (protocol.CatalogIngestResult{Added: 2, Skipped: 1}) {
		t.Fatalf("second instance's ingest = %+v", res)
	}
	if got := ids(t, a, protocol.CatalogSearchParams{}); len(got) != 3 {
		t.Fatalf("first instance lists %v", got)
	}
}

// A subfolder ingest can't read hides its files, which aren't gone.
func TestIngestSkipsSweepAfterPartialWalk(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root reads a folder without permission")
	}
	e := testEnv(t)
	runIngest(t, e, "")
	dir := filepath.Join(e.folder, "fantasy/elves")
	if err := os.Chmod(dir, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(dir, 0o755) })
	if res, _ := runIngest(t, e, ""); res != (protocol.CatalogIngestResult{}) {
		t.Fatalf("ingest with an unreadable subfolder = %+v, want nothing", res)
	}
}
