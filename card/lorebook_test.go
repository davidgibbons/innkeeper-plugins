package card

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestLorebookGolden decodes each testdata/*.lorebook.json, compares it with
// its .want.json, and checks the result decodes to itself.
func TestLorebookGolden(t *testing.T) {
	files, err := filepath.Glob("testdata/*.lorebook.json")
	if err != nil || len(files) == 0 {
		t.Fatalf("no golden lorebooks: %v", err)
	}
	for _, f := range files {
		t.Run(filepath.Base(f), func(t *testing.T) {
			in, err := os.ReadFile(f)
			if err != nil {
				t.Fatal(err)
			}
			book, err := DecodeLorebook(in, "Eldoria")
			if err != nil {
				t.Fatal(err)
			}
			if *update {
				var buf bytes.Buffer
				if err := json.Indent(&buf, book, "", "  "); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(f+".want.json", append(buf.Bytes(), '\n'), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			want, err := os.ReadFile(f + ".want.json")
			if err != nil {
				t.Fatalf("%v; run go test ./card -run TestLorebookGolden -update and review the file", err)
			}
			if !sameJSON(t, book, want) {
				t.Fatalf("decoded lorebook differs from %s.want.json:\n%s", f, book)
			}
			again, err := DecodeLorebook(book, "other")
			if err != nil || !sameJSON(t, again, book) {
				t.Fatalf("decoding the result changed it: %v\n%s", err, again)
			}
		})
	}
}

func TestDecodeLorebookShapes(t *testing.T) {
	book := `{"name": "B", "extensions": {}, "entries": [{"keys": ["k"], "content": "c", "extensions": {}, "enabled": true, "insertion_order": 0, "use_regex": false}]}`
	for name, in := range map[string]string{
		"character_book": book,
		"lorebook_v3":    `{"spec": "lorebook_v3", "data": ` + book + `}`,
	} {
		got, err := DecodeLorebook([]byte(in), "file")
		if err != nil || !sameJSON(t, got, []byte(book)) {
			t.Errorf("%s: %v\n%s", name, err, got)
		}
	}
	got, err := DecodeLorebook([]byte(`{"entries": []}`), "file")
	if err != nil || !strings.Contains(string(got), `"name":"file"`) || !strings.Contains(string(got), `"extensions":{}`) {
		t.Errorf("unnamed book: %v %s", err, got)
	}
}

func TestDecodeWorldInfo(t *testing.T) {
	in := `{"entries": {
		"10": {"uid": 10, "key": ["b"], "content": "B", "disable": true, "position": 1, "excludeRecursion": true, "addMemo": true},
		"2": {"uid": 2, "key": ["a"], "content": "A", "position": 0, "extensions": null},
		"30": {"uid": 30, "key": null, "content": null}}}`
	got, err := DecodeLorebook([]byte(in), "world")
	if err != nil {
		t.Fatal(err)
	}
	var book struct {
		Name    string
		Entries []map[string]any
	}
	if err := json.Unmarshal(got, &book); err != nil {
		t.Fatal(err)
	}
	if book.Name != "world" || len(book.Entries) != 3 || book.Entries[0]["content"] != "A" {
		t.Fatalf("book = %s; want name world and entries in uid order", got)
	}
	a, b := book.Entries[0], book.Entries[1]
	if a["position"] != "before_char" || a["enabled"] != true {
		t.Errorf("entry 2 = %v", a)
	}
	if c := book.Entries[2]; c["keys"] == nil || c["content"] != "" {
		t.Errorf("entry 30 = %v; nulls must keep the defaults", c)
	}
	ext := b["extensions"].(map[string]any)
	if b["id"] != 10.0 || b["enabled"] != false || b["position"] != "after_char" || b["use_regex"] != true ||
		ext["position"] != 1.0 || ext["exclude_recursion"] != true || ext["addMemo"] != true {
		t.Errorf("entry 10 = %v", b)
	}
}

func TestDecodeLorebookRejects(t *testing.T) {
	for _, in := range []string{
		`not json`,
		`[]`,
		`{"name": "x", "description": "a V1 card"}`,
		`{"entries": "x"}`,
		`{"entries": [1]}`,
		`{"spec": "chara_card_v3", "data": {"name": "x"}}`,
	} {
		if _, err := DecodeLorebook([]byte(in), "f"); !errors.Is(err, ErrNotLorebook) {
			t.Errorf("%s: err = %v, want ErrNotLorebook", in, err)
		}
	}
}
