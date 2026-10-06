package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/davidgibbons/innkeeper-plugins/card"
)

// fake is an in-memory SillyTavern with the routes the plugin uses, behaving
// as SillyTavern's source does at commit 06bde93.
type fake struct {
	mu  sync.Mutex
	srv *httptest.Server

	// Server settings.
	refuse       bool   // whitelistMode on and the caller's address not listed
	basicUser    string // basicAuthMode, when set
	basicPass    string
	accounts     bool // enableUserAccounts
	perUserBasic bool // perUserBasicAuth, which lets basic auth pick the account
	handle       string
	password     string
	csrfDisabled bool
	lazy         bool   // performance.lazyLoadCharacters
	csrfBody     string // when set, /csrf-token answers 200 with this body, as a proxy might

	// Test hooks.
	deny      bool // answer every private route with 403, as a disabled account would
	failWith  int  // answer every private route with this status
	badImport bool // answer /import with {error: true}
	// editFile, when set, is the file ID /edit writes for a name, as a
	// case-insensitive file system might choose; "" stores nothing.
	editFile func(name string) string

	// What the server saw.
	requests      int
	logins        int // successful logins, by GET /login or POST /api/users/login
	loginAttempts int // the rate limiter's count, cleared by a success
	csrfTokens    int
	lastCSRF      string // the X-CSRF-Token of the latest POST
	sessions      map[string]*fakeSession
	nextID        int

	characters map[string]map[string]any // by file name, the stored JSON
	images     map[string][]byte
	avatars    map[string]int   // /edit-avatar uploads by file name
	merges     []map[string]any // /merge-attributes bodies
	worlds     map[string]map[string]any
}

type fakeSession struct {
	handle string
	csrf   string
}

const fakePassword = "hunter2"

func newFake(t *testing.T) *fake {
	f := &fake{sessions: map[string]*fakeSession{}, handle: "owner", password: fakePassword,
		characters: map[string]map[string]any{}, images: map[string][]byte{}, avatars: map[string]int{},
		worlds: map[string]map[string]any{}}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /login", f.login)
	mux.HandleFunc("GET /csrf-token", f.csrfToken)
	mux.HandleFunc("POST /api/users/login", f.usersLogin)
	for path, h := range map[string]http.HandlerFunc{
		"/api/characters/all":              f.charactersAll,
		"/api/characters/get":              f.charactersGet,
		"/api/characters/import":           f.charactersImport,
		"/api/characters/merge-attributes": f.charactersMerge,
		"/api/characters/edit-avatar":      f.charactersEditAvatar,
		"/api/characters/export":           f.charactersExport,
		"/api/worldinfo/list":              f.worldList,
		"/api/worldinfo/get":               f.worldGet,
		"/api/worldinfo/edit":              f.worldEdit,
	} {
		mux.HandleFunc("POST "+path, f.private(h))
	}
	f.srv = httptest.NewServer(f.front(mux))
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

// endSessions forgets every session, as a server restart or a password change does.
func (f *fake) endSessions() {
	f.mu.Lock()
	defer f.mu.Unlock()
	clear(f.sessions)
}

// front runs the checks that come before every route, in SillyTavern's order:
// basic auth, the whitelist, the cookie session, then CSRF.
func (f *fake) front(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		f.requests++
		if f.basicUser != "" {
			if u, p, ok := r.BasicAuth(); !ok || u != f.basicUser || p != f.basicPass {
				w.Header().Set("WWW-Authenticate", `Basic realm="SillyTavern", charset="UTF-8"`)
				http.Error(w, "Unauthorized", http.StatusUnauthorized)
				return
			}
		}
		if f.refuse {
			w.Header().Set("Content-Type", "text/html")
			w.WriteHeader(http.StatusForbidden)
			_, _ = io.WriteString(w, "<html><body>Forbidden: add this address to the whitelist</body></html>")
			return
		}
		var s *fakeSession
		if c, err := r.Cookie("session"); err == nil {
			s = f.sessions[c.Value]
		}
		if s == nil {
			s = &fakeSession{}
			if !f.accounts {
				s.handle = "default-user"
			}
			id := f.id("sid")
			f.sessions[id] = s
			http.SetCookie(w, &http.Cookie{Name: "session", Value: id, Path: "/"})
		}
		if r.Method == http.MethodPost {
			f.lastCSRF = r.Header.Get("X-CSRF-Token")
			if !f.csrfDisabled && (s.csrf == "" || f.lastCSRF != s.csrf) {
				http.Error(w, "invalid csrf token", http.StatusForbidden)
				return
			}
		}
		next.ServeHTTP(w, r.WithContext(withSession(r, s)))
	})
}

