# ai

Connects a language model for other plugins to use, through
[any-llm-go](https://github.com/mozilla-ai/any-llm-go). It offers the `ai`
and `settings` capabilities, so it needs a core that has them.

Each instance is one connection. `provider` and `model` are required.
`api_key` is optional for `openai-compatible`, `ollama`, `llamacpp`, and
`llamafile`; the other providers need one. `openai-compatible` also needs
`base_url`, such as `http://localhost:1234/v1` for LM Studio or
`https://openrouter.ai/api/v1`.

## What a connection provides

Tick what the connection is for under **Provides**; chat is the default.

| Kind | Service | Providers |
|---|---|---|
| `chat` | `ai.chat` | All |
| `embed` | `ai.embed` | openai, openai-compatible, gemini, mistral, ollama, llamacpp, llamafile |

A consumer, such as forge-ai, picks an instance that offers the service it
needs. `ai.chat` returns the model's text as it is, including any
`<think>` block, and returns tool calls for the consumer to answer.

## Settings page

- **Load choices** on Model lists the provider's models. Anthropic doesn't
  list them, so type the name.
- **Test connection** makes one small real call per ticked kind, a
  one-token chat or one embedded word, and reports each with its latency.
  It gives up after 30 seconds for all the calls together.
