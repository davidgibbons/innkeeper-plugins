package main

import (
	"context"
	"net/http"
	"testing"
	"time"
)

func TestFetcherSpacesRequestsToOneHost(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET api.chub.ai/x", func(http.ResponseWriter, *http.Request) {})
	mux.HandleFunc("GET avatars.charhub.io/x", func(http.ResponseWriter, *http.Request) {})
	f := sites(t, mux)
	f.gap = 200 * time.Millisecond
	start := time.Now()
	// The second host doesn't wait; the third request waits for the first's gap.
	for _, u := range []string{"https://api.chub.ai/x", "https://avatars.charhub.io/x", "https://api.chub.ai/x"} {
		if _, err := f.get(context.Background(), "test", u); err != nil {
			t.Fatal(err)
		}
	}
	if d := time.Since(start); d < f.gap || d >= 2*f.gap {
		t.Errorf("three requests took %v, want one gap of %v", d, f.gap)
	}
}
