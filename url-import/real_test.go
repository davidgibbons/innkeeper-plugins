package main

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"os"
	"strings"
	"testing"
)

// TestReal imports each page URL_IMPORT_REAL lists, separated by spaces, from
// the live sites. Pick SFW cards that are public.
func TestReal(t *testing.T) {
	pages := strings.Fields(os.Getenv("URL_IMPORT_REAL"))
	if len(pages) == 0 {
		t.Skip("URL_IMPORT_REAL is not set")
	}
	f := newFetcher()
	for _, page := range pages {
		sent, err := importURL(t, f, page)
		if err != nil {
			t.Errorf("%s: %v", page, err)
			continue
		}
		t.Logf("%s: sent %d notifications", page, len(sent))
	}
}

// TestRealFollow fetches each remote_id URL_IMPORT_FOLLOW lists, such as
// chub:{user}/{slug}, twice from the live sites. Two copies that differ would
// flag an update on every check.
func TestRealFollow(t *testing.T) {
	ids := strings.Fields(os.Getenv("URL_IMPORT_FOLLOW"))
	if len(ids) == 0 {
		t.Skip("URL_IMPORT_FOLLOW is not set")
	}
	f := newFetcher()
	for _, id := range ids {
		a, err := get(t, f, id, true)
		if err != nil {
			t.Errorf("%s: %v", id, err)
			continue
		}
		b, err := get(t, f, id, true)
		if err != nil {
			t.Errorf("%s: %v", id, err)
			continue
		}
		if !bytes.Equal(a.Card, b.Card) || fileSum(t, a.Avatar) != fileSum(t, b.Avatar) {
			t.Errorf("%s: two fetches differ", id)
		}
	}
}

// fileSum is the SHA-256 of the file at path, or "" for no file.
func fileSum(t *testing.T, path string) string {
	t.Helper()
	if path == "" {
		return ""
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return fmt.Sprintf("%x", sha256.Sum256(b))
}
