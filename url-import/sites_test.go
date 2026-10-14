package main

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/davidgibbons/innkeeper/protocol"
)

// The cases below are core's internal/library/detect_test.go, so url-import
// stores what core's own detectors stored.

func entity(lorebook bool) string {
	if lorebook {
		return protocol.KindLorebook
	}
	return protocol.KindCard
}

func detect(t *testing.T, data string, lorebook bool) []protocol.Source {
	t.Helper()
	raw, _ := json.Marshal(protocol.SitesDetectParams{Entity: entity(lorebook), Data: json.RawMessage(data)})
	got, err := detectSources(raw)
	if err != nil {
		t.Fatalf("sites.detect %s: %v", data, err)
	}
	res := got.(protocol.SitesDetectResult)
	if res.Sources == nil {
		t.Fatalf("sites.detect %s: sources is null, want a list", data)
	}
	return res.Sources
}

func parse(p protocol.SitesParseParams) (*protocol.Source, error) {
	raw, _ := json.Marshal(p)
	got, err := parseSource(raw)
	if err != nil {
		return nil, err
	}
	return got.(protocol.SitesParseResult).Source, nil
}

func TestDetect(t *testing.T) {
	const sample = `{"extensions": {"chub": {"id": 3604386, "full_path": "frerena/the-demon-of-desire-6bf0cd2c0915"}}}`
	tests := []struct {
		name, data string
		lorebook   bool
		want       *protocol.Source
	}{
		{"card", sample, false, &protocol.Source{Kind: "chub", Ref: "frerena/the-demon-of-desire-6bf0cd2c0915", ExternalID: "3604386", URL: "https://chub.ai/characters/frerena/the-demon-of-desire-6bf0cd2c0915"}},
		{"lorebook", sample, true, &protocol.Source{Kind: "chub", Ref: "frerena/the-demon-of-desire-6bf0cd2c0915", ExternalID: "3604386", URL: "https://chub.ai/lorebooks/frerena/the-demon-of-desire-6bf0cd2c0915"}},
		{"large id", `{"extensions":{"chub":{"id":12345678901234567,"full_path":"a/b"}}}`, false, &protocol.Source{Kind: "chub", Ref: "a/b", ExternalID: "12345678901234567", URL: "https://chub.ai/characters/a/b"}},
		{"overlong id", `{"extensions":{"chub":{"id":"xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx","full_path":"a/b"}}}`, false, &protocol.Source{Kind: "chub", Ref: "a/b", URL: "https://chub.ai/characters/a/b"}},
		{"no id", `{"extensions":{"chub":{"full_path":"a/b"}}}`, false, &protocol.Source{Kind: "chub", Ref: "a/b", URL: "https://chub.ai/characters/a/b"}},
		{"empty path", `{"extensions":{"chub":{"id":1,"full_path":""}}}`, false, nil},
		{"no extensions", `{"name":"x"}`, false, nil},
		{"chub not object", `{"extensions":{"chub":"x"}}`, false, nil},
		{"string id", `{"extensions":{"chub":{"id":"42","full_path":"a/b"}}}`, false, &protocol.Source{Kind: "chub", Ref: "a/b", ExternalID: "42", URL: "https://chub.ai/characters/a/b"}},
		{"dotdot", `{"extensions":{"chub":{"full_path":"../b"}}}`, false, nil},
		{"space", `{"extensions":{"chub":{"full_path":"a/b c"}}}`, false, nil},
		{"query", `{"extensions":{"chub":{"full_path":"a/b?x=1"}}}`, false, nil},
		{"three segments", `{"extensions":{"chub":{"full_path":"a/b/c"}}}`, false, nil},
		{"one segment", `{"extensions":{"chub":{"full_path":"a"}}}`, false, nil},
		{"nul", "{\"extensions\":{\"chub\":{\"full_path\":\"a/b\\u0000\"}}}", false, nil},
		{"nbsp", "{\"extensions\":{\"chub\":{\"full_path\":\"a/b c\"}}}", false, nil},
		{"chub data and source", `{"extensions":{"chub":{"id":7,"full_path":"a/b"}},"source":["https://chub.ai/characters/a/b"]}`, false, &protocol.Source{Kind: "chub", Ref: "a/b", ExternalID: "7", URL: "https://chub.ai/characters/a/b"}},
		{"risu source", `{"source":["risurealm:976E92F8-2C7E-4BF9-AEF4-586A7A753338","https://realm.risuai.net/character/976e92f8-2c7e-4bf9-aef4-586a7a753338"]}`, false, &protocol.Source{Kind: "risu", Ref: "976e92f8-2c7e-4bf9-aef4-586a7a753338", URL: "https://realm.risuai.net/character/976e92f8-2c7e-4bf9-aef4-586a7a753338"}},
		{"tavern source", `{"source":["https://character-tavern.com/character/ann/brakka"]}`, false, &protocol.Source{Kind: "character-tavern", Ref: "ann/brakka", URL: "https://character-tavern.com/character/ann/brakka"}},
		{"unknown source", `{"source":["https://example.com/x", 5, null]}`, false, nil},
		{"source not a list", `{"source":"https://chub.ai/characters/a/b"}`, false, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := detect(t, tt.data, tt.lorebook)
			if tt.want == nil {
				if len(got) != 0 {
					t.Fatalf("got %v, want none", got)
				}
				return
			}
			if len(got) != 1 || got[0] != *tt.want {
				t.Fatalf("got %+v, want %+v", got, *tt.want)
			}
		})
	}
}

