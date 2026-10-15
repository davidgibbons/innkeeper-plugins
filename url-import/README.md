# url-import

Imports one card or lorebook from its page on a public site. Import params
are `{"url": "<page URL>"}`.

| Site | Page URLs |
|---|---|
| chub.ai | `chub.ai/characters/{user}/{slug}`, `chub.ai/{user}/{slug}`, `chub.ai/lorebooks/{user}/{slug}`. `characterhub.org`, `venus.chub.ai`, `www.chub.ai`, and `www.characterhub.org` work too |
| RisuRealm | `realm.risuai.net/character/{uuid}` |
| Character Tavern | `character-tavern.com/character/{author}/{slug}` |

The card's page URL is added to its `data.source`. Importing the same URL
again adds nothing unless the card changed.

It is also a `sites` plugin: it reads these sites' pages from a card's or
lorebook's `extensions.chub` and `data.source`, so the library links each
item to its pages however it was imported. It needs a core with the `sites`
capability; an older core refuses to load it, imports included.

It also pulls: the core checks each source of kind `chub`, `risu`, or
`character-tavern` by calling `target.get` with `remote_id` `kind:ref`, such
as `chub:alice/vyr`, and `target.get_lorebook` for a chub lorebook. A page
that is gone, private, or hides its definition answers `-32004`. A timeout, a
5xx, or a 429 is retryable. It never pushes. Requests to one site are at least
a second apart. Avatars come back without card chunks, so chub's card PNG
changes only when its picture does.

A card whose definition the site hides fails to import. JanitorAI needs a
login, so it isn't supported, and neither are private chub.ai cards.

To try the live sites:

```bash
URL_IMPORT_REAL="https://chub.ai/characters/<user>/<slug> https://realm.risuai.net/character/<uuid>" go test ./url-import -run TestReal -v
URL_IMPORT_FOLLOW="chub:<user>/<slug> risu:<uuid> character-tavern:<author>/<slug>" go test ./url-import -run TestRealFollow -v
```
