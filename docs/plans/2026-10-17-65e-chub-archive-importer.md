# chub-archive as an importer

**Goal:** chub-archive imports the chub.ai archive's SQL dump into an
Innkeeper library: one `emit.card` or `emit.lorebook` per version, in dump
order. It stops being a catalog.

**Source design:** core repo, `docs/superpowers/specs/2026-10-16-libraries-design.md`,
build step 5. Protocol: core `docs/protocol.md`, `import`, and the manifest's
`bulk` and `enrichments` keys, at core commit `ed2dde7`.

## Decisions

| Topic | Decision |
|---|---|
| Core version | Pin core `ed2dde7` (branch `inn-65-import`), which has `emit.card`'s `version`, `created`, `values`, and `bulk`. |
| Params | `import.run` params are `{"dump": "<path>"}`. The image folders stay in the instance config, since they rarely change between imports. |
| Item ID | Unchanged: `<table>:<id>`, or `<table>:<card hash>` for webring and generic rows. |
| `version` | `Version <increment>`. Booru rows have no increment and send `Version 1`. |
| `created` | The version row's `added`. |
| Values | `rating` (score 0 to 5, on the grid) and `topics` (list) from chub's `chub_character` row; `site` (text) from the table; `hidden` (text `yes`, sent only when hidden) from the version row or chub's row. Lorebooks carry no values; `emit.lorebook` has none. |
| chub's per-item rows | Held in memory, about 270k small entries. The checkpoint records the byte offset of the `chub_character` block, and a resumed run re-reads that block before seeking to its offset. `chub_lorebook` is no longer read: it only fed lorebook tags and ratings. |
| Avatars | Read from the image folders as now, written to `blob_tmp` with the image type's extension, sent as `avatar`. Bytes that aren't PNG, WebP, JPEG, or GIF are left out. The core compares avatars by pixels, so no re-encoding. |
| Checkpoint | Unchanged otherwise: table, columns, byte offset, dump size, every 1,000 rows. A checkpoint from a dump of another size is refused. |
| Plugin state | `database = true` stays, so `initialize` can drop the old tables: `source_row`, `parent`, `item_version`, `item`, catalogkit's `migration` table and `tags_text` function, under an advisory lock. Dropping `migration` lets a rollback to 0.2 recreate its empty tables. The core keeps the checkpoint. |
| Image store | Removed with its `image_store` setting. The README says to delete the store's files by hand. |
| catalogkit | Stays. The `folder` catalog uses all of it: index, put, read, and serve. chub-archive stops importing it. |
| Version | 0.3.0 in `plugin.toml`, `main.go`, and `index.toml`; category `Import`, capability `import`. |

## Tasks

- [ ] Pin core `ed2dde7`; commit `chore: pin the core that imports into libraries`.
- [ ] Tests first (`chub-archive/import_test.go`), through `run`:
  - every version is emitted in dump order with item, version, created, and values; a lorebook as `emit.lorebook`; the bad row skipped;
  - Aria's avatar is a file in `blob_tmp`; Brin, whose image is missing, has none;
  - a run stopped inside `chub_character_def` resumes from its checkpoint and still sends Aria's chub values;
  - a checkpoint from another dump is refused; a missing dump or image folder is invalid params;
  - an unreadable image leaves the version without an avatar.
- [ ] Test (`chub-archive/main_test.go`): `dropOldTables` drops the 0.2 tables and is a no-op on an empty schema.
- [ ] Implement `run`, `dropOldTables`, and the manifest; delete `images.go`'s store code, `check-config.json`, the testdata store, and the catalog code; commit `feat(chub-archive): import the dump into a library`.
- [ ] Update `chub-archive/README.md`, `.gitignore`, and `index.toml`; commit `docs(chub-archive): describe importing into a library`.
- [ ] Run `go vet ./...`, `go test -race ./...`, `go run ./internal/indexcheck`, and `make check` with the database variables.