func TestDetectSourceFit(t *testing.T) {
	if got := detect(t, `{"source":["https://chub.ai/lorebooks/a/b"]}`, false); len(got) != 0 {
		t.Errorf("a card took a lorebook page as a source: %+v", got)
	}
	if got := detect(t, `{"source":["https://realm.risuai.net/character/976e92f8-2c7e-4bf9-aef4-586a7a753338"]}`, true); len(got) != 0 {
		t.Errorf("a lorebook took a risu source: %+v", got)
	}
	got := detect(t, `{"extensions":{"chub":{"id":7,"full_path":"a/b"}},"source":["https://realm.risuai.net/character/976e92f8-2c7e-4bf9-aef4-586a7a753338"]}`, false)
	if len(got) != 2 || got[0].Kind != "chub" || got[1].Kind != "risu" {
		t.Errorf("sources = %+v, want chub then risu", got)
	}
}

func TestDetectKeepsAtMostMaxSources(t *testing.T) {
	var pages []string
	for i := range protocol.MaxSources + 5 {
		pages = append(pages, fmt.Sprintf(`"https://chub.ai/characters/u/c%d"`, i))
	}
	if got := detect(t, `{"source":[`+strings.Join(pages, ",")+`]}`, false); len(got) != protocol.MaxSources {
		t.Errorf("got %d sources, want %d", len(got), protocol.MaxSources)
	}
}

func TestDetectRefusesBadParams(t *testing.T) {
	for _, p := range []protocol.SitesDetectParams{
		{Entity: protocol.KindCard, Data: json.RawMessage(`"x"`)},
		{Entity: protocol.KindCard, Data: json.RawMessage(`null`)},
		{Entity: protocol.KindCard, Data: json.RawMessage(`[]`)},
		{Entity: "persona", Data: json.RawMessage(`{}`)},
	} {
		raw, _ := json.Marshal(p)
		if _, err := detectSources(raw); rpcErr(t, err).Code != protocol.CodeInvalidParams {
			t.Errorf("%s: err = %v, want invalid params", raw, err)
		}
	}
}

func TestParseURL(t *testing.T) {
	const ref = "frerena/the-demon-of-desire-6bf0cd2c0915"
	for _, u := range []string{
		"https://chub.ai/characters/" + ref,
		"https://www.chub.ai/characters/" + ref,
		"https://chub.ai/characters/" + ref + "/",
		"https://chub.ai/characters/" + ref + "?tab=chats",
		"https://chub.ai/lorebooks/" + ref,
		"http://chub.ai/characters/" + ref,
		"https://CHUB.AI/characters/" + ref,
		"https://chub.ai:443/characters/" + ref,
		" https://chub.ai/characters/" + ref + " ",
	} {
		s, err := parse(protocol.SitesParseParams{Entity: protocol.KindCard, URL: u})
		if err != nil || s == nil || s.Kind != "chub" || s.Ref != ref || s.URL != "https://chub.ai/characters/"+ref {
			t.Errorf("%s: got %+v, %v", u, s, err)
		}
	}
	for _, u := range []string{"https://example.com/x", "https://chub.ai/characters/onlyone", "https://chub.ai/characters/a/b/c", "not a url",
		"https://chub.ai.evil.com/characters/a/b", "https://evilchub.ai/characters/a/b", "https://chub.ai@evil.com/characters/a/b",
		"ftp://chub.ai/characters/a/b", "https://chub.ai/characters/../b", "https://chub.ai/characters/%2e%2e/b", "https://chub.ai/characters/a%2Fb/c", "https://chub.ai/characters/a/b%00", "https://chub.ai/characters/a/b%C2%A0c", "//chub.ai/characters/a/b", "https://chub.ai/characters/a/%20b", "https://chub.ai/characters/a/b%3Fx", "https://chub.ai/characters/a/%5Cb",
		"https://janitorai.com/characters/a/b"} {
		if s, err := parse(protocol.SitesParseParams{Entity: protocol.KindCard, URL: u}); err != nil || s != nil {
			t.Errorf("%s: got %+v, %v, want null", u, s, err)
		}
	}
}

