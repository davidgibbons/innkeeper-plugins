package main

import (
	"net/http"
	"slices"
	"testing"

	"github.com/davidgibbons/innkeeper/protocol"
)

func chubSite(t *testing.T) *fetcher {
	t.Helper()
	cardJSON := testdata(t, "v2-seraphina.json")
	avatar := testdata(t, "v2-seraphina.png")
	lorebook := testdata(t, "eldoria.lorebook.json")
	mux := http.NewServeMux()
	mux.HandleFunc("GET api.chub.ai/api/characters/{user}/{slug}", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("full") != "true" || r.PathValue("user") != "u" || r.PathValue("slug") != "seraphina" {
			http.NotFound(w, r)
			return
		}
		w.Write([]byte(`{"node": {"id": 3604386, "name": "Seraphina", "max_res_url": "https://avatars.charhub.io/avatars/u/seraphina/chara_card_v2.png"}}`))
	})
	mux.HandleFunc("GET api.chub.ai/api/v4/projects/3604386/repository/files/card.json/raw", func(w http.ResponseWriter, _ *http.Request) {
		w.Write(cardJSON)
	})
	mux.HandleFunc("GET avatars.charhub.io/avatars/u/seraphina/chara_card_v2.png", func(w http.ResponseWriter, _ *http.Request) {
		w.Write(avatar)
	})
	mux.HandleFunc("GET api.chub.ai/api/lorebooks/u/eldoria", func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte(`{"node": {"id": 7, "name": "Eldoria", "definition": {"embedded_lorebook": ` + string(lorebook) + `}}}`))
	})
	mux.HandleFunc("GET api.chub.ai/api/lorebooks/u/hidden", func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte(`{"node": {"id": 8, "name": "Hidden", "definition": {"embedded_lorebook": null}}}`))
	})
	return sites(t, mux)
}

func TestChubCard(t *testing.T) {
	f := chubSite(t)
	const item = "https://chub.ai/characters/u/seraphina"
	for _, page := range []string{
		item,
		"https://www.chub.ai/characters/u/seraphina/",
		"https://characterhub.org/characters/u/seraphina",
		"https://venus.chub.ai/characters/u/seraphina?tab=chats",
		"https://chub.ai/u/seraphina",
	} {
		sent, err := importURL(t, f, page)
		if err != nil {
			t.Fatalf("%s: %v", page, err)
		}
		ev := emitted(t, sent)
		if ev.Item != item || ev.Avatar == "" || !slices.Contains(sources(t, ev.Card), item) {
			t.Errorf("%s: item %q, avatar %q, source %q", page, ev.Item, ev.Avatar, sources(t, ev.Card))
		}
	}
}

func TestChubLorebook(t *testing.T) {
	sent, err := importURL(t, chubSite(t), "https://chub.ai/lorebooks/u/eldoria")
	if err != nil {
		t.Fatal(err)
	}
	ev, ok := sent[0].(protocol.EmitLorebook)
	if !ok || ev.Item != "https://chub.ai/lorebooks/u/eldoria" || len(ev.Lorebook) == 0 {
		t.Fatalf("sent %+v", sent)
	}
	if _, err := importURL(t, chubSite(t), "https://chub.ai/lorebooks/u/hidden"); rpcErr(t, err).Retryable() {
		t.Errorf("hidden lorebook: err = %v", err)
	}
}

func TestChubBadPaths(t *testing.T) {
	f := chubSite(t)
	for _, page := range []string{
		"https://chub.ai/characters/onlyone",
		"https://chub.ai/characters/a/b/c",
		"https://chub.ai/characters/../b",
		"https://chub.ai/",
	} {
		if _, err := importURL(t, f, page); rpcErr(t, err).Code != protocol.CodeInvalidParams {
			t.Errorf("%s: err = %v, want invalid params", page, err)
		}
	}
	if _, err := importURL(t, f, "https://chub.ai/characters/u/gone"); rpcErr(t, err).Retryable() {
		t.Errorf("missing card: err = %v, want not retryable", err)
	}
}

func TestChubAvatarGone(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET api.chub.ai/api/characters/u/seraphina", func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte(`{"node": {"id": 1, "max_res_url": "https://avatars.charhub.io/gone.png"}}`))
	})
	mux.HandleFunc("GET api.chub.ai/api/v4/projects/1/repository/files/card.json/raw", func(w http.ResponseWriter, _ *http.Request) {
		w.Write(testdata(t, "v2-seraphina.json"))
	})
	sent, err := importURL(t, sites(t, mux), "https://chub.ai/characters/u/seraphina")
	if err != nil {
		t.Fatalf("avatar 404 lost the card: %v", err)
	}
	emitted(t, sent)
}

func TestChubAvatarOutage(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET api.chub.ai/api/characters/u/seraphina", func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte(`{"node": {"id": 1, "max_res_url": "https://avatars.charhub.io/down.png"}}`))
	})
	mux.HandleFunc("GET api.chub.ai/api/v4/projects/1/repository/files/card.json/raw", func(w http.ResponseWriter, _ *http.Request) {
		w.Write(testdata(t, "v2-seraphina.json"))
	})
	mux.HandleFunc("GET avatars.charhub.io/down.png", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	})
	_, err := importURL(t, sites(t, mux), "https://chub.ai/characters/u/seraphina")
	if err == nil || !rpcErr(t, err).Retryable() {
		t.Fatalf("avatar 503: err = %v, want a retryable failure", err)
	}
}
