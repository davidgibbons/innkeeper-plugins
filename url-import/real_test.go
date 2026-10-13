package main

import (
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
