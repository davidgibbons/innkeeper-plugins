# chub-archive

A catalog over the chub.ai archive takeout's SQL dump
(`char_archive_database_01-05-2025.sql`). Innkeeper searches its cards and
lorebooks and adopts single versions into the library.

## Setup

1. Mount the archive where the core can read it.
2. Set **Dump file** to the dump's path, and **Image folders** to the
   `hashed-data` folders under the archive's `sql` folder.
3. Start an ingest. Until the web UI has an Archive page, start it through
   the API with `POST /catalogs/<instance id>/ingest`.

## Behavior

- Cards come from the chub, Pygmalion Booru, Nyai.me, RisuRealm, webring,
  and generic (JanitorAI and catbox) tables, and lorebooks from chub's.
- An item's ID is `<table>:<id>`. The webring and generic tables have no
  ID, so theirs is the row's card hash and each row is its own item.
- Each row is one version, numbered by its `increment`. Booru items have
  one version, `1`.
- Chub items take their tags, rating, date added, and hidden flag from
  chub's per-item tables.
- An ingest reads the whole dump, about 12 GB, and resumes from its last
  checkpoint if it stops. Re-ingesting skips versions it already has, so
  their ratings and tags don't change.
- Rows that won't decode are skipped and logged with their table, ID, and
  increment.
- If an image can't be read for any reason other than being missing, the
  ingest stops, so it can retry from its last checkpoint.
