# forge-ai skills Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** `forge-ai` keeps Agent Skills in its own database, lets the owner
manage and import them through the core's `records` capability, and gives
them to the model during action runs.

**Architecture:** A `skill` table in the plugin's schema, one row per skill
per instance. The `records.*` methods read and write it. Each skill has an
`enabled` switch. A run puts each forced skill's body in the system
prompt and offers `load_skill` and `load_skill_file` tools, which join the
library tools in the existing tool loop in `chat.go`.

**Tech Stack:** Go 1.27, `github.com/davidgibbons/innkeeper/protocol`
(records types, unreleased), `pgx/v5`, `archive/zip`, `gopkg.in/yaml.v3`
(already in the module graph), any-llm-go.

**Source design:** core repo, `docs/superpowers/specs/2026-10-14-forge-design.md`,
section "Skills in the AI plugin". Protocol: core `docs/protocol.md`,
sections `action` (the `skills` reply field and `context.skills`) and
`records`. Skill format: <https://agentskills.io/specification>.

---

## Decisions

| Topic | Decision |
|---|---|
| Core version | `records` is on core branch `forge-5-items`, not yet released. The last commit builds against it with a `replace` to `../innkeeper-forge-5`; swap it for a version bump once core merges. |
| Record type | `[records.skill]`, label "Skills", `import = ["file", "url"]`. |
| Record ID | The skill's name. Agent Skills names are lowercase letters, digits, and single hyphens, so they are valid record IDs and `@skills/` names. Renaming a skill changes its ID. |
| Record schema | `name`, `description`, `body` (`text/markdown`), `enabled` (default true), and `files`, an object from relative path to text. The settings form renders `files` as raw JSON, which is enough for small reference files. |
| Name rules | The Agent Skills ones: 1 to 64 of `a-z`, `0-9`, `-`, not starting or ending with `-`, no `--`. Description 1 to 1024 characters. The body must not be empty and is at most 1 MiB. Body, description, and file paths must be UTF-8 without NUL. |
| Enabled skills | An `enabled` boolean on the record, true for a new or imported skill so an import is usable at once. A rename keeps it; a reimport leaves it as it was. |
| Storage | `skill (instance, name, description, body, files jsonb, enabled)`, key `(instance, name)`. Instances share the schema, as in catalogkit. One `CREATE TABLE IF NOT EXISTS` under an advisory lock, since the API and the worker start instances together. |
| Put | Without `id`, refuses a name that exists, so a new skill can't overwrite one. With `id`, replaces it and may rename it; renaming onto another skill's name is refused. |
| Import | `records.import` takes a `SKILL.md` or a zip, by upload or `http`/`https` URL; zip is detected by its magic bytes, not the name. It replaces a skill of the same name. |
| Zip | One `SKILL.md` per archive, at any depth; its folder is the skill's root and files outside it are ignored. Refuse an entry whose path is absolute, uses `..`, or contains `\` (zip slip), even though nothing is written to disk. Limits: archive 8 MiB, 200 entries, 1 MiB per file, 4 MiB of text in all, 64 reference files. Read through a size-limited reader, never trusting the header sizes. Skip files that aren't text (not valid UTF-8, or holding a NUL byte), such as images, judged by their first 8 KiB before any size limit; only text counts toward the limits. |
| URL | `http`/`https` only, 30 s timeout, at most 8 MiB, the default redirect limit. Private and loopback addresses are allowed: the plugin runs on the owner's network, importing needs write access in the core, and refusing would break skills served from a LAN Git server. |
| Forced skills | Each name in `context.skills` must be an enabled skill, or the run fails with `-32602`, as does more than 256 KiB of forced instructions. Their bodies go in the system prompt under `## Skills`, with their reference file paths. |
| Tools | `load_skill(name)` returns the body and the list of reference files; `load_skill_file(name, path)` returns one file. Offered when any skill is enabled, to every action, beside the library tools for library actions. `load_skill`'s description lists every enabled skill's name and description, so a model without tools, after the retry drops them, sees only the forced skills. |
| No database | Without `database_url`, records methods fail with a plain error (not `-32601`), `action.list` lists no skills, and any forced skill is unknown. If listing skills fails, `action.list` logs it and lists none, and a run fails only if it forces a skill. |

## Files

