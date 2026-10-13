package main

import (
	"net/http"
	"slices"
	"testing"

	"github.com/davidgibbons/innkeeper/protocol"
)

func TestTavern(t *testing.T) {
	png := testdata(t, "v3-seraphina.png")
	mux := http.NewServeMux()
	mux.HandleFunc("GET ct-cards.storage.character-tavern.com/ann/seraphina.png", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("action") != "download" {
			http.Error(w, "zTXt chunks", http.StatusTeapot)
			return
		}
		w.Write(png)
	})
	f := sites(t, mux)
	const item = "https://character-tavern.com/character/ann/seraphina"
	sent, err := importURL(t, f, item)
	if err != nil {
		t.Fatal(err)
	}
	ev := emitted(t, sent)
	if ev.Item != item || ev.Avatar == "" || !slices.Contains(sources(t, ev.Card), item) {
		t.Errorf("item %q, avatar %q, source %q", ev.Item, ev.Avatar, sources(t, ev.Card))
	}
	for _, page := range []string{"https://character-tavern.com/character/ann", "https://character-tavern.com/ann/seraphina"} {
		if _, err := importURL(t, f, page); rpcErr(t, err).Code != protocol.CodeInvalidParams {
			t.Errorf("%s: err = %v, want invalid params", page, err)
		}
	}
}
