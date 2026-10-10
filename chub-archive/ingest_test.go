package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/davidgibbons/innkeeper-plugins/catalogkit"
	"github.com/davidgibbons/innkeeper-plugins/internal/pgtest"
	"github.com/davidgibbons/innkeeper/protocol"
)

func testEnv(t *testing.T) *env {
	t.Helper()
	store, err := catalogkit.Open(context.Background(), pgtest.URL(t), "test", catalogConfig)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(store.Close)
	return &env{store: store, dump: "testdata/dump.sql", images: []string{"testdata/images"}, blobTmp: t.TempDir()}
}

// runIngest ingests from checkpoint. stop, if set, ends the run with an
// error when it returns true for a checkpoint. It returns the counts, the
// checkpoints sent, and the run's error.
func runIngest(t *testing.T, e *env, checkpoint string, stop func(cp string) bool) (protocol.CatalogIngestResult, []string, error) {
	t.Helper()
	var cps []string
	errStop := errors.New("stopped")
	notify := func(method string, params any) error {
		if cp, ok := params.(protocol.Checkpoint); ok {
			cps = append(cps, string(cp.Checkpoint))
			if stop != nil && stop(string(cp.Checkpoint)) {
				return errStop
			}
		}
		return nil
	}
	ip := protocol.CatalogIngestParams{Run: "r"}
	if checkpoint != "" {
		ip.Checkpoint = json.RawMessage(checkpoint)
	}
	params, _ := json.Marshal(ip)
	res, err := ingest(context.Background(), e, notify, params)
	if err != nil && !errors.Is(err, errStop) {
		t.Fatal(err)
	}
	return res, cps, err
}

func search(t *testing.T, e *env, filters string) []string {
	t.Helper()
	p := protocol.CatalogSearchParams{Sort: "name"}
	if filters != "" {
		_ = json.Unmarshal([]byte(filters), &p.Filters)
	}
	res, err := e.store.Search(context.Background(), p)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, it := range res.Items {
		out = append(out, it.Name)
	}
	return out
}

func TestIngest(t *testing.T) {
	e := testEnv(t)
	res, cps, _ := runIngest(t, e, "", nil)
	if want := (protocol.CatalogIngestResult{Added: 8, Updated: 1, Skipped: 1}); res != want {
		t.Fatalf("first ingest = %+v, want %+v", res, want)
	}
	var last checkpoint
	_ = json.Unmarshal([]byte(cps[len(cps)-1]), &last)
	if last.Table != "" || last.Offset == 0 {
		t.Fatalf("last checkpoint = %+v, want the end of the dump", last)
	}

	all := search(t, e, "")
	if want := []string{"Aria", "Ava", "Brin", "Cora", "Eldoria", "Esther", "Nina", "Vergil"}; !slices.Equal(all, want) {
		t.Fatalf("items = %v, want %v", all, want)
	}
	for filters, want := range map[string][]string{
		`{"site": "chub"}`:                {"Aria", "Brin", "Eldoria"},
		`{"site": "generic"}`:             {"Vergil"},
		`{"hidden": "true"}`:              {"Brin"},
		`{"rating": {"min": 4}}`:          {"Aria"},
		`{"author": "ANN"}`:               {"Aria", "Eldoria"},
		`{"tag": "elf"}`:                  {"Aria"}, // chub's topics, not the card's tags
		`{"added": {"to": "2024-05-06"}}`: {"Aria"}, // chub_character's date, not the version's
	} {
		if got := search(t, e, filters); !slices.Equal(got, want) {
			t.Errorf("%s: got %v, want %v", filters, got, want)
		}
	}

	got, err := e.store.Get(context.Background(), "chub_character_def:100", "")
	if err != nil {
		t.Fatal(err)
	}
	if got.Version != "9" || len(got.Versions) != 2 || got.Item.Hashes.ImagePhash == nil ||
		!strings.Contains(string(got.Data), `who says \"hi\".\nShe guards\tthe woods.`) {
		t.Fatalf("Aria = version %q of %d, phash %v, data %s", got.Version, len(got.Versions), got.Item.Hashes.ImagePhash, got.Data)
	}

	// Re-ingesting the same dump changes nothing; the bad row is skipped again.
	if res, _, _ := runIngest(t, e, "", nil); res != (protocol.CatalogIngestResult{Skipped: 1}) {
		t.Fatalf("re-ingest = %+v, want only the bad row skipped", res)
	}
}

// An ingest stopped in the middle of a block resumes from its checkpoint
// and ends with the same items as one that ran through.
func TestIngestResumes(t *testing.T) {
	old := checkpointEvery
	checkpointEvery = 1
	t.Cleanup(func() { checkpointEvery = old })

	e := testEnv(t)
	_, cps, err := runIngest(t, e, "", func(cp string) bool { return strings.Contains(cp, `"table":"chub_character_def"`) })
	if err == nil {
		t.Fatal("the run wasn't stopped inside chub_character_def")
	}
	res, _, _ := runIngest(t, e, cps[len(cps)-1], nil)
	// Aria's first version came before the stop; Brin, Eldoria, and four others after it.
	if want := (protocol.CatalogIngestResult{Added: 6, Updated: 1, Skipped: 1}); res != want {
		t.Fatalf("resumed ingest = %+v, want %+v", res, want)
	}
	if got := search(t, e, ""); len(got) != 8 {
		t.Fatalf("items = %v, want 8", got)
	}
}

func TestIngestNeedsTheDump(t *testing.T) {
	e := testEnv(t)
	e.dump = "testdata/missing.sql"
	params, _ := json.Marshal(protocol.CatalogIngestParams{Run: "r"})
	_, err := ingest(context.Background(), e, func(string, any) error { return nil }, params)
	var rpc *protocol.Error
	if !errors.As(err, &rpc) || rpc.Code != protocol.CodeInvalidParams {
		t.Fatalf("err = %v, want invalid params", err)
	}
}

// An image that can't be read fails the run, so a retry reads it again.
func TestIngestFailsOnUnreadableImage(t *testing.T) {
	e := testEnv(t)
	broken := t.TempDir()
	if err := os.MkdirAll(filepath.Join(broken, "a", "b", "c", "0000000000000000000000000000d"), 0o755); err != nil {
		t.Fatal(err)
	}
	e.images = []string{broken}
	params, _ := json.Marshal(protocol.CatalogIngestParams{Run: "r"})
	if _, err := ingest(context.Background(), e, func(string, any) error { return nil }, params); err == nil {
		t.Fatal("ingest succeeded without reading Aria's image")
	}
	if got := search(t, e, `{"site": "chub"}`); slices.Contains(got, "Aria") {
		t.Fatalf("Aria was stored without her image: %v", got)
	}
}
