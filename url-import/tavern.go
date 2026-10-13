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
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) != 3 || parts[0] != "character" || !segment(parts[1]) || !segment(parts[2]) {
		return found{}, invalid(errors.New("a Character Tavern URL looks like https://character-tavern.com/character/{author}/{slug}"))
	}
	ref := url.PathEscape(parts[1]) + "/" + url.PathEscape(parts[2])
	// Without action=download the card chunks come back as zTXt, which card.Decode doesn't read.
	b, err := f.get(ctx, "Character Tavern", tavernCards+ref+".png?action=download")
	return found{item: "https://character-tavern.com/character/" + ref, card: b}, err
}
