# forge-ai

Writes, revises, tightens, and evaluates character cards, and adds
lorebook entries, with a language model, through [any-llm-go](https://github.com/mozilla-ai/any-llm-go). It
offers the `action` capability, so it needs a core that has it.

Each instance is one model setup. `provider` and `model` are required.
`api_key` is optional for `openai-compatible`, `ollama`, `llamacpp`, and
`llamafile`; the other providers need one. `openai-compatible` also needs
`base_url`, such as `http://localhost:1234/v1` for LM Studio or
`https://openrouter.ai/api/v1`.

## Actions

| ID | Runs on | Does |
|---|---|---|
| `generate` | a new card | Writes a card from a concept, in first or third person, or as a scenario card. Takes a reference image |
| `revise` | a card | Changes the card as the instruction says |
| `evaluate` | a card | Reports a score, six dimension scores, contradictions, misplaced content, and suggestions |
| `tighten-card` | a card | Cuts filler and repetition across the card |
| `change-pov` | a card | Rewrites the card in first or third person |
| `tighten`, `expand`, `to-prose`, `rewrite` | one prose field | Edits that field; `rewrite` takes an instruction |
| `examples` | `mes_example` | Writes 1 to 10 example exchanges, 3 by default |
| `alternate-greetings` | a card | Adds 1 to 10 alternate greetings, 3 by default, after the existing ones |
| `lore-entries` | a lorebook | Adds 1 to 20 entries, 5 by default, about what the instruction asks, from the cards it mentions |

Whole-card actions send the model only the card's prose keys, `name`, and
`tags`; `extensions` and the rest stay as they were.

`lore-entries` sends the lorebook's name, description, and each entry's
`comment`, `keys`, and `content`. Existing entries stay as they were. A new
entry has no `id`, `enabled` true, `insertion_order` 100, `position`
`before_char`, and no regex. A key an earlier entry already has is dropped,
ignoring case, and so is an entry left without keys.

## Prompts

The prompts are in `prompts/`, as `text/template` files with `[[ ]]`
delimiters so `{{char}}` and `{{user}}` reach the model as written. The
generate, revise, and evaluate prompts come from character-card-generator
at commit `0b2f57e`, changed to ask for JSON keyed by CCv3 data keys.

`house_style` is added to every action's system prompt, and
`additions.<action ID>` to that action's.

## Library access

`generate`, `revise`, `alternate-greetings`, and `lore-entries` may read the library through `library.call` when
the owner turns on Library access for the instance. If the model rejects
the request with tools, the run retries once without them, and only the
cards and lorebooks the owner attached reach the model.

## Skills

A skill is an [Agent Skill](https://agentskills.io/specification): a
`SKILL.md` with `name` and `description` frontmatter and Markdown
instructions, plus optional reference files. The plugin keeps skills in its
own database schema, so it needs a core with the `records` capability. The
owner adds, edits, and imports them on the instance's settings page.

Each skill has an Enabled switch. New and imported skills start enabled,
so an import is usable at once; reimporting a disabled skill leaves it
disabled. Only enabled skills reach the model:

- Every action offers the tools `load_skill` and `load_skill_file`.
  `load_skill`'s description lists each enabled skill's name and
  description; it returns the instructions and the reference file paths.
- A `@skills/<name>` mention puts that skill's instructions and its
  reference file paths in the system prompt. A mention of a skill that
  isn't enabled fails the run, and so do mentions whose instructions add up
  to more than 256 KiB.
- If the model rejects tools, the retry without them keeps only the
  mentioned skills.

Once any skill is enabled, every action sends tools, including ones that
otherwise send none, such as `tighten`. A model without tool support then
rejects each run's first request, and every run makes that failed request
before retrying without tools. Disable all skills for such a model.

### Import

Import takes a `SKILL.md` or a zip of a skill folder, uploaded or by an
`http` or `https` URL. A zip is recognized by its content, not its name.
Importing a skill replaces the one with the same name.

| Limit | Value |
|---|---|
| Upload or download | 8 MiB |
| Download timeout | 30 seconds |
| Zip entries | 200 |
| `SKILL.md`, instructions, or one reference file | 1 MiB |
| Instructions and reference files together | 4 MiB |
| Reference files | 64 |

- A zip holds exactly one `SKILL.md`. Its folder is the skill; files outside
  that folder are ignored.
- A zip entry whose path is absolute, contains `\`, or leaves the folder
  with `..` fails the import.
- Files that aren't UTF-8 text, such as images, are skipped and don't
  count toward the size limits.
- URLs may point at private and loopback addresses, such as a Git server on
  your LAN. The plugin runs on your network and importing needs write
  access in Innkeeper, so refusing them would block real uses. A writer can
  make the plugin request any address it can reach, and sees the status of
  a failed request; give write access only to people you trust with that.