// private is requireLoginMiddleware plus the test hooks.
func (f *fake) private(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if sessionOf(r).handle == "" || f.deny {
			reply(w, http.StatusForbidden, map[string]any{"error": "Forbidden"})
			return
		}
		if f.failWith != 0 {
			http.Error(w, "failing on purpose", f.failWith)
			return
		}
		h(w, r)
	}
}

// GET /login binds the session to the account that basic auth names, when perUserBasicAuth is on.
func (f *fake) login(w http.ResponseWriter, r *http.Request) {
	s := sessionOf(r)
	if f.accounts && f.perUserBasic && f.basicUser != "" && s.handle == "" {
		s.handle = f.basicUser
		f.logins++
	} else if !f.accounts {
		f.logins++
	}
	_, _ = io.WriteString(w, "<html>login</html>")
}

func (f *fake) csrfToken(w http.ResponseWriter, r *http.Request) {
	f.csrfTokens++
	if f.csrfBody != "" {
		_, _ = io.WriteString(w, f.csrfBody)
		return
	}
	s := sessionOf(r)
	if f.csrfDisabled {
		reply(w, 200, map[string]any{"token": "disabled"})
		return
	}
	if s.csrf == "" {
		s.csrf = f.id("csrf")
	}
	reply(w, 200, map[string]any{"token": s.csrf})
}

// POST /api/users/login counts every attempt toward five a minute and clears
// the count on a success, as users-public.js does.
func (f *fake) usersLogin(w http.ResponseWriter, r *http.Request) {
	b := body(r)
	if b["handle"] == nil || b["handle"] == "" {
		reply(w, 400, map[string]any{"error": "Missing required fields"})
		return
	}
	f.loginAttempts++
	if f.loginAttempts > 5 {
		w.Header().Set("Retry-After", "60")
		reply(w, 429, map[string]any{"error": "Too many attempts."})
		return
	}
	if b["handle"] != f.handle || b["password"] != f.password {
		reply(w, 403, map[string]any{"error": "Incorrect credentials"})
		return
	}
	f.loginAttempts = 0
	f.logins++
	sessionOf(r).handle = f.handle
	reply(w, 200, map[string]any{"handle": f.handle})
}

// readFromV2 copies a V2 card's fields to the V1 top level, as import does.
func readFromV2(c map[string]any) {
	data := object(c["data"])
	for _, k := range []string{"name", "description", "personality", "scenario", "first_mes", "mes_example", "tags"} {
		c[k] = data[k]
	}
	c["creatorcomment"] = data["creator_notes"]
	c["fav"] = false
	ext := object(data["extensions"])
	ext["fav"] = false
	data["extensions"] = ext
	c["data"] = data
	c["avatar"] = "none"
	c["create_date"] = time.Now().Format(time.RFC3339)
	delete(c, "chat")
}

func summaryOf(file string, c map[string]any) map[string]any {
	data := object(c["data"])
	ext := object(data["extensions"])
	return map[string]any{"shallow": true, "name": c["name"], "avatar": file, "fav": c["fav"],
		"create_date": c["create_date"], "tags": c["tags"],
		"data": map[string]any{"name": data["name"], "character_version": data["character_version"],
			"creator": data["creator"], "creator_notes": data["creator_notes"], "tags": data["tags"],
			"extensions": map[string]any{"fav": ext["fav"], "world": ext["world"]}}}
}

// character is /get's reply: the stored card converted to V2, the file
// name, and the stored JSON.
func character(file string, c map[string]any) map[string]any {
	raw, _ := json.Marshal(c)
	out := map[string]any{}
	for k, v := range c {
		out[k] = v
	}
	if _, ok := c["data"].(map[string]any); !ok {
		convertToV2(out)
	}
	out["avatar"] = file
	out["json_data"] = string(raw)
	return out
}

