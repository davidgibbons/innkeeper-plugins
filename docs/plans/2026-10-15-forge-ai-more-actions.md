# forge-ai more actions Plan

**Goal:** Two actions from phase 6 of the Forge (Kaneo INN-59):
`alternate-greetings` on a card and `lore-entries` on a lorebook.

**Source design:** core repo, `docs/superpowers/specs/2026-10-14-forge-design.md`,
section "Actions". Protocol: core `docs/protocol.md`, section `action`. The
core's lorebook Forge is planned in core
`docs/superpowers/plans/2026-10-15-forge-6-lorebook.md`.

## Decisions

| Topic | Decision |
|---|---|
| `alternate-greetings` | Whole-card, `applies = "card"`, input `count` (1 to 10, default 3), library access. The model returns only the new greetings; the plugin appends them to the draft's `alternate_greetings` and drops blanks and exact repeats. |
| `lore-entries` | `applies = "lorebook"`, `instruction = true`, input `count` (1 to 20, default 5), library access. The instruction is the focus and carries the `@cards/` mentions that attach the source card. It is the lorebook's only instruction action, so the core's Reforge box runs it. |
| New entries | The model returns `comment`, `keys`, and `content` per entry. The plugin adds the defaults core's blank entry has (`enabled: true`, `insertion_order: 100`, `position: "before_char"`, `extensions: {}`) plus `use_regex: false`, which CCv3 requires. No `id`: the protocol asks a lorebook action to leave it off new entries. |
| Existing entries | Kept byte for byte, `id`s included. The model sees each one's `comment`, `keys`, and `content` only. |
| Duplicate keys | A new entry's keys that an existing entry already has, ignoring case, are dropped; an entry left with no keys is dropped. |
| Progress | One message per greeting or entry as the model starts it, out of `count`. |
| Shape | Each action declares the data key it appends to (`appends`). Append actions build their proposal from the draft plus the new items, not by replacing keys. |
| Version | 0.3.0 in `plugin.toml`, `main.go`, and `index.toml`. |

## Tasks

1. Prompts `alternate-greetings.tmpl` and `lore-entries.tmpl`; prompt data
   gains the existing greetings and the lorebook.
2. Declarations, input parsing, proposals, and per-item progress, with
   tests through the fake model server.
3. `additions` in `plugin.toml`, README, version bump.
4. `go vet`, `go test -race -count=1 ./...`, `go run ./internal/indexcheck`,
   `make check`.
