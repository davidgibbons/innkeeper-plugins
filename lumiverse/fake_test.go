package main

import (
	"cmp"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
)

// fake is an in-memory Lumiverse with the routes the plugin uses, behaving
// as Lumiverse's source does at commit 7398fa5.
type fake struct {
	mu         sync.Mutex
	srv        *httptest.Server
	token      string
	signIns    int
	characters map[string]map[string]any
	books      map[string]map[string]any
	entries    map[string][]map[string]any // by book ID
	avatars    int
	nextID     int
}

const fakePassword = "hunter2"

func newFake(t *testing.T) *fake {
	f := &fake{characters: map[string]map[string]any{}, books: map[string]map[string]any{},
		entries: map[string][]map[string]any{}}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/auth/sign-in/{how}", f.signIn)
	api := map[string]http.HandlerFunc{
		"POST /characters/import":             f.importCharacter,
		"GET /characters/summary":             f.summary,
		"GET /characters/{id}":                f.getCharacter,
		"PUT /characters/{id}":                f.putCharacter,
		"POST /characters/{id}/avatar":        f.avatar,
		"GET /characters/{id}/export":         f.exportCharacter,
		"POST /world-books":                   f.createBook,
		"GET /world-books":                    f.listBooks,
		"GET /world-books/{id}":               f.getBook,
		"PUT /world-books/{id}":               f.putBook,
		"GET /world-books/{id}/entries":       f.listEntries,
		"POST /world-books/{id}/entries":      f.createEntry,
		"POST /world-books/{id}/entries/bulk": f.bulkEntries,
		"GET /world-books/{id}/export":        f.exportBook,
	}
	for pattern, h := range api {
		method, path, _ := strings.Cut(pattern, " ")
		mux.HandleFunc(method+" /api/v1"+path, f.authed(h))
	}
	f.srv = httptest.NewServer(mux)
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fake) id(prefix string) string {
	f.nextID++
	return fmt.Sprintf("%s%d", prefix, f.nextID)
}

func reply(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func body(r *http.Request) map[string]any {
	var m map[string]any
	_ = json.NewDecoder(r.Body).Decode(&m)
	return m
}

func (f *fake) signIn(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.signIns++
	b := body(r)
	who := b["username"]
	if r.PathValue("how") == "email" {
		who = b["email"]
	}
	if who != "owner" && who != "owner@example.com" || b["password"] != fakePassword {
		reply(w, 401, map[string]any{"message": "Invalid username or password"})
		return
	}
	f.token = fmt.Sprintf("token-%d", f.signIns)
	reply(w, 200, map[string]any{"token": f.token})
}

func (f *fake) authed(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		if f.token == "" || r.Header.Get("Authorization") != "Bearer "+f.token {
			reply(w, 401, map[string]any{"error": "Unauthorized", "code": "SESSION_EXPIRED"})
			return
		}
		h(w, r)
	}
}

var fakeStrings = []string{"name", "description", "personality", "scenario", "first_mes", "mes_example",
	"creator", "creator_notes", "system_prompt", "post_history_instructions"}

func (f *fake) importCharacter(w http.ResponseWriter, r *http.Request) {
	data := object(body(r)["data"])
	ext := map[string]any{"_lumiverse_library_scope": "mine"}
	for k, v := range object(data["extensions"]) {
		ext[k] = v
	}
	if v, ok := data["character_version"]; ok {
		ext["character_version"] = v
	}
	if b, ok := data["character_book"]; ok {
		ext["character_book"] = b
	}
	c := map[string]any{"id": f.id("c"), "extensions": ext,
		"tags": listOr(data["tags"]), "alternate_greetings": listOr(data["alternate_greetings"])}
	for _, k := range fakeStrings {
		s, _ := data[k].(string)
		c[k] = s
	}
	f.characters[c["id"].(string)] = c
	reply(w, 201, map[string]any{"character": c})
}

func (f *fake) summary(w http.ResponseWriter, r *http.Request) {
	q := strings.ToLower(r.URL.Query().Get("search"))
	out := []any{}
	for _, c := range f.characters {
		if strings.Contains(strings.ToLower(c["name"].(string)), q) {
			out = append(out, map[string]any{"id": c["id"], "name": c["name"]})
		}
	}
	reply(w, 200, map[string]any{"data": out, "total": len(out)})
}

func (f *fake) character(w http.ResponseWriter, r *http.Request) map[string]any {
	c, ok := f.characters[r.PathValue("id")]
	if !ok {
		reply(w, 404, map[string]any{"error": "Not found"})
	}
	return c
}

func (f *fake) getCharacter(w http.ResponseWriter, r *http.Request) {
	if c := f.character(w, r); c != nil {
		reply(w, 200, c)
	}
}

func (f *fake) putCharacter(w http.ResponseWriter, r *http.Request) {
	c := f.character(w, r)
	if c == nil {
		return
	}
	for k, v := range body(r) {
		c[k] = v // extensions included: replaced wholesale
	}
	reply(w, 200, c)
}

