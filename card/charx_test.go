package card

import (
	"archive/zip"
	"bytes"
	"errors"
	"os"
	"strings"
	"testing"
)

// charx zips files, from name to content.
func charx(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, content := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		w.Write([]byte(content))
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestCharXAvatar(t *testing.T) {
	golden, err := os.ReadFile("testdata/v3-mirelle.charx")
	if err != nil {
		t.Fatal(err)
	}
	if _, img, err := Decode(golden); err != nil || !isPNG(img) {
		t.Fatalf("golden CharX: err %v, avatar %.8q", err, img)
	}
	icon := func(uri string) string {
		return `{"spec": "chara_card_v3", "spec_version": "3.0", "data": {"name": "X", "assets": [{"type": "icon", "name": "main", "uri": "` + uri + `"}]}}`
	}
	for name, files := range map[string]map[string]string{
		"no assets":       {"card.json": `{"spec": "chara_card_v3", "spec_version": "3.0", "data": {"name": "X"}}`},
		"icon missing":    {"card.json": icon("embeded://assets/icon/image/main.png")},
		"icon not image":  {"card.json": icon("embeded://main.png"), "main.png": "not an image"},
		"path leaves zip": {"card.json": icon("embeded://../main.png"), "../main.png": "x"},
		"not embedded":    {"card.json": icon("ccdefault:")},
	} {
		if card, img, err := Decode(charx(t, files)); err != nil || card == nil || img != nil {
			t.Errorf("%s: err %v, card %s, %d-byte avatar; want the card and no avatar", name, err, card, len(img))
		}
	}
}

func TestCharXRejects(t *testing.T) {
	big := `{"spec": "chara_card_v3", "data": {"name": "` + strings.Repeat("x", 33<<20) + `"}}`
	for name, in := range map[string][]byte{
		"no card.json":   charx(t, map[string]string{"x_meta/main.json": "{}"}),
		"card.json junk": charx(t, map[string]string{"card.json": "not json"}),
		"over the cap":   charx(t, map[string]string{"card.json": big}),
		"truncated zip":  []byte("PK\x03\x04"),
	} {
		if _, _, err := Decode(in); !errors.Is(err, ErrNotCard) {
			t.Errorf("%s: err = %v, want ErrNotCard", name, err)
		}
	}
}
