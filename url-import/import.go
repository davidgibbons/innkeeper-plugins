package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/url"
	"slices"
	"strings"
	"unicode"

	"github.com/davidgibbons/innkeeper-plugins/card"
	"github.com/davidgibbons/innkeeper-plugins/internal/pluginio"
	"github.com/davidgibbons/innkeeper/protocol"
)

// params are an import's settings: URL, the page of a card or lorebook.
type params struct {
	URL string `json:"url"`
}

// found is what a resolver fetched: card, bytes card.Decode reads, or
// lorebook, a CCv3 character_book.
type found struct {
	item     string // the canonical page URL
	card     []byte
	avatar   []byte // any image format; nil keeps the one card.Decode returns
	lorebook json.RawMessage
	name     string // names a lorebook that has none
}

// resolver fetches the card or lorebook on the page u.
type resolver func(ctx context.Context, f *fetcher, u *url.URL) (found, error)

// resolvers maps each lowercase host to its site's resolver.
var resolvers = map[string]resolver{
	"chub.ai":              chub,
	"www.chub.ai":          chub,
	"characterhub.org":     chub,
	"www.characterhub.org": chub,
	"venus.chub.ai":        chub,
	"character-tavern.com": tavern,
	"janitorai.com":        janitor,
	"www.janitorai.com":    janitor,
	"realm.risuai.net":     risu,
}

// janitor fails: anonymous requests get every definition field as null.
func janitor(context.Context, *fetcher, *url.URL) (found, error) {
	return found{}, permanent(errors.New("JanitorAI needs a login; not supported yet"))
}

// notifyFunc sends a notification to the core.
type notifyFunc func(method string, params any) error

// run answers import.run. A run is one fetch, so it sends no checkpoint.
func run(ctx context.Context, f *fetcher, blobTmp string, notify notifyFunc, raw json.RawMessage) (any, error) {
	var p protocol.ImportRunParams
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, invalid(err)
	}
	var ps params
	dec := json.NewDecoder(bytes.NewReader(p.Params))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&ps); err != nil {
		return nil, invalid(fmt.Errorf(`params must be {"url": "<page URL>"}: %w`, err))
	}
	u, err := url.Parse(strings.TrimSpace(ps.URL))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return nil, invalid(fmt.Errorf("params: url %q is not an http or https URL", ps.URL))
	}
	resolve, ok := resolvers[strings.ToLower(u.Hostname())]
	if !ok {
		return nil, invalid(fmt.Errorf("no importer for %s", u.Hostname()))
	}
	got, err := resolve(ctx, f, u)
	if err != nil {
		return nil, err
	}
	if err := emit(got, p.Run, blobTmp, notify); err != nil {
		return nil, err
	}
	if err := notify(protocol.NotifyProgress, protocol.Progress{Run: p.Run, Done: 1, Total: 1}); err != nil {
		return nil, err
	}
	return struct{}{}, nil
}

// emit sends the card or lorebook got holds. A card gets its page URL in data.source.
func emit(got found, run, blobTmp string, notify notifyFunc) error {
	if got.lorebook != nil {
		book, err := card.DecodeLorebook(got.lorebook, got.name)
		if err != nil {
			return permanent(fmt.Errorf("%s: %w", got.item, err))
		}
		return notify(protocol.NotifyEmitLorebook, protocol.EmitLorebook{Run: run, Item: got.item, Lorebook: book})
	}
	c, img, err := card.Decode(got.card)
	if err != nil {
		return permanent(fmt.Errorf("%s: %w", got.item, err))
	}
	if c, err = withSource(c, got.item); err != nil {
		return permanent(fmt.Errorf("%s: %w", got.item, err))
	}
	if got.avatar != nil {
		if png, err := card.AsPNG(got.avatar); err == nil {
			img = png
		} else {
			// The card matters more than its picture; keep the one Decode returned.
			log.Printf("%s: no avatar: %v", got.item, err)
		}
	}
	ev := protocol.EmitCard{Run: run, Item: got.item, Card: c}
	if img != nil {
		if ev.Avatar, err = pluginio.WriteTmp(blobTmp, "avatar-*.png", img); err != nil {
			return err
		}
	}
	return notify(protocol.NotifyEmitCard, ev)
}

// withSource appends page to the CCv3 card's data.source, which core detection reads.
func withSource(c json.RawMessage, page string) (json.RawMessage, error) {
	var top, data map[string]json.RawMessage
	if err := json.Unmarshal(c, &top); err != nil {
		return nil, err
	}
	if err := json.Unmarshal(top["data"], &data); err != nil {
		return nil, fmt.Errorf("data: %w", err)
	}
	var src []any
	if s, ok := data["source"]; ok {
		if err := json.Unmarshal(s, &src); err != nil {
			return nil, fmt.Errorf("data.source: %w", err)
		}
	}
	if slices.Contains(src, any(page)) {
		return c, nil
	}
	var err error
	if data["source"], err = json.Marshal(append(src, page)); err != nil {
		return nil, err
	}
	if top["data"], err = json.Marshal(data); err != nil {
		return nil, err
	}
	return json.Marshal(top)
}

// segment reports whether s can stand as one path segment of a site URL.
// Core stores refs unescaped, so a ref must not smuggle in a query, a fragment, or blank space.
func segment(s string) bool {
	return s != "" && s != "." && s != ".." && !strings.ContainsAny(s, "?#\\") &&
		strings.IndexFunc(s, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }) < 0
}

// pathRef splits ref, exactly {a}/{b}, into its segments.
func pathRef(ref string) (a, b string, ok bool) {
	a, b, ok = strings.Cut(ref, "/")
	if !ok || !segment(a) || !segment(b) || strings.Contains(b, "/") {
		return "", "", false
	}
	return a, b, true
}