// convertToV2 fills a V1 card's data as charaFormatData does.
func convertToV2(c map[string]any) {
	str := func(k string) any {
		if s, ok := c[k].(string); ok {
			return s
		}
		return ""
	}
	tags, _ := c["tags"].([]any)
	if tags == nil {
		tags = []any{}
	}
	c["spec"], c["spec_version"], c["tags"] = "chara_card_v2", "2.0", tags
	c["data"] = map[string]any{"name": c["name"], "description": str("description"), "personality": str("personality"),
		"scenario": str("scenario"), "first_mes": str("first_mes"), "mes_example": str("mes_example"),
		"creator_notes": str("creatorcomment"), "system_prompt": "", "post_history_instructions": "", "tags": tags,
		"creator": str("creator"), "character_version": "", "alternate_greetings": []any{},
		"extensions": map[string]any{"talkativeness": 0.5, "fav": false, "world": "",
			"depth_prompt": map[string]any{"prompt": "", "depth": 4, "role": "system"}}}
}

func (f *fake) sortedFiles() []string {
	files := make([]string, 0, len(f.characters))
	for file := range f.characters {
		files = append(files, file)
	}
	slices.Sort(files)
	return files
}

func (f *fake) charactersAll(w http.ResponseWriter, r *http.Request) {
	out := []map[string]any{}
	for _, file := range f.sortedFiles() {
		if f.lazy {
			out = append(out, summaryOf(file, f.characters[file]))
		} else {
			out = append(out, character(file, f.characters[file]))
		}
	}
	reply(w, 200, out)
}

func (f *fake) charactersGet(w http.ResponseWriter, r *http.Request) {
	file, _ := body(r)["avatar_url"].(string)
	c, ok := f.characters[file]
	if !ok {
		http.NotFound(w, r)
		return
	}
	reply(w, 200, character(file, c))
}

// upload reads a multipart request: the text fields and the file in field "avatar".
func upload(r *http.Request) (fields map[string]string, file []byte, err error) {
	if err = r.ParseMultipartForm(32 << 20); err != nil {
		return nil, nil, err
	}
	fields = map[string]string{}
	for k, v := range r.MultipartForm.Value {
		fields[k] = v[0]
	}
	fh := r.MultipartForm.File["avatar"]
	if len(fh) == 0 {
		return fields, nil, nil
	}
	f, err := fh[0].Open()
	if err != nil {
		return nil, nil, err
	}
	defer f.Close()
	file, err = io.ReadAll(f)
	return fields, file, err
}

// charactersImport answers 200 {error: true} when the file isn't a card, and
// names a new file Name1, Name2 when Name.png exists.
func (f *fake) charactersImport(w http.ResponseWriter, r *http.Request) {
	fields, file, err := upload(r)
	if err != nil || file == nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	raw, _, err := card.Decode(file)
	if err != nil || fields["file_type"] != "png" || f.badImport {
		reply(w, 200, map[string]any{"error": true})
		return
	}
	var c map[string]any
	_ = json.Unmarshal(raw, &c)
	// Import cleans the card's name with sanitize-filename, as well as the file's.
	name := strings.Map(func(r rune) rune {
		if strings.ContainsRune(`\/:*?"<>|`, r) {
			return -1
		}
		return r
	}, fmt.Sprint(object(c["data"])["name"]))
	object(c["data"])["name"] = name
	readFromV2(c)
	if name == "" {
		name = "character"
	}
	stem := name
	for i := 1; f.characters[stem+".png"] != nil; i++ {
		stem = fmt.Sprintf("%s%d", name, i)
	}
	f.characters[stem+".png"], f.images[stem+".png"] = c, file
	reply(w, 200, map[string]any{"file_name": stem})
}

// deepMerge is util.js's: arrays and scalars replace, objects merge, and an
// object merged into a stored non-object yields a copy of that value as an
// object, dropping the source.
func deepMerge(target, source any) map[string]any {
	out := assign(target)
	tm, tok := target.(map[string]any)
	sm, sok := source.(map[string]any)
	if !tok || !sok {
		return out
	}
	for k, v := range sm {
		tv, in := tm[k]
		if _, isObj := v.(map[string]any); isObj && in {
			out[k] = deepMerge(tv, v)
		} else {
			out[k] = v
		}
	}
	return out
}

