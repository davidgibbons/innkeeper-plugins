package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
)

// greetings is the draft's alternate greetings.
func (r *run) greetings() ([]string, error) {
	var g []string
	if raw := r.draft["alternate_greetings"]; len(raw) > 0 && string(raw) != "null" {
		if err := json.Unmarshal(raw, &g); err != nil {
			return nil, errors.New("the card's alternate_greetings isn't a list of text")
		}
	}
	return g, nil
}

// withGreetings is the draft with the model's new greetings appended, less
// blanks and repeats.
func (r *run) withGreetings(raw json.RawMessage) (json.RawMessage, error) {
	greetings, err := r.greetings()
	if err != nil {
		return nil, err
	}
	var added []string
	if err := json.Unmarshal(raw, &added); err != nil {
		return nil, errors.New("the model's alternate_greetings isn't a list of text")
	}
	for _, g := range added {
		if g = strings.TrimSpace(g); g != "" && !slices.Contains(greetings, g) {
			greetings = append(greetings, g)
		}
	}
	return r.draftWith("alternate_greetings", greetings)
}

// briefEntry is the part of a lore entry the model reads and writes.
type briefEntry struct {
	Comment string   `json:"comment,omitempty"`
	Keys    []string `json:"keys"`
	Content string   `json:"content"`
}

// entry is a new CCv3 lore entry, with the defaults core gives a blank one
// and no id, which the protocol leaves to the core.
type entry struct {
	briefEntry
	Enabled        bool           `json:"enabled"`
	InsertionOrder int            `json:"insertion_order"`
	Position       string         `json:"position"`
	UseRegex       bool           `json:"use_regex"`
	Extensions     map[string]any `json:"extensions"`
}

// entries is the draft lorebook's entries as they are.
func (r *run) entries() ([]json.RawMessage, error) {
	var list []json.RawMessage
	if raw := r.draft["entries"]; len(raw) > 0 && string(raw) != "null" {
		if err := json.Unmarshal(raw, &list); err != nil {
			return nil, errors.New("the lorebook's entries aren't a list")
		}
	}
	return list, nil
}

// briefEntries is what the model sees of the draft's entries. One that
// isn't an entry object is left out; the proposal keeps it as it was.
func briefEntries(list []json.RawMessage) []briefEntry {
	brief := []briefEntry{}
	for _, raw := range list {
		var b briefEntry
		if json.Unmarshal(raw, &b) == nil {
			brief = append(brief, b)
		}
	}
	return brief
}

// sentLorebook is the lorebook as the model sees it.
func (r *run) sentLorebook() (map[string]any, error) {
	book := map[string]any{}
	for _, k := range []string{"name", "description"} {
		v, err := typed(k, r.draft[k])
		if err != nil {
			return nil, fmt.Errorf("the lorebook's %s: %w", k, err)
		}
		book[k] = v
	}
	list, err := r.entries()
	if err != nil {
		return nil, err
	}
	book["entries"] = briefEntries(list)
	return book, nil
}

// withEntries is the draft with the model's new entries appended. Existing
// entries stay byte for byte. A key an earlier entry has, ignoring case, is
// dropped, and so is an entry left without keys or content.
func (r *run) withEntries(raw json.RawMessage) (json.RawMessage, error) {
	list, err := r.entries()
	if err != nil {
		return nil, err
	}
	var added []briefEntry
	if err := json.Unmarshal(raw, &added); err != nil {
		return nil, fmt.Errorf("the model's entries: %w", err)
	}
	taken := map[string]bool{}
	for _, b := range briefEntries(list) {
		for _, k := range b.Keys {
			taken[strings.ToLower(strings.TrimSpace(k))] = true
		}
	}
	for _, b := range added {
		var keys []string
		for _, k := range b.Keys {
			k = strings.TrimSpace(k)
			if k != "" && !taken[strings.ToLower(k)] {
				taken[strings.ToLower(k)] = true
				keys = append(keys, k)
			}
		}
		b.Keys, b.Content, b.Comment = keys, strings.TrimSpace(b.Content), strings.TrimSpace(b.Comment)
		if len(keys) == 0 || b.Content == "" {
			continue
		}
		e, err := json.Marshal(entry{briefEntry: b, Enabled: true, InsertionOrder: 100, Position: "before_char",
			Extensions: map[string]any{}})
		if err != nil {
			return nil, err
		}
		list = append(list, e)
	}
	return r.draftWith("entries", list)
}

// draftWith is the draft with key set to v.
func (r *run) draftWith(key string, v any) (json.RawMessage, error) {
	out := maps.Clone(r.draft)
	var err error
	if out[key], err = json.Marshal(v); err != nil {
		return nil, err
	}
	return json.Marshal(out)
}
