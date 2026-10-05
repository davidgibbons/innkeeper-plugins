package main

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/davidgibbons/innkeeper/protocol"
)

// bookFields are the character_book fields SillyTavern doesn't use. They
// live in the world file's extensions.innkeeper.
var bookFields = []string{"description", "scan_depth", "token_budget", "recursive_scanning", "extensions"}

// stashedFields are entry fields SillyTavern drops or rewrites, kept in the
// entry's extensions.innkeeper.
var stashedFields = []string{"id", "name", "position", "priority"}

// setting is a SillyTavern entry field that a character_book entry keeps in
// its extensions. null is what SillyTavern's converter writes for a null.
type setting struct {
	camel, snake string
	null         any
}

// settings mirror convertCharacterBook and convertWorldInfoToCharacterBook.
var settings = []setting{
	{"excludeRecursion", "exclude_recursion", nil},
	{"preventRecursion", "prevent_recursion", false},
	{"delayUntilRecursion", "delay_until_recursion", false},
	{"probability", "probability", nil},
	{"useProbability", "useProbability", false},
	{"depth", "depth", 4.0},
	{"selectiveLogic", "selectiveLogic", 0.0},
	{"outletName", "outlet_name", ""},
	{"group", "group", ""},
	{"groupOverride", "group_override", false},
	{"groupWeight", "group_weight", nil},
	{"scanDepth", "scan_depth", nil},
	{"matchWholeWords", "match_whole_words", nil},
	{"useGroupScoring", "use_group_scoring", false},
	{"automationId", "automation_id", ""},
	{"role", "role", 0.0},
	{"vectorized", "vectorized", false},
	{"sticky", "sticky", nil},
	{"cooldown", "cooldown", nil},
	{"delay", "delay", nil},
	{"matchPersonaDescription", "match_persona_description", false},
	{"matchCharacterDescription", "match_character_description", false},
	{"matchCharacterPersonality", "match_character_personality", false},
	{"matchCharacterDepthPrompt", "match_character_depth_prompt", false},
	{"matchScenario", "match_scenario", false},
	{"matchCreatorNotes", "match_creator_notes", false},
	{"triggers", "triggers", []any{}},
	{"ignoreBudget", "ignore_budget", false},
}

// worldFile is an item of /api/worldinfo/list.
type worldFile struct {
	FileID     string         `json:"file_id"`
	Name       string         `json:"name"`
	Extensions map[string]any `json:"extensions"`
}

// createMu makes picking a free file name and writing it one step.
var createMu sync.Mutex

func (p *plugin) putLorebook(ctx context.Context, params json.RawMessage) (any, error) {
	var in protocol.TargetPutLorebookParams
	if err := json.Unmarshal(params, &in); err != nil {
		return nil, invalid(err)
	}
	var book map[string]any
	if err := json.Unmarshal(in.Lorebook, &book); err != nil || book == nil {
		return nil, invalid(errors.New("lorebook is not a JSON object"))
	}
	files, err := p.worldFiles(ctx)
	if err != nil {
		return nil, err
	}
	id, key := in.RemoteID, in.Key
	if id == "" && key != "" {
		for _, f := range files {
			if f.key() == key {
				id = f.FileID
				break
			}
		}
	}
	if id != "" {
		// /get answers a missing file with an empty one, so /list decides.
		i := slices.IndexFunc(files, func(f worldFile) bool { return f.FileID == id })
		if i < 0 {
			return nil, missingWorld(id)
		}
		return p.writeWorld(ctx, id, book, cmp.Or(key, files[i].key()))
	}
	// ponytail: covers one plugin process; two workers creating same-named
	// books at once can still pick one file. Needs a lock in SillyTavern.
	createMu.Lock()
	defer createMu.Unlock()
	if files, err = p.worldFiles(ctx); err != nil {
		return nil, err
	}
	name, _ := book["name"].(string)
	id = freeName(files, name)
	return p.writeWorld(ctx, id, book, key)
}

// key is the sync key of the push that created the file, or "".
func (f worldFile) key() string {
	k, _ := object(f.Extensions[innkeeperKey])["sync_key"].(string)
	return k
}

func missingWorld(id string) error {
	return protocol.NewError(protocol.CodeRemoteNotFound, id+": no such world file", false)
}

func (p *plugin) worldFiles(ctx context.Context) ([]worldFile, error) {
	var files []worldFile
	err := p.c.call(ctx, "/api/worldinfo/list", map[string]any{}, &files)
	return files, err
}

