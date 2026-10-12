# chub-archive

A catalog over the chub.ai archive takeout's SQL dump
(`char_archive_database_01-05-2025.sql`). Innkeeper searches its cards and
lorebooks and adopts single versions into the library.

## Setup

1. Extract the takeout's `.7z` files. The plugin reads the extracted dump
   and image folders.
2. Mount the extracted archive where the core can read it. On Kubernetes,
   mount it read-only into both the API and worker containers. Keep it
   mounted after the ingest: `catalog.get` reads each avatar from it.
3. Set **Dump file** to the dump's path, and **Image folders** to the
   `hashed-data` folders under the archive's `sql` folder.
4. Start an ingest. Until the web UI has an Archive page, start it through
   the API with `POST /catalogs/<instance id>/ingest`.

With the archive on a network share, an ingest of the 01-05-2025 dump took
4.5 hours and stored 267,473 items and 351,106 versions, about 6.5 GB in
Postgres.

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
- A version whose image is missing or unreadable (permission denied, a
  folder, over 32 MiB, or not an image) is stored without one. Other image
  read errors stop the ingest, and the core retries it from its last
  checkpoint.

## Testing against a real dump

`TestRealDump` ingests a real dump into the schema named by
`CHUB_DATABASE_URL`'s `search_path`, which must exist. It retries from its
checkpoint as the core does, and skips unless `CHUB_DUMP` is set:

```bash
A=/path/to/archive/sql
CHUB_DUMP=$A/database_01-05-2025/char_archive_database_01-05-2025.sql \
CHUB_IMAGES=$A/hashed-data_part1/hashed-data:$A/hashed-data_01-09-2025/hashed-data \
CHUB_DATABASE_URL='postgres://user:pass@localhost:5432/postgres?search_path=chub_real' \
go test ./chub-archive/ -run TestRealDump -count=1 -timeout 0 -v
```