func TestParseURLLorebook(t *testing.T) {
	s, err := parse(protocol.SitesParseParams{Entity: protocol.KindLorebook, URL: "https://www.chub.ai/lorebooks/a/b?x=1"})
	if err != nil || s == nil || s.Kind != "chub" || s.Ref != "a/b" || s.URL != "https://chub.ai/lorebooks/a/b" {
		t.Fatalf("got %+v, %v", s, err)
	}
}

func TestParseURLSites(t *testing.T) {
	const risuID = "976e92f8-2c7e-4bf9-aef4-586a7a753338"
	for u, want := range map[string]protocol.Source{
		"https://characterhub.org/characters/a/b":           {Kind: "chub", Ref: "a/b", URL: "https://chub.ai/characters/a/b"},
		"https://www.characterhub.org/lorebooks/a/b":        {Kind: "chub", Ref: "a/b", URL: "https://chub.ai/lorebooks/a/b"},
		"https://venus.chub.ai/characters/a/b":              {Kind: "chub", Ref: "a/b", URL: "https://chub.ai/characters/a/b"},
		"https://chub.ai/a/b":                               {Kind: "chub", Ref: "a/b", URL: "https://chub.ai/characters/a/b"},
		"https://realm.risuai.net/character/" + risuID:      {Kind: "risu", Ref: risuID, URL: "https://realm.risuai.net/character/" + risuID},
		"risurealm:" + risuID:                               {Kind: "risu", Ref: risuID, URL: "https://realm.risuai.net/character/" + risuID},
		"https://character-tavern.com/character/ann/brakka": {Kind: "character-tavern", Ref: "ann/brakka", URL: "https://character-tavern.com/character/ann/brakka"},
	} {
		// A lorebook page is a lorebook's source, as core rebuilt the URL for the entity.
		if got, err := parse(protocol.SitesParseParams{Entity: entity(strings.Contains(u, "/lorebooks/")), URL: u}); err != nil || got == nil || *got != want {
			t.Errorf("%s: got %+v, %v", u, got, err)
		}
	}
	for _, u := range []string{
		"https://realm.risuai.net/character/not-a-uuid", "risurealm:x", "https://evil.example/character/" + risuID,
		"https://character-tavern.com/character/ann", "https://character-tavern.com/ann/brakka", "https://character-tavern.com/character/../x",
		"https://character-tavern.com/character/ann/b%20c",
	} {
		if s, err := parse(protocol.SitesParseParams{Entity: protocol.KindCard, URL: u}); err != nil || s != nil {
			t.Errorf("%s: got %+v, %v, want null", u, s, err)
		}
	}
	_, err := parse(protocol.SitesParseParams{Entity: protocol.KindLorebook, URL: "https://realm.risuai.net/character/" + risuID})
	if rpcErr(t, err).Code != protocol.CodeInvalidParams {
		t.Errorf("risu lorebook: err = %v, want invalid params", err)
	}
}

func TestParseRef(t *testing.T) {
	got, err := parse(protocol.SitesParseParams{Entity: protocol.KindLorebook, Kind: "chub", Ref: "a/b"})
	if err != nil || got == nil || got.URL != "https://chub.ai/lorebooks/a/b" {
		t.Fatalf("got %+v, %v", got, err)
	}
	got, err = parse(protocol.SitesParseParams{Entity: protocol.KindCard, Kind: "risu", Ref: "976E92F8-2C7E-4BF9-AEF4-586A7A753338"})
	if err != nil || got == nil || got.Ref != "976e92f8-2c7e-4bf9-aef4-586a7a753338" {
		t.Fatalf("risu: got %+v, %v", got, err)
	}
	if got, err := parse(protocol.SitesParseParams{Entity: protocol.KindCard, Kind: "other", Ref: "a/b"}); err != nil || got != nil {
		t.Errorf("other kind: got %+v, %v, want null", got, err)
	}
	for _, p := range []protocol.SitesParseParams{
		{Entity: protocol.KindCard, Kind: "chub", Ref: "a"},
		{Entity: protocol.KindCard, Kind: "chub", Ref: "../b"},
		{Entity: protocol.KindCard, Kind: "chub", Ref: "a/b c"},
		{Entity: protocol.KindLorebook, Kind: "risu", Ref: "976e92f8-2c7e-4bf9-aef4-586a7a753338"},
		{Entity: protocol.KindLorebook, Kind: "character-tavern", Ref: "ann/brakka"},
		{Entity: protocol.KindCard},
		{Entity: protocol.KindCard, URL: "https://chub.ai/a/b", Kind: "chub", Ref: "a/b"},
		{Entity: "persona", URL: "https://chub.ai/a/b"},
	} {
		if _, err := parse(p); rpcErr(t, err).Code != protocol.CodeInvalidParams {
			t.Errorf("%+v: err = %v, want invalid params", p, err)
		}
	}
}