| File | Responsibility |
|---|---|
| `forge-ai/skill.go` | The `skill` type, its validation, `SKILL.md` parsing, zip and URL import |
| `forge-ai/store.go` | Opening the database and the `skill` table queries |
| `forge-ai/records.go` | The `records.*` handlers |
| `forge-ai/chat.go` | The tool loop takes a `toolbox` of tools and a dispatcher, in place of the `library` func |
| `forge-ai/skilltools.go` | `load_skill` and `load_skill_file`, and the forced skills' prompt section |
| `forge-ai/main.go` | Initialize opens the store; dispatch for `records.*`; `action.list` adds `skills` |
| `forge-ai/plugin.toml`, `index.toml` | `records` capability, `[records.skill]`, `database = true`, version 0.2.0 |

## Tasks

Each task is test-first: write the test, see it fail, write the code, see it
pass, commit.

### Task 1: Parse and check a skill

- [ ] Tests in `forge-ai/skill_test.go`: `TestParseSkillMarkdown` (frontmatter
  with a quoted and a folded description, body trimmed),
  `TestParseSkillRejects` (no frontmatter, bad name, `--`, empty description,
  description over 1024, empty body).
- [ ] `parseSkillMarkdown(b []byte) (skill, error)` and `(skill).check() error`.
  Errors are `-32602` with an owner-readable message.
- [ ] Commit `feat(forge-ai): parse Agent Skills SKILL.md files`.

### Task 2: Import a zip or a URL

- [ ] Tests: `TestImportZip` (folder `college/` with `SKILL.md`,
  `references/campus.md`, and a PNG that is skipped), `TestImportZipRefusesSlip`
  (`../evil.md`), `TestImportZipRefusesTwoSkills`, `TestImportZipLimits`
  (an entry over 1 MiB that claims to be small), `TestImportURL` against
  `httptest` for both a `SKILL.md` and a zip, and a 404.
- [ ] `parseUpload(b []byte) (skill, error)`, `fetch(ctx, url) ([]byte, error)`.
- [ ] Commit `feat(forge-ai): import skills from a zip or a URL`.

### Task 3: Store skills and answer records methods

- [ ] Tests in `forge-ai/records_test.go` against `pgtest.URL`, through the
  fake host: put, list, get, rename by put, duplicate create refused, delete,
  `-32004` for an unknown ID, `-32602` for an unknown type, import of an
  uploaded file, two instances kept apart.
- [ ] `store.go`, `records.go`, and `main.go` dispatch.
- [ ] Commit `feat(forge-ai): keep skills as records in the plugin's database`.

### Task 4: Enabled skills in action.list

- [ ] Test `TestActionListEnabledSkills`: `college` enabled, another skill
  disabled; only `college` is listed, with its description.
- [ ] `enabled` on the record, in `store.go` and the schema in `plugin.toml`.
- [ ] Commit `feat(forge-ai): list enabled skills for @skills/ mentions`.

### Task 5: Skills in runs

- [ ] Tests in `forge-ai/run_test.go`: `TestLoadSkillToolCall` (model calls
  `load_skill` then `load_skill_file`; results hold the body and the file;
  no `library.call` reaches the host), `TestForcedSkillInPrompt`,
  `TestUnknownForcedSkillRefused` (`-32602`), `TestNoToolsKeepsForcedSkills`
  (400 then retry: no tools, forced body still in the system prompt),
  `TestSkillToolsOfferedToNonLibraryActions`.
- [ ] `toolbox` in `chat.go`; `skilltools.go`; `run` in `main.go` builds the
  toolbox and the prompt section.
- [ ] Commit `feat(forge-ai): give the model enabled skills during runs`.

### Task 6: Manifest, docs, version

- [ ] `plugin.toml`: `capabilities = ["action", "records"]`, `database = true`,
  `[records.skill]`. Version 0.2.0 in `plugin.toml`, `index.toml`, `main.go`.
- [ ] README section "Skills" with the import limits and the private-address
  decision.
- [ ] Commit `feat(forge-ai): declare skills records and release 0.2.0`.

### Task 7: Build against the unreleased core

- [ ] `go.mod`: `replace github.com/davidgibbons/innkeeper => ../innkeeper-forge-5`;
  `go mod tidy`.
- [ ] Commit `chore: build against the unreleased records protocol`.

## Checks

```bash
go vet ./...
INNKEEPER_TEST_DATABASE_URL=postgres://postgres:postgres@localhost:55432/postgres go test -race ./...
go run ./internal/indexcheck
INNKEEPER_TEST_DATABASE_URL=… INNKEEPER_PLUGIN_DATABASE_URL=… make check
```

`make check` must show the records and action contract checks passing for
forge-ai.