// assign is Object.assign({}, v): null and numbers give {}, strings and
// arrays give their elements keyed by index.
func assign(v any) map[string]any {
	out := map[string]any{}
	switch v := v.(type) {
	case map[string]any:
		for k, e := range v {
			out[k] = e
		}
	case string:
		for i, r := range []rune(v) {
			out[fmt.Sprint(i)] = string(r)
		}
	case []any:
		for i, e := range v {
			out[fmt.Sprint(i)] = e
		}
	}
	return out
}

func processUnset(target, source map[string]any) {
	for k, v := range source {
		if v == unset {
			delete(target, k)
		} else if sm, ok := v.(map[string]any); ok {
			if tm, ok := target[k].(map[string]any); ok {
				processUnset(tm, sm)
			}
		}
	}
}

// charactersMerge answers 500 for a missing card, as the real route does.
func (f *fake) charactersMerge(w http.ResponseWriter, r *http.Request) {
	update := body(r)
	f.merges = append(f.merges, update)
	file, _ := update["avatar"].(string)
	c, ok := f.characters[file]
	if !ok {
		reply(w, 500, map[string]any{"message": "Unexpected error while saving character.", "error": "Error: ENOENT"})
		return
	}
	merged := deepMerge(c, update)
	processUnset(merged, update)
	f.characters[file] = merged
	w.WriteHeader(http.StatusOK)
}

func (f *fake) charactersEditAvatar(w http.ResponseWriter, r *http.Request) {
	fields, file, err := upload(r)
	if err != nil || file == nil || fields["avatar_url"] == "" {
		http.Error(w, "Error: no file uploaded", http.StatusBadRequest)
		return
	}
	if f.characters[fields["avatar_url"]] == nil {
		http.Error(w, "Error: character file does not exist", http.StatusBadRequest)
		return
	}
	f.images[fields["avatar_url"]] = file
	f.avatars[fields["avatar_url"]]++
	w.WriteHeader(http.StatusOK)
}

// charactersExport answers format png with the stored image. SillyTavern
// also clears private fields in its card chunk, which the plugin ignores.
func (f *fake) charactersExport(w http.ResponseWriter, r *http.Request) {
	b := body(r)
	file, _ := b["avatar_url"].(string)
	img, ok := f.images[file]
	switch {
	case b["format"] != "png":
		w.WriteHeader(http.StatusBadRequest)
	case !ok:
		w.WriteHeader(http.StatusNotFound)
	default:
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(img)
	}
}

func (f *fake) worldList(w http.ResponseWriter, r *http.Request) {
	names := make([]string, 0, len(f.worlds))
	for n := range f.worlds {
		names = append(names, n)
	}
	slices.Sort(names)
	out := []map[string]any{}
	for _, n := range names {
		name := n
		if s, ok := f.worlds[n]["name"].(string); ok && s != "" {
			name = s
		}
		out = append(out, map[string]any{"file_id": n, "name": name, "extensions": object(f.worlds[n]["extensions"])})
	}
	reply(w, 200, out)
}

// worldGet answers 200 {entries: {}} for a missing file.
func (f *fake) worldGet(w http.ResponseWriter, r *http.Request) {
	name, _ := body(r)["name"].(string)
	if name == "" {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	if book, ok := f.worlds[name]; ok {
		reply(w, 200, book)
		return
	}
	reply(w, 200, map[string]any{"entries": map[string]any{}})
}

// worldEdit overwrites the file with data as sent.
func (f *fake) worldEdit(w http.ResponseWriter, r *http.Request) {
	b := body(r)
	name, _ := b["name"].(string)
	data, _ := b["data"].(map[string]any)
	if name == "" || data == nil || data["entries"] == nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	if f.editFile != nil {
		name = f.editFile(name)
	}
	if name != "" {
		f.worlds[name] = data
	}
	reply(w, 200, map[string]any{"ok": true})
}

type sessionKey struct{}

func withSession(r *http.Request, s *fakeSession) context.Context {
	return context.WithValue(r.Context(), sessionKey{}, s)
}

func sessionOf(r *http.Request) *fakeSession { return r.Context().Value(sessionKey{}).(*fakeSession) }