func (f *fake) avatar(w http.ResponseWriter, r *http.Request) {
	c := f.character(w, r)
	if c == nil {
		return
	}
	file, _, err := r.FormFile("avatar")
	if err != nil {
		reply(w, 400, map[string]any{"error": "avatar file is required"})
		return
	}
	raw, _ := io.ReadAll(file)
	f.avatars++
	c["image_id"] = fmt.Sprintf("img-%d-%d", f.avatars, len(raw))
	reply(w, 200, c)
}

func (f *fake) exportCharacter(w http.ResponseWriter, r *http.Request) {
	c := f.character(w, r)
	if c == nil {
		return
	}
	data := map[string]any{"tags": c["tags"], "alternate_greetings": c["alternate_greetings"]}
	for _, k := range fakeStrings {
		data[k] = c[k]
	}
	ext := map[string]any{}
	for k, v := range object(c["extensions"]) {
		if k != "world_book_ids" {
			ext[k] = v
		}
	}
	if v, ok := ext["character_version"]; ok {
		data["character_version"] = v
	}
	data["extensions"] = ext
	// Rebuilt from the linked world books.
	if ids := listOr(object(c["extensions"])["world_book_ids"]); len(ids) > 0 {
		if b, ok := f.books[fmt.Sprint(ids[0])]; ok {
			data["character_book"] = f.exportOf(b)
		}
	}
	reply(w, 200, map[string]any{"spec": "chara_card_v3", "spec_version": "3.0", "data": data})
}

func (f *fake) createBook(w http.ResponseWriter, r *http.Request) {
	b := body(r)
	b["id"] = f.id("wb")
	f.books[b["id"].(string)] = b
	reply(w, 201, b)
}

func (f *fake) listBooks(w http.ResponseWriter, r *http.Request) {
	out := []any{}
	for _, b := range f.books {
		out = append(out, b)
	}
	reply(w, 200, map[string]any{"data": out, "total": len(out)})
}

func (f *fake) book(w http.ResponseWriter, r *http.Request) map[string]any {
	b, ok := f.books[r.PathValue("id")]
	if !ok {
		reply(w, 404, map[string]any{"error": "Not found"})
	}
	return b
}

func (f *fake) getBook(w http.ResponseWriter, r *http.Request) {
	if b := f.book(w, r); b != nil {
		out := map[string]any{"entries": map[string]any{"data": f.entries[b["id"].(string)]}}
		for k, v := range b {
			out[k] = v
		}
		reply(w, 200, out)
	}
}

func (f *fake) putBook(w http.ResponseWriter, r *http.Request) {
	b := f.book(w, r)
	if b == nil {
		return
	}
	for k, v := range body(r) {
		b[k] = v
	}
	reply(w, 200, b)
}

func (f *fake) listEntries(w http.ResponseWriter, r *http.Request) {
	if b := f.book(w, r); b != nil {
		// One page of at most two, so callers must page.
		list := f.entries[b["id"].(string)]
		list = list[:min(2, len(list))]
		reply(w, 200, map[string]any{"data": list, "total": len(f.entries[b["id"].(string)])})
	}
}

func (f *fake) createEntry(w http.ResponseWriter, r *http.Request) {
	b := f.book(w, r)
	if b == nil {
		return
	}
	e := body(r)
	e["id"] = f.id("e")
	e["uid"] = "uid-" + e["id"].(string)
	f.entries[b["id"].(string)] = append(f.entries[b["id"].(string)], e)
	reply(w, 201, e)
}

func (f *fake) bulkEntries(w http.ResponseWriter, r *http.Request) {
	b := f.book(w, r)
	if b == nil {
		return
	}
	in := body(r)
	ids := listOr(in["entry_ids"])
	f.entries[b["id"].(string)] = slices.DeleteFunc(f.entries[b["id"].(string)], func(e map[string]any) bool {
		return slices.Contains(ids, e["id"])
	})
	reply(w, 200, map[string]any{"affected": len(ids)})
}

func (f *fake) exportBook(w http.ResponseWriter, r *http.Request) {
	if b := f.book(w, r); b != nil {
		reply(w, 200, f.exportOf(b))
	}
}

// exportOf builds a character_book as Lumiverse's entryToCharacterBookSpec
// does: ordered by order_value, renumbered, with its own extension keys.
func (f *fake) exportOf(b map[string]any) map[string]any {
	list := slices.Clone(f.entries[b["id"].(string)])
	slices.SortStableFunc(list, func(x, y map[string]any) int {
		return cmp.Compare(x["order_value"].(float64), y["order_value"].(float64))
	})
	entries := []any{}
	for i, e := range list {
		ext := map[string]any{}
		for k, v := range object(e["extensions"]) {
			ext[k] = v
		}
		ext["priority"], ext["sticky"], ext["probability"], ext["uid"] = e["priority"], 0, 100, e["uid"]
		entries = append(entries, map[string]any{"id": i, "keys": e["key"], "secondary_keys": e["keysecondary"],
			"content": e["content"], "comment": e["comment"], "enabled": e["disabled"] != true,
			"insertion_order": e["order_value"], "position": e["position"], "depth": cmp.Or(e["depth"], any(4.0)),
			"selective": e["selective"], "constant": e["constant"], "case_sensitive": e["case_sensitive"],
			"match_whole_words": false, "extensions": ext})
	}
	return map[string]any{"name": b["name"], "description": b["description"], "entries": entries}
}
