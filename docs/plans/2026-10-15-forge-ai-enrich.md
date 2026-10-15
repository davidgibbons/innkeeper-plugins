# forge-ai enrich Plan

**Goal:** forge-ai offers the `enrich` capability, so the library can score
cards in bulk with the evaluate prompt. Phase 7 of the Forge (Kaneo INN-60).

**Source design:** core repo, `docs/superpowers/specs/2026-10-15-enrichments-design.md`,
section "forge-ai". Protocol: core `docs/protocol.md`, section `enrich`.

## Decisions

| Topic | Decision |
|---|---|
| Keys | `quality` (the report's `score`, `grid = true`) and one per evaluate dimension: `consistency`, `voice`, `first_impression`, `roleplayability`, `token_efficiency`, `field_governance`. All `score`, range 0 to 100, labeled as the report labels them. |
| One list | The dimension keys and labels come from `dimensions` in `actions.go`; a test checks that `plugin.toml` declares exactly those keys plus `quality`. |
| Code path | `enrich.run` runs each revision as an `evaluate` action run through the same prompt, completion, and report parsing as `action.run`. |
| Batch | Sequential, one model call per revision. A failed call or unparsable report becomes that revision's `error`; the rest go on. A settings error fails the whole call. |
| Range | Scores outside 0 to 100 are clamped to the nearest end. |
| `meta` | `{"model", "prompt"}`; `prompt` is `evaluate-` plus a short hash of `prompts/evaluate.tmpl`, so it changes whenever the prompt does. |
| Progress | `Evaluating {name} (n of m)` before each revision. |
| Cancel | Checked before each revision; a cancel during a call aborts it and ends the batch. |
| Version | 0.4.0 in `plugin.toml`, `main.go`, and `index.toml`. |

## Tasks

1. Split `action.run`'s completion out of request parsing, so `enrich.run`
   can call it with its own progress.
2. `enrich.run`, the manifest's `[enrichments]`, and tests through the fake
   model server: mapping, a failed revision, cancel, declarations in sync.
3. README, version bump, index entry.
4. Build against the core's unreleased `enrich` protocol with a `replace`,
   in its own commit.
