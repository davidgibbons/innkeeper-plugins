# url-import

Imports one card or lorebook from its page on a public site. Import params
are `{"url": "<page URL>"}`.

| Site | Page URLs |
|---|---|
| chub.ai | `chub.ai/characters/{user}/{slug}`, `chub.ai/{user}/{slug}`, `chub.ai/lorebooks/{user}/{slug}`. `characterhub.org`, `venus.chub.ai`, `www.chub.ai`, and `www.characterhub.org` work too |
| RisuRealm | `realm.risuai.net/character/{uuid}` |
| Character Tavern | `character-tavern.com/character/{author}/{slug}` |

The card's page URL is added to its `data.source`, so the library links the
card to its page. Importing the same URL again adds nothing unless the card
changed.

A card whose definition the site hides fails to import. JanitorAI needs a
login, so it isn't supported, and neither are private chub.ai cards.

To try the live sites:

```bash
URL_IMPORT_REAL="https://chub.ai/characters/<user>/<slug> https://realm.risuai.net/character/<uuid>" go test ./url-import -run TestReal -v
```
