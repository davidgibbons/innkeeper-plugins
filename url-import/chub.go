package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"strings"
)

const chubAPI = "https://api.chub.ai"

// chub resolves chub.ai and characterhub.org pages. The card it fetches
// already carries extensions.chub, which the core's chub detector reads.
func chub(ctx context.Context, f *fetcher, u *url.URL) (found, error) {
	dir, ref, ok := chubPath(u.Path)
	if !ok {
		return found{}, invalid(errors.New("a chub.ai URL looks like https://chub.ai/characters/{user}/{slug} or https://chub.ai/lorebooks/{user}/{slug}"))
	}
	item := "https://chub.ai/" + dir + "/" + ref
	var meta struct {
		Node struct {
			ID         json.Number `json:"id"`
			Name       string      `json:"name"`
			MaxResURL  string      `json:"max_res_url"`
			Definition struct {
				EmbeddedLorebook json.RawMessage `json:"embedded_lorebook"`
			} `json:"definition"`
		} `json:"node"`
	}
	if err := f.getJSON(ctx, "chub.ai", chubAPI+"/api/"+dir+"/"+ref+"?full=true", &meta); err != nil {
		return found{}, err
	}
	if dir == "lorebooks" {
		if lb := meta.Node.Definition.EmbeddedLorebook; len(lb) == 0 || string(lb) == "null" {
			return found{}, permanent(errors.New("chub.ai sent no lorebook; its definition may be hidden"))
		}
		return found{item: item, lorebook: meta.Node.Definition.EmbeddedLorebook, name: meta.Node.Name}, nil
	}
	if meta.Node.ID == "" {
		return found{}, permanent(errors.New("chub.ai sent no card ID"))
	}
	b, err := f.get(ctx, "chub.ai", chubAPI+"/api/v4/projects/"+meta.Node.ID.String()+"/repository/files/card.json/raw")
	if err != nil {
		return found{}, err
	}
	got := found{item: item, card: b}
	if meta.Node.MaxResURL != "" {
		if got.avatar, err = f.get(ctx, "chub.ai", meta.Node.MaxResURL); err != nil {
			return found{}, err
		}
	}
	return got, nil
}

// chubPath reads /characters/{user}/{slug}, /lorebooks/{user}/{slug}, or
// /{user}/{slug}, a character. ref is {user}/{slug}, path-escaped.
func chubPath(p string) (dir, ref string, ok bool) {
	parts := strings.Split(strings.Trim(p, "/"), "/")
	switch {
	case len(parts) == 3 && (parts[0] == "characters" || parts[0] == "lorebooks"):
		dir, parts = parts[0], parts[1:]
	case len(parts) == 2 && parts[0] != "characters" && parts[0] != "lorebooks":
		dir = "characters"
	default:
		return "", "", false
	}
	if !segment(parts[0]) || !segment(parts[1]) {
		return "", "", false
	}
	return dir, url.PathEscape(parts[0]) + "/" + url.PathEscape(parts[1]), true
}
