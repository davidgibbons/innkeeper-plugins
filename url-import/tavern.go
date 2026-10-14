package main

import (
	"context"
	"errors"
	"net/url"
	"strings"
)

const tavernCards = "https://ct-cards.storage.character-tavern.com/"

// tavern resolves Character Tavern pages. The card PNG is also the avatar.
func tavern(ctx context.Context, f *fetcher, u *url.URL) (found, error) {
	author, slug, ok := tavernPage(u.Path)
	if !ok {
		return found{}, invalid(errors.New("a Character Tavern URL looks like https://character-tavern.com/character/{author}/{slug}"))
	}
	ref := url.PathEscape(author) + "/" + url.PathEscape(slug)
	// Without action=download the card chunks come back as zTXt, which card.Decode doesn't read.
	b, err := f.get(ctx, "Character Tavern", tavernCards+ref+".png?action=download")
	return found{item: tavernURL(author, slug), card: b}, err
}

// tavernPage reads /character/{author}/{slug}. author and slug are unescaped.
func tavernPage(p string) (author, slug string, ok bool) {
	ref, ok := strings.CutPrefix(strings.Trim(p, "/"), "character/")
	if !ok {
		return "", "", false
	}
	return pathRef(ref)
}

// tavernURL is the canonical page of a Character Tavern card.
func tavernURL(author, slug string) string {
	return "https://character-tavern.com/character/" + url.PathEscape(author) + "/" + url.PathEscape(slug)
}
