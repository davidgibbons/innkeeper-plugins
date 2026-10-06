# lumiverse

Pushes cards to a [Lumiverse](https://github.com/prolix-oc/Lumiverse) server
as characters, and lorebooks as world books, and reads both back so Innkeeper
can show changes made in Lumiverse.

```bash
innkeeper call POST /instances '{"plugin": "lumiverse", "name": "Lumiverse",
  "config": {"url": "https://lumiverse.example.com", "username": "owner"},
  "secrets": {"password": "<password>"}}'
```

`username` can be an email address. Use an account that owns the
characters; Lumiverse has no API keys, so the plugin signs in with the
password.

If Lumiverse rejects the password, the plugin stops signing in until you
change the instance's config or secrets. Five rejected sign-ins lock the
Innkeeper server out of all of Lumiverse for 15 minutes or more.

## What it syncs

- Card fields Lumiverse keeps, including `extensions` such as the depth
  prompt. Lumiverse's own settings, such as the voice, favorites, and
  expressions, stay as they are in Lumiverse and don't show as changes.
  SillyTavern's `fav` and `world` keys, which a SillyTavern import leaves
  behind, are dropped when reading.
- The avatar, uploaded only when it changes, and read back when asked.
- Lorebooks as world books, linked to their characters. Pushing a lorebook
  replaces its world book's entries and keeps its ID. Reading a character
  returns the world books it links to.

Lumiverse stores CCv3 fields it has no place for in the plugin's own keys:
`extensions.innkeeper` on characters and entries, and `metadata` on world
books. Leave them alone; the plugin uses them to find its earlier copies and
to restore entry IDs, names, and order.

An entry reads back exactly as pushed until it changes in Lumiverse. A
changed entry comes back as Lumiverse exports it, with about 17 settings
such as `sticky` and `probability` under `extensions`, and adopting it brings
them into the library.

Point only one Innkeeper at a Lumiverse account. The plugin finds its
earlier copies by keys that are unique only within one Innkeeper, so a
second one could take over the first's characters and world books.