// writeWorld replaces world file id with book in one write, so a failure
// leaves the old file.
func (p *plugin) writeWorld(ctx context.Context, id string, book map[string]any, key string) (any, error) {
	stash := map[string]any{}
	if key != "" {
		stash["sync_key"] = key
	}
	for _, k := range bookFields {
		if v, ok := book[k]; ok {
			stash[k] = v
		}
	}
	entries := map[string]any{}
	list, _ := book["entries"].([]any)
	for i, e := range list {
		entries[strconv.Itoa(i)] = toWorldEntry(object(e), i)
	}
	data := map[string]any{"entries": entries, "extensions": map[string]any{innkeeperKey: stash}}
	if name, _ := book["name"].(string); name != "" {
		data["name"] = name
	}
	if err := p.c.call(ctx, "/api/worldinfo/edit", map[string]any{"name": id, "data": data}, nil); err != nil {
		return nil, err
	}
	return protocol.TargetPutResult{RemoteID: id}, nil
}

// freeName returns the file ID for a new world file named name, one that no
// file has. /edit overwrites a file without asking.
func freeName(files []worldFile, name string) string {
	base := fileID(truncate(cmp.Or(name, "Lorebook"), 200))
	if base == "" {
		base = "Lorebook"
	}
	for n := 1; ; n++ {
		id := base
		if n > 1 {
			id = fmt.Sprintf("%s %d", base, n)
		}
		// Case-insensitive, since the server's file system may be.
		if !slices.ContainsFunc(files, func(f worldFile) bool { return strings.EqualFold(f.FileID, id) }) {
			return id
		}
	}
}

// truncate cuts s to at most n bytes, on a character boundary.
func truncate(s string, n int) string {
	for len(s) > n {
		_, size := utf8.DecodeLastRuneInString(s)
		s = s[:len(s)-size]
	}
	return s
}

var (
	reservedName = regexp.MustCompile(`^\.+$|(?i)^(con|prn|aux|nul|com[0-9]|lpt[0-9])(\..*)?$`)
	trailing     = regexp.MustCompile(`[. ]+$`)
)

// fileID is the file ID SillyTavern gives a world file named name: the
// sanitize-filename package's sanitize(name + ".json"), without ".json".
func fileID(name string) string {
	s := strings.Map(func(r rune) rune {
		if strings.ContainsRune(`/?<>\:*|"`, r) || r < 0x20 || r >= 0x80 && r <= 0x9f {
			return -1
		}
		return r
	}, name+".json")
	if reservedName.MatchString(s) {
		s = ""
	}
	return strings.TrimSuffix(trailing.ReplaceAllString(s, ""), ".json")
}

// toWorldEntry is convertCharacterBook's entry, with only the settings the
// entry has, so a file read back before the browser saves it is unchanged.
func toWorldEntry(e map[string]any, index int) map[string]any {
	ext := map[string]any{}
	for k, v := range object(e["extensions"]) {
		if k != innkeeperKey {
			ext[k] = v
		}
	}
	comment, _ := e["comment"].(string)
	name, _ := e["name"].(string)
	comment = cmp.Or(comment, name)
	var position any = topPosition(e["position"])
	if v := ext["position"]; v != nil {
		position = v
	}
	out := map[string]any{
		"uid":          index,
		"displayIndex": index,
		"key":          listOr(e["keys"]),
		"keysecondary": listOr(e["secondary_keys"]),
		"comment":      comment,
		"content":      e["content"],
		"constant":     e["constant"] == true,
		"selective":    e["selective"] == true,
		"disable":      e["enabled"] != true,
		"addMemo":      comment != "",
		"position":     position,
	}
	if v, ok := e["insertion_order"]; ok {
		out["order"] = v
	}
	if v, ok := e["case_sensitive"].(bool); ok {
		out["caseSensitive"] = v
	} else if v, ok := ext["case_sensitive"]; ok && v != nil {
		out["caseSensitive"] = v
	}
	for _, s := range settings {
		if v, ok := ext[s.snake]; ok && v != nil {
			out[s.camel] = v
		}
	}
	stash := map[string]any{}
	for _, k := range stashedFields {
		if v, ok := e[k]; ok {
			stash[k] = v
		}
	}
	ext[innkeeperKey] = stash
	out["extensions"] = ext
	return out
}

func listOr(v any) []any {
	if l, ok := v.([]any); ok {
		return l
	}
	return []any{}
}

