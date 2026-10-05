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
- The avatar, uploaded only when it changes.
- Lorebooks as world books, linked to their characters. Pushing a lorebook
  replaces its world book's entries and keeps its ID.

Lumiverse stores CCv3 fields it has no place for in the plugin's own keys:
`extensions.innkeeper` on characters and entries, and `metadata` on world
books. Leave them alone; the plugin uses them to find its earlier copies and
to restore entry IDs, names, and order.

Lumiverse adds about 17 settings to each world book entry's `extensions`,
such as `sticky` and `probability`. They come back with the lorebook, and
adopting changes from Lumiverse brings them into the library.
