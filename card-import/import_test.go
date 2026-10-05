package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/davidgibbons/innkeeper/protocol"
)

// recorder keeps the notifications an import sends.
type recorder struct{ sent []any }

func (r *recorder) notify(_ string, params any) error {
	r.sent = append(r.sent, params)
	return nil
}

// items lists what was emitted, as "card <item>" or "lorebook <item>".
func (r *recorder) items() []string {
	var out []string
	for _, p := range r.sent {
		switch p := p.(type) {
		case protocol.EmitCard:
			out = append(out, "card "+p.Item)
		case protocol.EmitLorebook:
			out = append(out, "lorebook "+p.Item)
		}
	}
	return out
}

// startImport runs import.run with params and checkpoint, both JSON.
func startImport(t *testing.T, e env, params, checkpoint string) (*recorder, error) {
	t.Helper()
	raw, err := json.Marshal(protocol.ImportRunParams{Run: "1.1", Params: json.RawMessage(params),
		Checkpoint: json.RawMessage(checkpoint)})
	if err != nil {
		t.Fatal(err)
	}
	rec := &recorder{}
	_, err = run(context.Background(), e, rec.notify, raw)
	return rec, err
}

func testdata(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "card", "testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func isInvalid(err error) bool {
	var rpcErr *protocol.Error
	return errors.As(err, &rpcErr) && rpcErr.Code == protocol.CodeInvalidParams
}

func TestImportUpload(t *testing.T) {
	lib, tmp := t.TempDir(), t.TempDir()
	put := func(b []byte) string {
		sum := sha256.Sum256(b)
		sha := hex.EncodeToString(sum[:])
		path := filepath.Join(lib, sha[:2], sha[2:4], sha)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, b, 0o644); err != nil {
			t.Fatal(err)
		}
		return sha
	}
	e := env{library: lib, blobTmp: tmp}

	sha := put(testdata(t, "v3-seraphina.png"))
	rec, err := startImport(t, e, `{"blob": "`+sha+`", "name": "seraphina.png"}`, `null`)
	if err != nil {
		t.Fatal(err)
	}
	if got := rec.items(); !slices.Equal(got, []string{"card " + sha}) {
		t.Fatalf("emitted %q", got)
	}
	if avatar := rec.sent[0].(protocol.EmitCard).Avatar; filepath.Dir(avatar) != tmp {
		t.Fatalf("avatar %q is not in blob_tmp", avatar)
	}

	sha = put(testdata(t, "eldoria.lorebook.json"))
	rec, err = startImport(t, e, `{"blob": "`+sha+`", "name": "Eldoria.json"}`, `null`)
	if err != nil {
		t.Fatal(err)
	}
	if book := rec.sent[0].(protocol.EmitLorebook).Lorebook; !strings.Contains(string(book), `"name":"Eldoria"`) {
		t.Fatalf("lorebook not named from the upload: %.200s", book)
	}

	sha = put([]byte("not a card"))
	if _, err := startImport(t, e, `{"blob": "`+sha+`"}`, `null`); !isInvalid(err) {
		t.Fatalf("non-card upload: err = %v, want -32602", err)
	}
}

func TestImportRejects(t *testing.T) {
	dir := t.TempDir()
	missing := strings.Repeat("a", 64)
	for name, c := range map[string]struct {
		e      env
		params string
	}{
		"no folder configured":   {env{}, `{}`},
		"path leaves the folder": {env{folder: dir}, `{"path": "../etc"}`},
		"absolute path":          {env{folder: dir}, `{"path": "/etc"}`},
		"missing subfolder":      {env{folder: dir}, `{"path": "nope"}`},
		"path and blob":          {env{folder: dir, library: dir}, `{"path": "a", "blob": "` + missing + `"}`},
		"unknown param":          {env{folder: dir}, `{"folder": "/etc"}`},
		"blob not a sha256":      {env{library: dir}, `{"blob": "../../etc/passwd"}`},
		"missing upload":         {env{library: dir}, `{"blob": "` + missing + `"}`},
	} {
		if _, err := startImport(t, c.e, c.params, `null`); !isInvalid(err) {
			t.Errorf("%s: err = %v, want -32602", name, err)
		}
	}
}