func (p *plugin) getLorebook(ctx context.Context, params json.RawMessage) (any, error) {
	var in protocol.TargetGetParams
	if err := json.Unmarshal(params, &in); err != nil {
		return nil, invalid(err)
	}
	files, err := p.worldFiles(ctx)
	if err != nil {
		return nil, err
	}
	if !slices.ContainsFunc(files, func(f worldFile) bool { return f.FileID == in.RemoteID }) {
		return nil, missingWorld(in.RemoteID)
	}
	var data map[string]any
	if err := p.c.call(ctx, "/api/worldinfo/get", map[string]any{"name": in.RemoteID}, &data); err != nil {
		return nil, err
	}
	name, _ := data["name"].(string)
	book := map[string]any{"name": cmp.Or(name, in.RemoteID)}
	stash := object(object(data["extensions"])[innkeeperKey])
	for _, k := range bookFields {
		if v, ok := stash[k]; ok {
			book[k] = v
		}
	}
	var stored []map[string]any
	for _, e := range object(data["entries"]) {
		stored = append(stored, object(e))
	}
	slices.SortFunc(stored, func(a, b map[string]any) int {
		return cmp.Or(cmp.Compare(number(a["displayIndex"]), number(b["displayIndex"])), cmp.Compare(number(a["uid"]), number(b["uid"])))
	})
	entries := make([]any, len(stored))
	for i, e := range stored {
		entries[i] = fromWorldEntry(e)
	}
	book["entries"] = entries
	raw, err := json.Marshal(book)
	if err != nil {
		return nil, err
	}
	return protocol.TargetGetLorebookResult{Lorebook: raw}, nil
}

func number(v any) float64 {
	if n, ok := v.(float64); ok {
		return n
	}
	return 1 << 50
}

// topPosition is the number SillyTavern gives a character_book position.
func topPosition(v any) float64 {
	if v == "before_char" {
		return 0
	}
	return 1
}

// fromWorldEntry is convertWorldInfoToCharacterBook's entry for st, with the
// stashed fields restored. Settings come back only where st has them.
func fromWorldEntry(st map[string]any) map[string]any {
	ext := map[string]any{}
	for k, v := range object(st["extensions"]) {
		ext[k] = v
	}
	stash, pushed := ext[innkeeperKey].(map[string]any)
	delete(ext, innkeeperKey)
	e := map[string]any{"enabled": st["disable"] != true, "extensions": ext}
	for from, to := range map[string]string{"key": "keys", "keysecondary": "secondary_keys", "comment": "comment",
		"content": "content", "constant": "constant", "selective": "selective", "order": "insertion_order"} {
		if v, ok := st[from]; ok {
			e[to] = v
		}
	}
	pos, hasPos := st["position"].(float64)
	_, extPos := ext["position"]
	position := any("after_char")
	if hasPos && pos == 0 {
		position = "before_char"
	}
	// Restore the pushed position while it still gives SillyTavern's.
	if pushed && (extPos || hasPos && topPosition(stash["position"]) == pos) {
		position = stash["position"]
	}
	if position != nil {
		e["position"] = position
	}
	if hasPos && (extPos || pos != topPosition(position)) {
		ext["position"] = pos
	}
	if _, ok := ext["display_index"]; ok {
		ext["display_index"] = st["displayIndex"]
	}
	if v, ok := st["caseSensitive"]; ok {
		if _, in := ext["case_sensitive"]; in || v == nil {
			ext["case_sensitive"] = v
		} else {
			e["case_sensitive"] = v
		}
	}
	for _, s := range settings {
		if v, ok := st[s.camel]; ok {
			if v == nil {
				v = s.null
			}
			ext[s.snake] = v
		}
	}
	// An entry added in the browser has no id; its uid could clash with a pushed one.
	for _, k := range []string{"id", "name", "priority"} {
		if v, ok := stash[k]; ok {
			e[k] = v
		}
	}
	return e
}

func (p *plugin) listLorebooks(ctx context.Context) (protocol.TargetListResult, error) {
	files, err := p.worldFiles(ctx)
	if err != nil {
		return protocol.TargetListResult{}, err
	}
	items := make([]protocol.TargetItem, 0, len(files))
	for _, f := range files {
		items = append(items, protocol.TargetItem{RemoteID: f.FileID, Name: f.Name})
	}
	return protocol.TargetListResult{Items: items}, nil
}
