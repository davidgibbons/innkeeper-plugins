# forge-ai

Writes, revises, tightens, and evaluates character cards with a language
model, through [any-llm-go](https://github.com/mozilla-ai/any-llm-go). It
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

Whole-card actions send the model only the card's prose keys, `name`, and
`tags`; `extensions` and the rest stay as they were.

## Prompts

The prompts are in `prompts/`, as `text/template` files with `[[ ]]`
delimiters so `{{char}}` and `{{user}}` reach the model as written. The
generate, revise, and evaluate prompts come from character-card-generator
at commit `0b2f57e`, changed to ask for JSON keyed by CCv3 data keys.

`house_style` is added to every action's system prompt, and
`additions.<action ID>` to that action's.

## Library access

`generate` and `revise` may read the library through `library.call` when
the owner turns on Library access for the instance. If the model rejects
the request with tools, the run retries once without them, and only the
cards and lorebooks the owner attached reach the model.
