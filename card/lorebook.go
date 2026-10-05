package card

import (
	"bytes"
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
)

// ErrNotLorebook means the input isn't a lorebook this package can read.
var ErrNotLorebook = errors.New("not a lorebook")

// stExtensionKeys renames world info fields kept under an entry's
// extensions to the names SillyTavern gives them there, so it reads them
// back when it imports the book. Fields not listed keep their names.
var stExtensionKeys = map[string]string{
	"excludeRecursion":          "exclude_recursion",
	"displayIndex":              "display_index",
	"outletName":                "outlet_name",
	"groupOverride":             "group_override",
	"groupWeight":               "group_weight",
	"preventRecursion":          "prevent_recursion",
	"delayUntilRecursion":       "delay_until_recursion",
	"scanDepth":                 "scan_depth",
	"matchWholeWords":           "match_whole_words",
	"useGroupScoring":           "use_group_scoring",
	"caseSensitive":             "case_sensitive",
	"automationId":              "automation_id",
	"matchPersonaDescription":   "match_persona_description",
	"matchCharacterDescription": "match_character_description",
	"matchCharacterPersonality": "match_character_personality",
	"matchCharacterDepthPrompt": "match_character_depth_prompt",
	"matchScenario":             "match_scenario",
	"matchCreatorNotes":         "match_creator_notes",
	"ignoreBudget":              "ignore_budget",
}

// DecodeLorebook reads a standalone lorebook file: a CCv3 character_book,
// one wrapped as {"spec": "lorebook_v3", "data": ...}, or SillyTavern world
// info. It returns a CCv3 character_book. name names a book that has none,
// such as the file name without its extension. Every error wraps
// ErrNotLorebook.
func DecodeLorebook(b []byte, name string) (json.RawMessage, error) {
	b = bytes.TrimPrefix(b, []byte("\xef\xbb\xbf"))
	var top map[string]json.RawMessage
	if err := json.Unmarshal(b, &top); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrNotLorebook, err)
	}
	var spec string
	_ = json.Unmarshal(top["spec"], &spec)
	if spec == "lorebook_v3" {
		var data map[string]json.RawMessage
		if err := json.Unmarshal(top["data"], &data); err != nil || data == nil {
			return nil, fmt.Errorf("%w: data must be an object", ErrNotLorebook)
		}
		top = data
	}
	entries := bytes.TrimSpace(top["entries"])
	switch {
	case len(entries) > 0 && entries[0] == '[':
		var list []map[string]json.RawMessage
		if err := json.Unmarshal(entries, &list); err != nil {
			return nil, fmt.Errorf("%w: entries: %w", ErrNotLorebook, err)
		}
	case len(entries) > 0 && entries[0] == '{':
		list, err := fromWorldInfo(entries)
		if err != nil {
			return nil, fmt.Errorf("%w: entries: %w", ErrNotLorebook, err)
		}
		if top["entries"], err = json.Marshal(list); err != nil {
			return nil, err
		}
	default:
		return nil, fmt.Errorf("%w: no entries list", ErrNotLorebook)
	}
	var current string
	if json.Unmarshal(top["name"], &current) != nil || current == "" {
		top["name"], _ = json.Marshal(name)
	}
	// CCv3 requires extensions on a character_book.
	if e := bytes.TrimSpace(top["extensions"]); len(e) == 0 || e[0] != '{' {
		top["extensions"] = json.RawMessage("{}")
	}
	return json.Marshal(top)
}

// fromWorldInfo converts world info entries, an object keyed by uid, to a
// character_book entries list, as SillyTavern does.
func fromWorldInfo(raw json.RawMessage) ([]map[string]any, error) {
	var byID map[string]map[string]json.RawMessage
	if err := json.Unmarshal(raw, &byID); err != nil {
		return nil, err
	}
	// Keys are decimal uids; ordering by length, then text, sorts them as numbers.
	ids := slices.SortedFunc(maps.Keys(byID), func(a, b string) int {
		return cmp.Or(cmp.Compare(len(a), len(b)), cmp.Compare(a, b))
	})
	entries := make([]map[string]any, 0, len(ids))
	for _, id := range ids {
		wi := byID[id]
		var ext map[string]json.RawMessage
		_ = json.Unmarshal(wi["extensions"], &ext)
		if ext == nil {
			ext = map[string]json.RawMessage{}
		}
		// SillyTavern keys may be /regex/, so use_regex is always true.
		e := map[string]any{"keys": []string{}, "content": "", "enabled": true, "insertion_order": 100,
			"use_regex": true, "extensions": ext}
		for k, v := range wi {
			// CCv3 forbids null in the fields these map to, so keep the defaults.
			if string(v) == "null" && slices.Contains([]string{"uid", "key", "keysecondary", "order", "comment", "content", "constant", "selective", "disable"}, k) {
				continue
			}
			switch k {
			case "extensions":
			case "uid":
				e["id"] = v
			case "key":
				e["keys"] = v
			case "keysecondary":
				e["secondary_keys"] = v
			case "order":
				e["insertion_order"] = v
			case "comment", "content", "constant", "selective":
				e[k] = v
			case "disable":
				e["enabled"] = string(v) != "true"
			case "position":
				e["position"] = "after_char"
				if string(v) == "0" {
					e["position"] = "before_char"
				}
				ext[k] = v
			default:
				ext[cmp.Or(stExtensionKeys[k], k)] = v
			}
		}
		entries = append(entries, e)
	}
	return entries, nil
}
