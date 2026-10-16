# forge-ai on AI Services Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** forge-ai uses an `ai` instance for every model call, offers ten distinct actions, runs an edit on the whole card as a plan followed by one edit per field, and has a Prompts tab that shows and overrides each action's instructions.

**Architecture:** This is section 4 of the core spec `docs/superpowers/specs/2026-10-15-ai-services-design.md`, in the innkeeper repo. It builds on these core plans, all in innkeeper `docs/superpowers/plans/`:

| Plan | What forge-ai uses from it |
|---|---|
| `2026-10-15-ai-capability.md` | `protocol.AIChatParams`, `ServiceCall{Instance, Run}`, `AIDelta`, `CodeAIRejected`, `Manifest.Uses` |
| `2026-10-15-settings-form.md` | The `settings` capability, `settings.defaults`, `format = "instance"`, `x-offers`, tabs |
| `2026-10-15-whole-card-actions.md` | `protocol.Action.WholeCard` |

The changes:
- **Model calls.** forge-ai stops calling providers. Each request is an `ai.chat` to the core, with the `connection` instance id from forge-ai's config. Streamed text comes back as `ai.delta` notifications under a per-request token. Tool calls arrive whole, and forge-ai runs the tool loop as it does today.
- **Actions.** The ten are Forge, Revise, Tighten, Expand, Convert to prose, Change point of view, Write examples, Write alternate greetings, Write lore entries and Evaluate. The five Edit actions are field actions with `WholeCard`.
- **Whole-card runs.** One plan call returns a note or `skip` per field. The action then runs on each planned field in turn, against the card as the earlier fields left it.
- **Prompts.** Each prompt's rules move into an `[[define "<key>.instructions"]]` block that takes no template data. The frame around it keeps the JSON contract and the card data. An override in `prompts.<key>` replaces the block. `settings.defaults` returns each block.

**Tech Stack:** Go, `text/template`, JSON-RPC over stdio.

**Prerequisites:**
1. Core plans 1, 2 and 4a are merged and pushed.
2. **`protocol.AIChatResult` carries the answering model** as `Model string` with JSON `model,omitempty`, and the `ai` plugin (plan 3) fills it in. Plan 1 as written lacks this field, so add it there before starting. forge-ai's score metadata records the model, and forge-ai no longer knows which model it is.

**Deviations from the spec:**
- **Tighten, Expand and Convert to prose take no instruction.** `protocol.Action.Instruction` means the instruction is required, so "optional" would need a protocol change. Their whole-card plan uses the action's own instructions as its direction.
- **Forge has three Prompts entries:** first person, third person and scenario. Each is a different prompt today.
- **Revise runs on `name`, the prose fields, and `tags`.** The other Edit actions run on the prose fields, and Change point of view skips `creator_notes`.
- **The plan call offers no tools.** Each field's call offers the skills and library tools, as today.
- **`stream.go` stays.** It parses the model's output. Provider streaming is `stream` and `mergeToolCalls` in `chat.go`; those move to the `ai` plugin.

**Tests:** `go test ./forge-ai/`. Tests that need a database skip without `INNKEEPER_PLUGIN_DATABASE_URL`. To run them, set it, for example `INNKEEPER_PLUGIN_DATABASE_URL=postgres://postgres:postgres@localhost:55432/postgres`.

---

## File map

| File | Change |
|---|---|
| `go.mod`, `go.sum` | Core pin bumped; any-llm-go stays only if plan 3's `ai` plugin is in the module |
| `forge-ai/settings.go`, `settings_test.go` | `connection`, `prompts`; provider code deleted |
| `forge-ai/chat.go` | `ai.chat` client and tool loop on protocol types |
| `forge-ai/main.go` | No provider; `ai.delta` routing; `settings.defaults`; whole-card dispatch |
| `forge-ai/skilltools.go` | `protocol.ChatTool` |
| `forge-ai/enrich.go`, `enrich_test.go` | Model in meta from the reply; prompt hash from `prompts` |
| `forge-ai/actions.go`, `actions_test.go` | Ten actions; `note`, `model` on `run` |
| `forge-ai/prompts.go`, `prompts_test.go` | Instructions, overrides, `promptKeys`, defaults |
| `forge-ai/prompts/*.tmpl` | Instructions blocks and frames; `revise.tmpl` and `tighten-card.tmpl` deleted; `plan.tmpl` added |
| `forge-ai/wholecard.go`, `wholecard_test.go` | New: plan, then sections |
| `forge-ai/run_test.go`, `records_test.go` | Fake host answers `ai.chat` |
| `forge-ai/plugin.toml`, `README.md`, `index.toml` | Manifest, docs, 0.5.0 |

---

### Task 1: Pin the core

**Files:**
- Modify: `go.mod`, `go.sum`

- [ ] **Step 1: Bump the pin to core main**

Run: `go get github.com/inkandale/innkeeper@main && go mod tidy`
Expected: `go.mod`'s `github.com/inkandale/innkeeper` line moves to a new pseudo-version.

- [ ] **Step 2: Check the contracts are there**

Run: `go doc github.com/inkandale/innkeeper/protocol AIChatResult && go doc github.com/inkandale/innkeeper/protocol Action | grep WholeCard && go doc github.com/inkandale/innkeeper/protocol SettingsDefaultsResult`
Expected: `AIChatResult` lists `Model`, `Action` lists `WholeCard`, and `SettingsDefaultsResult` exists. If any is missing, stop: a prerequisite isn't merged.

- [ ] **Step 3: Build and test**

Run: `go build ./... && go test ./forge-ai/`
Expected: PASS

- [ ] **Step 4: Commit**

```bash
git add go.mod go.sum
git commit -m "chore: pin the core with ai services"
```

---

### Task 2: Call the model through `ai.chat`

**Files:**
- Modify: `forge-ai/settings.go`, `forge-ai/settings_test.go`, `forge-ai/chat.go`, `forge-ai/main.go`, `forge-ai/skilltools.go`, `forge-ai/enrich.go`, `forge-ai/actions.go`, `forge-ai/plugin.toml`
- Test: `forge-ai/run_test.go`, `forge-ai/records_test.go`, `forge-ai/enrich_test.go`

- [ ] **Step 1: Replace the fake model with a fake `ai` connection**

In `forge-ai/run_test.go`, replace everything from `// fakeLLM speaks the OpenAI-compatible chat API.` through the end of `ServeHTTP` with:

```go
// fakeLLM stands in for an ai instance: the fake host answers each ai.chat
// for its connection with the next reply. requests holds each call's params
// as a JSON object.
type fakeLLM struct {
	mu        sync.Mutex
	replies   []fakeReply
	requests  []map[string]any
	onRequest func()
}

type fakeReply struct {
	status int      // nonzero: refuse with -32010, as a model without tool support does
	deltas []string // the reply's text, streamed piece by piece
	calls  []protocol.ToolCall
}

// streamed splits text into small pieces, as a model streams it.
func streamed(text string) fakeReply {
	var r fakeReply
	for len(text) > 0 {
		n := min(7, len(text))
		r.deltas = append(r.deltas, text[:n])
		text = text[n:]
	}
	return r
}

func toolCall(id, name, args string) fakeReply {
	return fakeReply{calls: []protocol.ToolCall{{ID: id, Name: name, Arguments: args}}}
}

// llms finds a test's fakeLLM by the connection ID in its config.
var (
	llms   sync.Map
	llmIDs atomic.Int64
)

// answer replies to one ai.chat, streaming its text to conn first.
func (f *fakeLLM) answer(conn *protocol.Conn, raw json.RawMessage) (any, error) {
	var body map[string]any
	var p protocol.AIChatParams
	if err := json.Unmarshal(raw, &body); err != nil || json.Unmarshal(raw, &p) != nil {
		return nil, protocol.NewError(protocol.CodeInvalidParams, "bad ai.chat", false)
	}
	f.mu.Lock()
	f.requests = append(f.requests, body)
	hook := f.onRequest
	if len(f.replies) == 0 {
		f.mu.Unlock()
		return nil, protocol.NewError(-32000, "no reply scripted", false)
	}
	reply := f.replies[0]
	f.replies = f.replies[1:]
	f.mu.Unlock()
	if hook != nil {
		hook()
	}
	if reply.status != 0 {
		return nil, protocol.NewError(protocol.CodeAIRejected, "this model does not support tools", false)
	}
	var text strings.Builder
	for _, d := range reply.deltas {
		_ = conn.Notify(protocol.NotifyAIDelta, protocol.AIDelta{Run: p.Run, Text: d})
		text.WriteString(d)
	}
	return protocol.AIChatResult{Text: text.String(), ToolCalls: reply.calls, Model: "m"}, nil
}
```

In `host`, add a field:

```go
	srv      *server
```

In `startWith`, after `h := &host{}`, add `h.srv = srv`. At the top of the host's handler func, before `h.mu.Lock()`, add:

```go
		if method == protocol.MethodAIChat {
			var call protocol.ServiceCall
			_ = json.Unmarshal(params, &call)
			f, ok := llms.Load(call.Instance)
			if !ok {
				return nil, protocol.NewError(protocol.CodeForbidden, "no such connection", false)
			}
			return f.(*fakeLLM).answer(h.conn, params)
		}
```

Replace `withLLM` with:

```go
func withLLM(t *testing.T, replies ...fakeReply) (*fakeLLM, map[string]any) {
	f := &fakeLLM{replies: replies}
	id := llmIDs.Add(1)
	llms.Store(id, f)
	t.Cleanup(func() { llms.Delete(id) })
	return f, map[string]any{"connection": id, "house_style": "No em dashes."}
}
```

Delete `content`, `argsDelta`, the old `toolCall`, and `TestMergeToolCallsAcrossProviders`. Then, in `run_test.go`, `records_test.go` and `enrich_test.go`, turn each `fakeReply{deltas: []string{toolCall(ARGS)}}` into `toolCall(ARGS)`:

```bash
perl -pi -e 's/fakeReply\{deltas: \[\]string\{(toolCall\(.*?\))\}\}/$1/g' forge-ai/run_test.go forge-ai/records_test.go forge-ai/enrich_test.go
```

The match is non-greedy because the arguments contain braces, such as `{"id": 7}`. The reply in `TestProgressRestartsEachRound` also has text, so the regex doesn't match it; Step 2 changes it by hand.

Fix the imports: add `sync/atomic` to `run_test.go`, and remove `net/http`, `net/http/httptest` and `anyllm` where they become unused.

- [ ] **Step 2: Point the assertions at `ai.chat` params**

The request maps now use the protocol's keys. Make these edits:

In `TestRunStreamsProgressPerField`, replace:

```go
	if req["stream"] != true || req["model"] != "m" {
		t.Fatalf("request %v", req)
	}
```

with:

```go
	if run, _ := req["run"].(string); !strings.HasPrefix(run, "1.1/") {
		t.Fatalf("request %v: want a streaming token under the run's", req)
	}
```

In `TestLibraryToolCallAnsweredByHost`, replace the first reply with `toolCall("call_1", "get_card", `{"id": 7}`)`, and the `asked` lines with:

```go
	asked := msgs[len(msgs)-2].(map[string]any)["tool_calls"].([]any)[0].(map[string]any)
```

In `TestReferenceImageSent`, replace the `parts` and `url` lines with:

```go
	parts := llm.requests[0]["messages"].([]any)[1].(map[string]any)["parts"].([]any)
	url := parts[1].(map[string]any)["image_url"].(string)
```

In `TestProgressRestartsEachRound`, replace the first reply with:

```go
		fakeReply{deltas: []string{`{"description": "draft"`}, calls: []protocol.ToolCall{{ID: "call_1", Name: "get_card", Arguments: `{"id": 7}`}}},
```

In `TestLoadSkillToolCall` (`records_test.go`), replace the `desc` line with:

```go
	desc := tools[0].(map[string]any)["description"].(string)
```

In `TestRunWithoutSettingsFails` and `TestEnrichWithoutSettingsFailsTheCall`, change `"provider"` to `"connection"`.

Replace `TestCancelStopsTheToolLoop` with:

```go
func TestCancelStopsTheToolLoop(t *testing.T) {
	llm, cfg := withLLM(t, toolCall("call_1", "get_card", `{"id": 7}`), streamed(`{"description": "x"}`))
	h := startPlugin(t, cfg, nil)
	ctx, cancel := context.WithCancel(context.Background())
	lib := func(context.Context, string, string) (string, error) {
		cancel() // The owner pressed Stop while the tool ran.
		return "{}", nil
	}
	params := protocol.AIChatParams{Messages: []protocol.ChatMessage{{Role: "user", Content: "hi"}}}
	noProgress := func() func(string) { return func(string) {} }
	if _, err := h.srv.complete(ctx, h.srv.cur.Load(), "1.1", params, toolbox{tools: libraryTools, call: lib}, noProgress); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if len(llm.requests) != 1 {
		t.Fatalf("%d requests after cancel, want 1", len(llm.requests))
	}
}
```

Replace `TestEnrichCancelStopsFurtherCalls` with:

```go
func TestEnrichCancelStopsFurtherCalls(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	llm, cfg := withLLM(t, streamed(evaluated), streamed(evaluated))
	llm.onRequest = cancel // The owner pressed Stop during the first call.
	h := startPlugin(t, cfg, nil)
	params, _ := json.Marshal(enrichParams("Brakka", "Mira"))
	if _, err := h.srv.enrich(ctx, h.srv.cur.Load(), params); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	llm.mu.Lock()
	defer llm.mu.Unlock()
	if len(llm.requests) > 1 {
		t.Fatalf("%d requests after cancel, want at most 1", len(llm.requests))
	}
}
```

Replace `TestProviders` in `forge-ai/settings_test.go` with:

```go
func TestParseSettings(t *testing.T) {
	s, err := parseSettings(map[string]any{"connection": 9.0, "temperature": 0.5})
	if err != nil || s.Connection != 9 || *s.Temperature != 0.5 {
		t.Fatalf("settings %+v: %v", s, err)
	}
	for _, cfg := range []map[string]any{{}, {"connection": "nine"}, {"connection": 9.0, "temperature": "hot"}} {
		if _, err := parseSettings(cfg); err == nil {
			t.Errorf("%v: no error", cfg)
		}
	}
}
```

Drop `strings` from its imports.

- [ ] **Step 3: Run the tests to see them fail**

Run: `go test ./forge-ai/`
Expected: FAIL to compile, with `h.srv.complete undefined`, `s.Connection undefined`, and `protocol.MethodAIChat` unused in the plugin.

- [ ] **Step 4: Rewrite `forge-ai/settings.go`**

```go
package main

import (
	"encoding/json"
	"errors"
	"fmt"
)

// settings are the instance's config.
type settings struct {
	// Connection is the ai instance every model call goes to.
	Connection  int64             `json:"connection"`
	Temperature *float64          `json:"temperature"`
	MaxTokens   *int              `json:"max_tokens"`
	HouseStyle  string            `json:"house_style"`
	Additions   map[string]string `json:"additions"`
}

func parseSettings(cfg map[string]any) (settings, error) {
	var s settings
	b, err := json.Marshal(cfg)
	if err == nil {
		err = json.Unmarshal(b, &s)
	}
	if err != nil {
		return s, fmt.Errorf("settings: %w", err)
	}
	if s.Connection == 0 {
		return s, errors.New("settings: pick an AI connection")
	}
	return s, nil
}
```

- [ ] **Step 5: Rewrite the model calls in `forge-ai/chat.go`**

Change the imports to:

```go
import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/inkandale/innkeeper-plugins/internal/pluginio"
	"github.com/inkandale/innkeeper/protocol"
)
```

Change `libraryTools` to `var libraryTools = []protocol.ChatTool{`, leaving its entries as they are, and replace `tool`:

```go
func tool(name, description string, properties map[string]any, required ...string) protocol.ChatTool {
	params, _ := json.Marshal(map[string]any{"type": "object", "properties": properties, "required": required})
	return protocol.ChatTool{Name: name, Description: description, Parameters: params}
}
```

In `toolbox`, change `tools []anyllm.Tool` to `tools []protocol.ChatTool`.

Replace `complete`, `stream` and `mergeToolCalls` with:

```go
// complete runs the conversation to the model's final answer, answering its
// tool calls through box. newProgress gives each request its own callback,
// which gets that request's text so far.
func (srv *server) complete(ctx context.Context, in *instance, run string, params protocol.AIChatParams, box toolbox, newProgress func() func(string)) (protocol.AIChatResult, error) {
	params.Instance = in.settings.Connection
	for round := 1; ; round++ {
		if err := ctx.Err(); err != nil {
			return protocol.AIChatResult{}, err
		}
		// The last round offers no tools, so the model has to answer.
		params.Tools = box.tools
		if round == maxToolRounds {
			params.Tools = nil
		}
		res, err := srv.chat(ctx, run, params, newProgress())
		if err != nil && round == 1 && params.Tools != nil && rejected(err) {
			// Most likely a model without tool support; the context and forced skills in the prompt still apply.
			fmt.Fprintf(os.Stderr, "retrying without tools: %v\n", err)
			box.tools, params.Tools = nil, nil
			res, err = srv.chat(ctx, run, params, newProgress())
		}
		if err != nil || len(res.ToolCalls) == 0 || params.Tools == nil {
			return res, err
		}
		params.Messages = append(params.Messages, protocol.ChatMessage{Role: "assistant", Content: res.Text, ToolCalls: res.ToolCalls})
		for _, c := range res.ToolCalls {
			result, err := box.call(ctx, c.Name, c.Arguments)
			if err == nil {
				err = ctx.Err()
			}
			if err != nil {
				return protocol.AIChatResult{}, fmt.Errorf("tool %s: %w", c.Name, err)
			}
			params.Messages = append(params.Messages, protocol.ChatMessage{Role: "tool", Content: result, ToolCallID: c.ID})
		}
	}
}

// chat sends one ai.chat through the core under its own streaming token,
// passing onText the reply's text so far as it streams.
func (srv *server) chat(ctx context.Context, run string, params protocol.AIChatParams, onText func(string)) (protocol.AIChatResult, error) {
	params.Run = fmt.Sprintf("%s/%d", run, srv.chats.Add(1))
	var text strings.Builder
	srv.deltas.Store(params.Run, func(piece string) {
		text.WriteString(piece)
		onText(text.String())
	})
	defer srv.deltas.Delete(params.Run)
	var res protocol.AIChatResult
	err := srv.conn.Call(ctx, protocol.MethodAIChat, params, &res)
	return res, err
}

// rejected reports a provider refusing the request as invalid.
func rejected(err error) bool {
	var rpcErr *protocol.Error
	return errors.As(err, &rpcErr) && rpcErr.Code == protocol.CodeAIRejected
}
```

Replace `imagePart`:

```go
// imagePart reads an image from the blob store as a data URL.
func imagePart(path string) (protocol.ContentPart, error) {
	b, err := pluginio.ReadFile(path)
	if err != nil {
		return protocol.ContentPart{}, fmt.Errorf("read image: %w", err)
	}
	url := "data:" + http.DetectContentType(b) + ";base64," + base64.StdEncoding.EncodeToString(b)
	return protocol.ContentPart{Type: "image_url", ImageURL: url}, nil
}
```

- [ ] **Step 6: Update `forge-ai/skilltools.go`**

Drop the `anyllm` import, and change `skillTools`' signature and return to `[]protocol.ChatTool`, importing `github.com/inkandale/innkeeper/protocol`:

```go
func skillTools(enabled []skill) []protocol.ChatTool {
```

```go
	return []protocol.ChatTool{
```

- [ ] **Step 7: Update `forge-ai/main.go`**

Drop the `anyllm` import; add `"sync"`. Replace `instance` and `server`:

```go
// instance is the setup initialize configured. err is a settings problem,
// reported by the first run. store is nil without a database_url.
type instance struct {
	settings settings
	err      error
	store    *store
}

type server struct {
	conn *protocol.Conn
	// Set by initialize; requests run on other goroutines.
	cur atomic.Pointer[instance]
	// deltas maps an ai.chat's streaming token to its text callback.
	deltas sync.Map
	chats  atomic.Int64
}
```

In `handle`'s initialize case, replace the two settings lines with:

```go
		in.settings, in.err = parseSettings(p.Config)
```

In the first `switch`, before `case protocol.MethodShutdown:`, add:

```go
	case protocol.NotifyAIDelta:
		var d protocol.AIDelta
		if json.Unmarshal(params, &d) == nil {
			if onText, ok := srv.deltas.Load(d.Run); ok {
				onText.(func(string))(d.Text)
			}
		}
		return nil, nil
```

In `perform`, replace everything from `var userContent any = user` through `output, err := complete(...)`'s error check with:

```go
	message := protocol.ChatMessage{Role: "user", Content: user}
	if r.Images.Reference != "" || r.Images.Avatar != "" {
		message = protocol.ChatMessage{Role: "user", Parts: []protocol.ContentPart{{Type: "text", Text: user}}}
		for _, path := range []string{r.Images.Reference, r.Images.Avatar} {
			if path == "" {
				continue
			}
			part, err := imagePart(path)
			if err != nil {
				return nil, err
			}
			message.Parts = append(message.Parts, part)
		}
	}
	temperature, maxTokens := r.limits(in.settings)
	req := protocol.AIChatParams{Temperature: &temperature, MaxTokens: &maxTokens,
		Messages: []protocol.ChatMessage{{Role: "system", Content: system}, message}}
	reply, err := srv.complete(ctx, in, r.Run, req, box, newProgress)
	if err != nil {
		return nil, err
	}
	r.model = reply.Model
	res, err := r.result(reply.Text)
```

Keep the lines after it that check `err` and return `&res`.

- [ ] **Step 8: Record the model on the run**

In `forge-ai/actions.go`, add to `run`:

```go
	// model is the model that answered, from the ai instance's reply.
	model string
```

- [ ] **Step 9: Take the model for scores from the reply**

In `forge-ai/enrich.go`, delete the `meta, err := json.Marshal(...)` block before the loop. In the loop, replace from `values, err := srv.scores(...)` through `out := protocol.EnrichResult{...}` with:

```go
		values, model, err := srv.scores(ctx, in, p.Run, rev)
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		meta, _ := json.Marshal(map[string]string{"model": model, "prompt": prompt})
		out := protocol.EnrichResult{RevisionID: rev.RevisionID, Values: values, Meta: meta}
```

Change `scores` to return `(map[string]json.RawMessage, string, error)`. Its error returns become `return nil, "", err`, the "no scores" one becomes `return nil, "", errors.New(...)`, and the last becomes `return values, r.model, nil`. Update its doc comment:

```go
// scores runs evaluate on one revision and returns its scores by enrichment
// key, each clamped to 0 to 100, and the model that answered.
```

- [ ] **Step 10: Update the manifest**

In `forge-ai/plugin.toml`, replace `secrets = ["api_key"]` with `uses = ["ai.chat"]`. Change `required = ["provider", "model"]` to `required = ["connection"]`. Replace the `provider`, `base_url` and `model` property tables with:

```toml
[config.properties.connection]
type = "integer"
title = "AI connection"
format = "instance"
x-offers = "ai.chat"
description = "The ai instance whose chat model every action uses."
```

- [ ] **Step 11: Run the tests**

Run: `go vet ./forge-ai/ && go test ./forge-ai/`
Expected: PASS

- [ ] **Step 12: Commit**

```bash
git add forge-ai/ go.mod go.sum
git commit -m "feat(forge-ai): call the model through an ai instance"
```

---

### Task 3: The ten actions

**Files:**
- Modify: `forge-ai/actions.go`, `forge-ai/prompts.go`, `forge-ai/prompts/field.tmpl`, `forge-ai/prompts/change-pov.tmpl`, `forge-ai/prompts/shared.tmpl`, `forge-ai/plugin.toml`
- Delete: `forge-ai/prompts/revise.tmpl`, `forge-ai/prompts/tighten-card.tmpl`
- Test: `forge-ai/actions_test.go`, `forge-ai/prompts_test.go`, `forge-ai/run_test.go`

- [ ] **Step 1: Write the failing tests**

In `forge-ai/prompts_test.go`, in `sampleRun`, replace:

```go
	if len(a.Fields) > 0 {
		p.Field = a.Fields[0]
	}
```

with:

```go
	if slices.Contains(a.Fields, "description") {
		p.Field = "description"
	} else if len(a.Fields) > 0 {
		p.Field = a.Fields[0]
	}
```

Add to `forge-ai/actions_test.go`:

```go
func TestTheTenActions(t *testing.T) {
	var ids []string
	for _, a := range actions {
		ids = append(ids, a.ID)
	}
	want := []string{"generate", "revise", "tighten", "expand", "to-prose", "change-pov", "examples", "alternate-greetings", "lore-entries", "evaluate"}
	if !slices.Equal(ids, want) {
		t.Fatalf("actions %v, want %v", ids, want)
	}
	for _, id := range []string{"revise", "tighten", "expand", "to-prose", "change-pov"} {
		if a, _ := findAction(id); !a.WholeCard || len(a.Fields) == 0 {
			t.Errorf("%s: want a field action that also runs on the whole card", id)
		}
	}
}

func TestReviseOnTagsTakesCommaText(t *testing.T) {
	p := sampleRun(t, "revise")
	p.Field = "tags"
	system, user, _ := render(t, p, settings{})
	if !strings.Contains(user, "orc") || !strings.Contains(system, `"tags"`) {
		t.Fatalf("tags field prompt:\n%s\n%s", system, user)
	}
}
```

Add `slices` to `actions_test.go`'s imports.

Replace these tests in `actions_test.go`:

```go
func TestNullMeansUnchanged(t *testing.T) {
	m := proposalOf(t, result(t, sampleRun(t, "revise"), `{"description": null}`))
	if m["description"] != "A very big, very tall orc who guards {{user}}." {
		t.Fatalf("proposal %v", m)
	}
}

func TestTagsAsCommaSeparatedText(t *testing.T) {
	p := sampleRun(t, "revise")
	p.Field = "tags"
	m := proposalOf(t, result(t, p, `{"tags": "orc, guard ,, brother"}`))
	tags, _ := m["tags"].([]any)
	if len(tags) != 3 || tags[1] != "guard" || tags[2] != "brother" {
		t.Fatalf("tags = %v", m["tags"])
	}
}
```

Delete `TestWholeCardProposalKeepsOtherKeys`; Task 5's whole-card tests cover a run that leaves other keys alone.

In `TestRunInputsChecked`, change `p := sampleRun(t, "rewrite")` to `p := sampleRun(t, "revise")`, the "field not offered" change to `p.Field = "extensions"`, and both change-pov cases' `p.Field` from `""` to `"description"`.

In `run_test.go`'s `TestProgressRestartsEachRound`, change the wanted message to `"Writing Description (1 of 1)"`.

- [ ] **Step 2: Run them to see them fail**

Run: `go test ./forge-ai/`
Expected: FAIL. `TestTheTenActions` lists twelve actions, and `TestReviseOnTagsTakesCommaText` fails because revise doesn't run on a field.

- [ ] **Step 3: Declare the actions**

In `forge-ai/actions.go`, after `cardKeys`, add:

```go
// povFields are the fields change-pov rewrites; creator_notes speaks to the player, not as {{char}}.
var povFields = []string{"description", "personality", "scenario", "first_mes", "mes_example", "system_prompt", "post_history_instructions"}
```

Replace `actions` with:

```go
var actions = []action{
	{protocol.Action{ID: "generate", Label: "Forge", Description: "Write a new card from a concept.",
		Applies: protocol.ApplyNew, Instruction: true, Library: true, Images: true,
		Inputs: json.RawMessage(fmt.Sprintf(povSchema, `"first", "third", "scenario"`, ""))}, 0.8, 8192, nil},
	{protocol.Action{ID: "revise", Label: "Revise", Description: "Change it as you describe.",
		Applies: protocol.ApplyCard, Fields: cardKeys, WholeCard: true, Instruction: true, Library: true}, 0.7, 4096, nil},
	{protocol.Action{ID: "tighten", Label: "Tighten", Description: "Cut filler and repetition; keep every fact.",
		Applies: protocol.ApplyCard, Fields: textFields, WholeCard: true}, 0.5, 4096, nil},
	{protocol.Action{ID: "expand", Label: "Expand", Description: "Add concrete detail without changing what's there.",
		Applies: protocol.ApplyCard, Fields: textFields, WholeCard: true}, 0.8, 4096, nil},
	{protocol.Action{ID: "to-prose", Label: "Convert to prose", Description: "Turn lists and bracket formats into paragraphs.",
		Applies: protocol.ApplyCard, Fields: textFields, WholeCard: true}, 0.5, 4096, nil},
	{protocol.Action{ID: "change-pov", Label: "Change point of view", Description: "Switch between first and third person.",
		Applies: protocol.ApplyCard, Fields: povFields, WholeCard: true,
		Inputs: json.RawMessage(fmt.Sprintf(povSchema, `"first", "third"`, `, "required": ["point_of_view"]`))}, 0.6, 4096, nil},
	{protocol.Action{ID: "examples", Label: "Write examples", Description: "Write new example dialogue.",
		Applies: protocol.ApplyCard, Fields: []string{"mes_example"},
		Inputs: json.RawMessage(`{"type": "object", "properties": {"count": {"type": "integer", "title": "Examples", "minimum": 1, "maximum": 10, "default": 3}}}`)}, 0.8, 4096, nil},
	{protocol.Action{ID: "alternate-greetings", Label: "Write alternate greetings", Description: "Add new opening messages to the card.",
		Applies: protocol.ApplyCard, Library: true,
		Inputs: json.RawMessage(`{"type": "object", "properties": {"count": {"type": "integer", "title": "Greetings", "minimum": 1, "maximum": 10, "default": 3}}}`)}, 0.9, 8192, greetingsAppender},
	{protocol.Action{ID: "lore-entries", Label: "Write lore entries", Description: "Add entries about what you describe, from the cards you mention.",
		Applies: protocol.ApplyLorebook, Instruction: true, Library: true,
		Inputs: json.RawMessage(`{"type": "object", "properties": {"count": {"type": "integer", "title": "Entries", "minimum": 1, "maximum": 20, "default": 5}}}`)}, 0.7, 8192, entriesAppender},
	{protocol.Action{ID: "evaluate", Label: "Evaluate", Description: "Score the card on six dimensions and list its problems.",
		Applies: protocol.ApplyCard, ReportOnly: true}, 0.3, 4096, nil},
}
```

- [ ] **Step 4: Send a list field as comma text**

In `forge-ai/prompts.go`, replace:

```go
		if r.Field != "" {
			d.FieldText, _ = card[r.Field].(string)
		}
```

with:

```go
		switch v := card[r.Field].(type) {
		case string:
			d.FieldText = v
		case []string:
			d.FieldText = strings.Join(v, ", ")
		}
```

- [ ] **Step 5: Rewrite the field prompts**

Delete `forge-ai/prompts/revise.tmpl` and `forge-ai/prompts/tighten-card.tmpl`. `field-ownership` stays in `shared.tmpl` for generate.

In `forge-ai/prompts/field.tmpl`, replace `field-rules` and `field.user` with these. A tags value is comma text, and `change-pov` mustn't be told to keep the point of view:

```
[[- define "field-rules" -]]
You edit one field of a roleplay character card: "[[.Field]]". Return one JSON object with the single key "[[.Field]]", whose value is the new text[[if eq .Field "tags"]]: the tags, separated by commas[[end]].

Keep every {{char}} and {{user}} macro and <START> blocks[[if not .PointOfView]], and the field's point of view and voice[[end]]. The rest of the card is context; don't move its content into this field.
[[- end]]

[[- define "field.user" -]]
[[- if .Instruction]]Instruction: [[.Instruction]]

[[end -]]
The field to edit, "[[.Field]]":
[[.FieldText]]

The whole card, for context:
[[.Card]]
[[- template "context" .]]
[[- end]]
```

Rename the `rewrite.system` and `rewrite.user` blocks to `revise.system` and `revise.user`, and make `revise.system`'s rule line:

```
Change it as the owner's instruction asks. Change only what the instruction asks for, and keep the rest of the field's content, voice, and formatting.
```

Replace `forge-ai/prompts/change-pov.tmpl` with:

```
[[- define "change-pov.system" -]]
[[template "field-rules" .]]

[[if eq .PointOfView "first" -]]
Rewrite it in first person, in {{char}}'s own voice: I, me, my. In system_prompt and post_history_instructions, enforce first person: {{char}} speaks as I and never breaks character.
[[- else -]]
Rewrite it in third person: he, she, they, or {{char}}; never I, me, or my outside quoted dialogue. In first_mes and mes_example, narrate {{char}} as an omniscient narrator; dialogue stays in {{char}}'s voice. In system_prompt and post_history_instructions, enforce third-person narration.
[[- end]]

Change only the point of view. Keep every fact, name, rule, detail, and the tone, and keep the formatting.

Return only the JSON object: no code fence, no commentary.
[[- end]]

[[- define "change-pov.user" -]][[template "field.user" .]][[end]]
```

- [ ] **Step 6: Update the additions in the manifest**

In `forge-ai/plugin.toml`, under `[config.properties.additions.properties]`, delete `tighten-card` and `rewrite`, and retitle `revise` as `"Revise"`. Change `max_tokens`' description to:

```toml
description = "Leave empty to use each action's own: 8192 for Forge, alternate greetings, and lore entries, 4096 for the rest."
```

- [ ] **Step 7: Run the tests**

Run: `go test ./forge-ai/`
Expected: PASS. `TestEveryActionRenders` still checks that each prompt names each key it writes.

- [ ] **Step 8: Commit**

```bash
git add -A forge-ai/
git commit -m "feat(forge-ai): offer ten distinct actions, editing a field or the whole card"
```

---

### Task 4: Instructions, overrides, and the Prompts tab

**Files:**
- Modify: `forge-ai/prompts.go`, `forge-ai/prompts/*.tmpl`, `forge-ai/settings.go`, `forge-ai/main.go`, `forge-ai/enrich.go`, `forge-ai/plugin.toml`
- Test: `forge-ai/prompts_test.go`, `forge-ai/enrich_test.go`, `forge-ai/run_test.go`

An **instructions block** is the part of a prompt that the owner can replace. It is a `[[define "<key>.instructions"]]` block with no template actions in it, so the Prompts tab can show the text as it is. The frame around it keeps the JSON contract and the card data.

- [ ] **Step 1: Write the failing tests**

In `forge-ai/prompts_test.go`, replace `TestHouseStyleAndAdditionInSystemPrompt` and `TestAdditionsMatchActions` with:

```go
func TestHouseStyleAndOverrideInSystemPrompt(t *testing.T) {
	s := settings{Prompts: map[string]string{"house_style": "Use British spelling.", "evaluate": "Be harsh about clichés."}}
	for _, a := range actions {
		system, user, keys := render(t, sampleRun(t, a.ID), s)
		if !strings.Contains(system, "Use British spelling.") {
			t.Errorf("%s: no house style", a.ID)
		}
		if got := strings.Contains(system, "Be harsh about clichés."); got != (a.ID == "evaluate") {
			t.Errorf("%s: override in system prompt = %v", a.ID, got)
		}
		if strings.Contains(user, "British") {
			t.Errorf("%s: house style leaked into the user prompt", a.ID)
		}
		for _, k := range keys {
			if !strings.Contains(system, `"`+k+`"`) {
				t.Errorf("%s: an override lost the JSON contract's key %q", a.ID, k)
			}
		}
	}
	system, _, _ := render(t, sampleRun(t, "evaluate"), s)
	if strings.Contains(system, "Determine Card Type") || !strings.Contains(system, `"overall_score"`) {
		t.Errorf("evaluate override kept the default or lost the contract:\n%s", system)
	}
}

func TestDefaultInstructionsTakeNoData(t *testing.T) {
	full := promptData{Card: "{}", Field: "description", FieldText: "x", Instruction: "y", PointOfView: "first", Count: 7, Image: true}
	for _, k := range promptKeys {
		empty, err := execute(k+".instructions", promptData{})
		if err != nil {
			t.Fatalf("%s: %v", k, err)
		}
		filled, err := execute(k+".instructions", full)
		if err != nil || filled != empty || strings.TrimSpace(empty) == "" {
			t.Errorf("%s: instructions depend on the run or are empty:\n%s", k, empty)
		}
	}
}

func TestEveryRunHasInstructions(t *testing.T) {
	for _, a := range actions {
		in, err := parseRun(sampleRun(t, a.ID))
		if err != nil {
			t.Fatal(err)
		}
		if !slices.Contains(promptKeys, in.instructionsKey()) {
			t.Errorf("%s: instructions key %q isn't in promptKeys", a.ID, in.instructionsKey())
		}
	}
}

func TestPromptSettingsMatchKeys(t *testing.T) {
	var m struct {
		Config struct {
			Properties struct {
				Prompts struct {
					Properties map[string]any `toml:"properties"`
				} `toml:"prompts"`
			} `toml:"properties"`
		} `toml:"config"`
	}
	if _, err := toml.DecodeFile("plugin.toml", &m); err != nil {
		t.Fatal(err)
	}
	want := append([]string{"house_style"}, promptKeys...)
	slices.Sort(want)
	if got := slices.Sorted(maps.Keys(m.Config.Properties.Prompts.Properties)); !slices.Equal(got, want) {
		t.Fatalf("prompts properties %v, want %v", got, want)
	}
}

func TestSettingsDefaultsAreTheInstructions(t *testing.T) {
	res, err := settingsDefaults()
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Defaults) != len(promptKeys) {
		t.Fatalf("%d defaults, want %d", len(res.Defaults), len(promptKeys))
	}
	want, _ := execute("tighten.instructions", promptData{})
	if res.Defaults["/prompts/tighten"] != want {
		t.Fatalf("tighten default %q", res.Defaults["/prompts/tighten"])
	}
}
```

In `TestPromptsCarryRunInputs`, the counts move to the user prompt. Change:
- the examples check to `if _, user, _ := render(t, p, settings{}); !strings.Contains(user, "Write 5 new example")`;
- the greetings check to look for `"Write 4 new alternate greetings"` in `user`;
- the generate POV loop to check `system+user`. Its wanted strings are still in the instructions.

In `run_test.go`'s `withLLM`, change the config to:

```go
	return f, map[string]any{"connection": id, "prompts": map[string]any{"house_style": "No em dashes."}}
```

In `enrich_test.go`'s `TestEnrichPromptVersionFollowsTheSetup`, replace the `house_style` and `addition` changes and the final check with:

```go
		"house_style": func(c map[string]any) { c["prompts"] = map[string]any{"house_style": "British spelling."} },
		"override":    func(c map[string]any) { c["prompts"] = map[string]any{"house_style": "No em dashes.", "evaluate": "Be harsh."} },
```

```go
	if other := prompt(func(c map[string]any) {
		c["prompts"] = map[string]any{"house_style": "No em dashes.", "revise": "x"}
	}); other != base {
		t.Errorf("another action's override changed the prompt version")
	}
```

Each change replaces `withLLM`'s whole `prompts` map. That's why `override` and `other` repeat its house style: only the key under test should differ from `base`.

- [ ] **Step 2: Run them to see them fail**

Run: `go test ./forge-ai/`
Expected: FAIL to compile, with `undefined: promptKeys`, `settingsDefaults` and `instructionsKey`, and `unknown field Prompts`.

- [ ] **Step 3: Settings hold the prompts**

In `forge-ai/settings.go`, replace `HouseStyle` and `Additions` with:

```go
	// Prompts are the owner's overrides by instructions key, and house_style.
	Prompts map[string]string `json:"prompts"`
```

- [ ] **Step 4: Render instructions into the frame**

In `forge-ai/prompts.go`, add to `promptData`:

```go
	// Instructions are the action's rules: the owner's override or the default.
	Instructions string
	// Keys are the keys generate writes, quoted, for its JSON contract.
	Keys string
```

Add:

```go
// promptKeys are the instructions blocks, in the Prompts tab's order. Each is
// a setting under prompts and a template named <key>.instructions.
var promptKeys = []string{"generate-first", "generate-third", "generate-scenario", "revise", "tighten", "expand",
	"to-prose", "change-pov", "examples", "alternate-greetings", "lore-entries", "evaluate"}

// instructionsKey names the run's instructions block.
func (r *run) instructionsKey() string {
	if r.ID == "generate" {
		return "generate-" + r.pointOfView
	}
	return r.ID
}

// instructions are key's override, or else its default block.
func instructions(s settings, key string) (string, error) {
	if o := strings.TrimSpace(s.Prompts[key]); o != "" {
		return o, nil
	}
	return execute(key+".instructions", promptData{})
}

// quoted is keys as a JSON-style list without brackets: "a", "b".
func quoted(keys []string) string {
	return `"` + strings.Join(keys, `", "`) + `"`
}

// settingsDefaults is each instructions block, by its setting's pointer.
func settingsDefaults() (protocol.SettingsDefaultsResult, error) {
	out := protocol.SettingsDefaultsResult{Defaults: map[string]any{}}
	for _, k := range promptKeys {
		text, err := execute(k+".instructions", promptData{})
		if err != nil {
			return out, err
		}
		out.Defaults["/prompts/"+k] = text
	}
	return out, nil
}
```

In `prompts`, before `if system, err = execute(r.ID+".system", d)`, add:

```go
	if d.Instructions, err = instructions(s, r.instructionsKey()); err != nil {
		return "", "", err
	}
	d.Keys = quoted(r.writes())
```

Replace the house style and addition lines at the end with:

```go
	if hs := strings.TrimSpace(s.Prompts["house_style"]); hs != "" {
		system += "\n\n## House style\n\n" + hs
	}
```

Change the doc comment on `prompts` to "prompts renders the run's system and user prompts. The system prompt ends with the owner's house style."

- [ ] **Step 5: Split the templates**

Each `<id>.system` keeps its contract and data, and renders `[[.Instructions]]` where the rules were. Write the files as follows.

`forge-ai/prompts/field.tmpl`: keep `field-rules` and `field.user`. Add a shared frame:

```
[[- define "field.system" -]]
[[template "field-rules" .]]

[[.Instructions]]

Return only the JSON object: no code fence, no commentary.
[[- end]]
```

For each of `tighten`, `expand`, `to-prose` and `revise`, replace the `.system` block with an instructions block holding its rule line, as Task 3 left it, plus a one-line frame. For `tighten`:

```
[[- define "tighten.instructions" -]]
Tighten it: cut filler, repetition, and words that don't change how {{char}} behaves. Keep every concrete fact, name, rule, and voice marker. Aim for about two thirds of the length. Don't add anything.
[[- end]]
[[- define "tighten.system" -]][[template "field.system" .]][[end]]
```

`expand`, `to-prose` and `revise` follow the same pattern, each with its own rule line.

`forge-ai/prompts/change-pov.tmpl`: the point-of-view lines stay in the frame, because they depend on the input:

```
[[- define "change-pov.instructions" -]]
Change only the point of view. Keep every fact, name, rule, detail, and the tone, and keep the formatting.
[[- end]]

[[- define "change-pov.system" -]]
[[template "field-rules" .]]

[[if eq .PointOfView "first" -]]
Rewrite it in first person, in {{char}}'s own voice: I, me, my. In system_prompt and post_history_instructions, enforce first person: {{char}} speaks as I and never breaks character.
[[- else -]]
Rewrite it in third person: he, she, they, or {{char}}; never I, me, or my outside quoted dialogue. In first_mes and mes_example, narrate {{char}} as an omniscient narrator; dialogue stays in {{char}}'s voice. In system_prompt and post_history_instructions, enforce third-person narration.
[[- end]]

[[.Instructions]]

Return only the JSON object: no code fence, no commentary.
[[- end]]

[[- define "change-pov.user" -]][[template "field.user" .]][[end]]
```

`forge-ai/prompts/examples.tmpl`:

```
[[- define "examples.instructions" -]]
- Write the example exchanges the user asks for, showing {{char}}'s voice across different emotional contexts: a neutral or casual moment, pressure or conflict, and vulnerability or want, then any other contexts the card suggests.
- Match the card's point of view, voice, and tone exactly.
- Format each block:
<START>
{{char}}: [dialogue line]
*[brief action or internal beat]*
- Each block should be independently useful if the others are dropped from context.
- Never write {{user}}'s lines or actions beyond a brief prompt line starting "{{user}}:".
[[- end]]

[[- define "examples.system" -]]
You write example dialogue (mes_example) for roleplay character cards. Return one JSON object with the single key "mes_example".

## Rules

[[.Instructions]]

Return only the JSON object: no code fence, no commentary.
[[- end]]
```

Keep `examples.user` as it is; it already says `Write [[.Count]] new example exchanges`.

`forge-ai/prompts/alternate-greetings.tmpl` and `lore-entries.tmpl`: do the same.
- The first line, which names the JSON key, stays in `.system` together with the final "Return only the JSON object" line.
- The `## Rules` bullets move into `alternate-greetings.instructions` and `lore-entries.instructions`.
- In the first bullet, `Write [[.Count]] new greetings.` becomes `Write the greetings the user asks for.` and `Write [[.Count]] new entries` becomes `Write the entries the user asks for`.
- Keep the `.user` blocks, which state the count.

`forge-ai/prompts/evaluate.tmpl`:
- Move everything from `## Step 1: Determine Card Type` through the `**suggestions**` line, as written, into `[[define "evaluate.instructions"]]`.
- `evaluate.system` becomes its first line, a blank line, `[[.Instructions]]`, a blank line, and the `## Output` section as it is now.

`forge-ai/prompts/generate.tmpl`:
- Rename `generate-first`, `generate-third` and `generate-scenario` to `generate-first.instructions`, `generate-third.instructions` and `generate-scenario.instructions`.
- Delete the `[[template "json-only"]]` line at the end of each; those are the only template actions in them.
- Replace `generate.system` with:

```
[[- define "generate.system" -]]
[[.Instructions]]

Return one JSON object with exactly these keys, in this order: [[.Keys]].

[[template "json-only"]]
[[- end]]
```

Each variant's "Output Format" section describes the fields and stays in its instructions. The frame's key list keeps the contract, even for an override that drops that section.

- [ ] **Step 6: Answer `settings.defaults`**

In `forge-ai/main.go`, in the first `switch` of `handle`, add before `case protocol.MethodShutdown:`:

```go
	case protocol.MethodSettingsDefaults:
		return settingsDefaults()
```

Change the initialize reply's capabilities to `[]string{"action", "records", "enrich", "settings"}`.

- [ ] **Step 7: Hash the override into the score version**

In `forge-ai/enrich.go`, in `evaluatePrompt`, replace the `setup` line with:

```go
	setup, err := json.Marshal([]any{s.Prompts["house_style"], s.Prompts["evaluate"], temperature, maxTokens})
```

- [ ] **Step 8: The Prompts tab in the manifest**

In `forge-ai/plugin.toml`, change `capabilities` to `["action", "records", "enrich", "settings"]`. Delete the `house_style`, `additions` and `additions.properties` tables, and add:

```toml
[config.properties.prompts]
type = "object"
title = "Prompts"
description = "Each action's instructions. Leave one unchanged to follow the plugin's default as it improves; reset brings it back."

[config.properties.prompts.properties]
house_style = { type = "string", title = "House style", contentMediaType = "text/plain", description = "Added to every action's system prompt, such as \"British spelling; no em dashes\"." }
generate-first = { type = "string", title = "Forge, first person", contentMediaType = "text/plain" }
generate-third = { type = "string", title = "Forge, third person", contentMediaType = "text/plain" }
generate-scenario = { type = "string", title = "Forge, scenario card", contentMediaType = "text/plain" }
revise = { type = "string", title = "Revise", contentMediaType = "text/plain" }
tighten = { type = "string", title = "Tighten", contentMediaType = "text/plain" }
expand = { type = "string", title = "Expand", contentMediaType = "text/plain" }
to-prose = { type = "string", title = "Convert to prose", contentMediaType = "text/plain" }
change-pov = { type = "string", title = "Change point of view", contentMediaType = "text/plain" }
examples = { type = "string", title = "Write examples", contentMediaType = "text/plain" }
alternate-greetings = { type = "string", title = "Write alternate greetings", contentMediaType = "text/plain" }
lore-entries = { type = "string", title = "Write lore entries", contentMediaType = "text/plain" }
evaluate = { type = "string", title = "Evaluate", contentMediaType = "text/plain" }
```

`connection`, `temperature` and `max_tokens` stay top-level, so they go on the General tab.

- [ ] **Step 9: Run the tests**

Run: `go vet ./forge-ai/ && go test ./forge-ai/`
Expected: PASS

- [ ] **Step 10: Commit**

```bash
git add forge-ai/
git commit -m "feat(forge-ai): show and override each action's instructions"
```

---

### Task 5: Whole-card runs

**Files:**
- Create: `forge-ai/wholecard.go`, `forge-ai/wholecard_test.go`, `forge-ai/prompts/plan.tmpl`
- Modify: `forge-ai/actions.go`, `forge-ai/main.go`, `forge-ai/prompts.go`, `forge-ai/prompts/field.tmpl`, `forge-ai/plugin.toml`

- [ ] **Step 1: Write the failing tests**

`forge-ai/wholecard_test.go`:

```go
package main

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"
)

func userPrompt(req map[string]any) string {
	return req["messages"].([]any)[1].(map[string]any)["content"].(string)
}

func TestWholeCardRunPlansThenEditsEachPlannedField(t *testing.T) {
	llm, cfg := withLLM(t,
		streamed(`{"description": "Cut the repeated height.", "personality": "skip"}`),
		streamed(`{"description": "A towering orc who guards {{user}}."}`))
	h := startPlugin(t, cfg, nil)
	p := sampleRun(t, "tighten")
	p.Field = ""
	res, err := h.run(t, p)
	if err != nil {
		t.Fatal(err)
	}
	m := proposalOf(t, res)
	if m["description"] != "A towering orc who guards {{user}}." || m["personality"] != "Gruff." || m["extensions"] == nil {
		t.Fatalf("proposal %v", m)
	}
	if len(llm.requests) != 2 || !strings.Contains(userPrompt(llm.requests[1]), "Plan for this field: Cut the repeated height.") {
		t.Fatalf("requests %v", llm.requests)
	}
	var messages []string
	for _, pr := range h.progress {
		if pr.Done >= pr.Total {
			t.Fatalf("progress %+v: done must stay below total", pr)
		}
		messages = append(messages, pr.Message)
	}
	if !slices.Equal(messages[:2], []string{"Planning Tighten", "Tighten: Description (1 of 1)"}) {
		t.Fatalf("progress %q", messages)
	}
}

func TestLaterSectionsSeeEarlierEdits(t *testing.T) {
	llm, cfg := withLLM(t,
		streamed(`{"description": "Shorten.", "personality": "Match the new description."}`),
		streamed(`{"description": "A big orc."}`),
		streamed(`{"personality": "Calm."}`))
	h := startPlugin(t, cfg, nil)
	p := sampleRun(t, "tighten")
	p.Field = ""
	res, err := h.run(t, p)
	if err != nil {
		t.Fatal(err)
	}
	if m := proposalOf(t, res); m["description"] != "A big orc." || m["personality"] != "Calm." {
		t.Fatalf("proposal %v", m)
	}
	if !strings.Contains(userPrompt(llm.requests[2]), "A big orc.") {
		t.Fatalf("personality's section didn't see the new description:\n%s", userPrompt(llm.requests[2]))
	}
}

func TestPlanThatSkipsEverythingChangesNothing(t *testing.T) {
	llm, cfg := withLLM(t, streamed(`{"description": "skip"}`))
	h := startPlugin(t, cfg, nil)
	p := sampleRun(t, "expand")
	p.Field = ""
	res, err := h.run(t, p)
	if err != nil {
		t.Fatal(err)
	}
	var got, want map[string]any
	_ = json.Unmarshal(res.Proposal, &got)
	_ = json.Unmarshal([]byte(sampleCard), &want)
	if len(llm.requests) != 1 || !jsonEqual(got, want) {
		t.Fatalf("%d requests, proposal %v", len(llm.requests), got)
	}
}

func TestPlanCarriesTheDirectionAndNamesTheFields(t *testing.T) {
	llm, cfg := withLLM(t, streamed(`{}`))
	h := startPlugin(t, cfg, nil)
	p := sampleRun(t, "revise")
	p.Field = ""
	if _, err := h.run(t, p); err != nil {
		t.Fatal(err)
	}
	system := systemPrompt(llm.requests[0])
	if !strings.Contains(userPrompt(llm.requests[0]), "The owner's direction: make him the brother of @cards/Billy") {
		t.Fatalf("plan user prompt:\n%s", userPrompt(llm.requests[0]))
	}
	for _, k := range cardKeys {
		if !strings.Contains(system, `"`+k+`"`) {
			t.Errorf("plan system prompt never names %q", k)
		}
	}
	if _, ok := llm.requests[0]["tools"]; ok {
		t.Error("the plan offered tools")
	}
}
```

Add a `jsonEqual` helper next to it, comparing by `reflect.DeepEqual` after a JSON round trip. If `actions_test.go` already has one, use that instead.

```go
func jsonEqual(a, b any) bool {
	ja, _ := json.Marshal(a)
	jb, _ := json.Marshal(b)
	return string(ja) == string(jb)
}
```

Add `"whole-card"` to the expected list in `TestPromptSettingsMatchKeys` by way of `promptKeys` (Step 4), and to the manifest in Step 6.

- [ ] **Step 2: Run them to see them fail**

Run: `go test ./forge-ai/ -run 'WholeCard|LaterSections|PlanThat|PlanCarries'`
Expected: FAIL. `parseRun` refuses the empty field with `doesn't run on field ""`.

- [ ] **Step 3: Accept the whole card and carry a note**

In `forge-ai/actions.go`, in `parseRun`, replace:

```go
	if len(a.Fields) > 0 && !slices.Contains(a.Fields, p.Field) {
```

with:

```go
	if len(a.Fields) > 0 && !slices.Contains(a.Fields, p.Field) && (p.Field != "" || !a.WholeCard) {
```

Add to `run`:

```go
	// note is the plan's note for this field, in a whole-card run.
	note string
```

In `forge-ai/prompts.go`, add `Note string` to `promptData` with the comment `// Note is the plan's note for the field, in a whole-card run.`, and set it in `prompts` with the other run values: `d := promptData{..., Note: r.note}`.

In `forge-ai/prompts/field.tmpl`'s `field.user`, after the instruction block, add:

```
[[- if .Note]]Plan for this field: [[.Note]]

[[end -]]
```

- [ ] **Step 4: The plan prompt**

`forge-ai/prompts/plan.tmpl`:

```
[[- define "whole-card.instructions" -]]
Plan an edit of the whole card, field by field. Each field is then edited on its own by someone who sees only your note for it and the rest of the card.

- Give a field a note only when the edit should change it; otherwise answer "skip".
- Make the notes work together: each fact lives in the field that owns it, so a note can say to cut a fact another field already holds.
- Keep the card's voice, point of view, and tone consistent across the notes, unless the edit is to change them.
- Be specific: name what to cut, add, or move.
[[- end]]

[[- define "plan.system" -]]
[[.Instructions]]

[[template "field-ownership"]]

## The edit: [[.Action]][[if .PointOfView]], to [[.PointOfView]] person[[end]]

[[.ActionInstructions]]

Return one JSON object whose keys are the fields to plan, [[.Keys]], each with a note of one to three sentences or "skip". Return only the JSON object: no code fence, no commentary.
[[- end]]

[[- define "plan.user" -]]
[[- if .Instruction]]The owner's direction: [[.Instruction]]

[[end -]]
The card:
[[.Card]]
[[- template "context" .]]
[[- end]]
```

In `forge-ai/prompts.go`, add to `promptData`:

```go
	// Action and ActionInstructions describe the edit a whole-card plan is for.
	Action             string
	ActionInstructions string
```

append `"whole-card"` to `promptKeys`, and add:

```go
// planPrompts renders the prompts for a whole-card run's plan.
func (r *run) planPrompts(s settings) (system, user string, err error) {
	card, err := r.sent()
	if err != nil {
		return "", "", err
	}
	d := promptData{Instruction: r.Instruction, PointOfView: r.pointOfView, Context: r.Context, Action: r.Label, Keys: quoted(r.Fields)}
	if d.Card, err = indented(card); err != nil {
		return "", "", err
	}
	if d.Instructions, err = instructions(s, "whole-card"); err != nil {
		return "", "", err
	}
	if d.ActionInstructions, err = instructions(s, r.instructionsKey()); err != nil {
		return "", "", err
	}
	if system, err = execute("plan.system", d); err != nil {
		return "", "", err
	}
	if user, err = execute("plan.user", d); err != nil {
		return "", "", err
	}
	if hs := strings.TrimSpace(s.Prompts["house_style"]); hs != "" {
		system += "\n\n## House style\n\n" + hs
	}
	return system, user, nil
}
```

The house style tail is now in two places. Leave it there until a third copy appears.

- [ ] **Step 5: `forge-ai/wholecard.go`**

```go
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/inkandale/innkeeper/protocol"
)

// planLimits are the plan call's temperature and max tokens, unless the
// instance sets its own.
var planLimits = action{temperature: 0.3, maxTokens: 2048}

// wholeCard runs a field action on the whole card: one call plans a note per
// field, then the action runs on each planned field in turn, against the card
// as the fields before it left it.
func (srv *server) wholeCard(ctx context.Context, in *instance, r *run) (*protocol.ActionRunResult, error) {
	_ = srv.conn.Notify(protocol.NotifyProgress, protocol.Progress{Run: r.Run, Total: 1, Message: "Planning " + r.Label})
	notes, err := srv.plan(ctx, in, r)
	if err != nil {
		return nil, fmt.Errorf("plan: %w", err)
	}
	var planned []string
	for _, f := range r.Fields {
		if notes[f] != "" {
			planned = append(planned, f)
		}
	}
	for i, f := range planned {
		_ = srv.conn.Notify(protocol.NotifyProgress, protocol.Progress{Run: r.Run, Done: int64(i + 1), Total: int64(len(planned) + 1),
			Message: fmt.Sprintf("%s: %s (%d of %d)", r.Label, label(f), i+1, len(planned))})
		section := *r
		section.Field, section.note = f, notes[f]
		res, err := srv.perform(ctx, in, &section, true, func() func(string) { return func(string) {} })
		if err != nil {
			return nil, fmt.Errorf("%s: %w", label(f), err)
		}
		if err := json.Unmarshal(res.Proposal, &r.draft); err != nil {
			return nil, err
		}
	}
	proposal, err := json.Marshal(r.draft)
	if err != nil {
		return nil, err
	}
	return &protocol.ActionRunResult{Proposal: proposal}, nil
}

// plan returns the model's note per field of r's, leaving out each it skips.
func (srv *server) plan(ctx context.Context, in *instance, r *run) (map[string]string, error) {
	system, user, err := r.planPrompts(in.settings)
	if err != nil {
		return nil, err
	}
	temperature, maxTokens := planLimits.limits(in.settings)
	req := protocol.AIChatParams{Temperature: &temperature, MaxTokens: &maxTokens,
		Messages: []protocol.ChatMessage{{Role: "system", Content: system}, {Role: "user", Content: user}}}
	reply, err := srv.complete(ctx, in, r.Run, req, toolbox{}, func() func(string) { return func(string) {} })
	if err != nil {
		return nil, err
	}
	obj, err := parseObject(reply.Text)
	if err != nil {
		return nil, err
	}
	notes := map[string]string{}
	for _, f := range r.Fields {
		var note string
		if json.Unmarshal(obj[f], &note) != nil {
			continue
		}
		if note = strings.TrimSpace(note); note != "" && !strings.EqualFold(note, "skip") {
			notes[f] = note
		}
	}
	return notes, nil
}
```

`section := *r` shares `r.draft`'s map. Each section's `proposal` copies the draft before changing its field, and the `Unmarshal` writes the change back into `r.draft`, so the next section sees it.

- [ ] **Step 6: Dispatch, label, and manifest**

In `forge-ai/main.go`, extract the label lookup from `progress`:

```go
// label names a card key in progress messages.
func label(key string) string {
	if l := labels[key]; l != "" {
		return l
	}
	return key
}
```

In `progress`, replace the four lines that compute `label` with `label := label(key)`, renaming that local to `name` so it doesn't shadow the function: `name := label(key)`. Then use `name` in the `Sprintf`.

In `run`, after `if in.err != nil { return nil, in.err }`, add:

```go
	if r.WholeCard && r.Field == "" {
		return srv.wholeCard(ctx, in, r)
	}
```

In `forge-ai/plugin.toml`, under `[config.properties.prompts.properties]`, after `house_style`, add:

```toml
whole-card = { type = "string", title = "Whole card guidance", contentMediaType = "text/plain", description = "How a whole-card edit plans each field before editing it." }
```

- [ ] **Step 7: Run the tests**

Run: `go vet ./forge-ai/ && go test ./forge-ai/`
Expected: PASS

- [ ] **Step 8: Commit**

```bash
git add forge-ai/
git commit -m "feat(forge-ai): plan a whole-card edit, then edit each field"
```

---

### Task 6: README, version, and index

**Files:**
- Modify: `forge-ai/README.md`, `forge-ai/plugin.toml`, `forge-ai/main.go`, `index.toml`

- [ ] **Step 1: Bump to 0.5.0**

Set `version = "0.5.0"` in `forge-ai/plugin.toml` and the `forge-ai` entry of `index.toml`, and `const version = "0.5.0"` in `forge-ai/main.go`. In `index.toml`, set `capabilities = ["action", "records", "enrich", "settings"]` and replace the description with:

```toml
description = "Write, revise, tighten, expand, and evaluate cards, write examples, alternate greetings, and lore entries, and score cards in bulk for the library grid, through an ai instance. Each action's instructions can be overridden on the Prompts tab. Keeps Agent Skills the model can load. Needs a core with the ai, action, records, enrich, and settings capabilities, and an ai instance offering chat."
```

- [ ] **Step 2: Rewrite the README's top, Actions, and Prompts sections**

Replace the first two paragraphs of `forge-ai/README.md` with:

```markdown
Writes, revises, tightens, expands, evaluates, and scores character cards,
and adds lorebook entries, with a language model. It offers the `action`,
`records`, `enrich`, and `settings` capabilities, and calls `ai.chat` on the
`ai` instance its `connection` setting names, so it needs a core with those
and an `ai` instance that offers chat.

Settings has two tabs. **General** holds `connection`, and `temperature` and
`max_tokens`, which override each action's own. **Prompts** holds the house
style, the whole-card guidance, and each action's instructions.
```

Replace the Actions table and the paragraph after it with:

```markdown
| ID | Runs on | Does |
|---|---|---|
| `generate` (Forge) | a new card | Writes a card from a concept, in first or third person, or as a scenario card. Takes a reference image |
| `revise` | a field or the whole card | Changes it as the instruction says. Runs on `name`, the prose fields, and `tags` |
| `tighten` | a prose field or the whole card | Cuts filler and repetition, keeping every fact |
| `expand` | a prose field or the whole card | Adds concrete detail without changing what's there |
| `to-prose` | a prose field or the whole card | Turns lists and bracket formats into paragraphs |
| `change-pov` | a prose field or the whole card | Rewrites it in first or third person. Skips `creator_notes` |
| `examples` | `mes_example` | Writes 1 to 10 example exchanges, 3 by default |
| `alternate-greetings` | a card | Adds 1 to 10 alternate greetings, 3 by default, after the existing ones |
| `lore-entries` | a lorebook | Adds 1 to 20 entries, 5 by default, about what the instruction asks, from the cards it mentions |
| `evaluate` | a card | Reports a score, six dimension scores, contradictions, misplaced content, and suggestions |

An edit on the whole card runs in two steps:

1. One call reads the whole card, the owner's direction, and the action's
   instructions, and writes a note per field, or `skip`.
2. The field action runs on each field with a note, in order, with its note
   and the card as the earlier fields left it.

The core keeps only the changes to the action's fields. A plan that skips
every field proposes the card unchanged.
```

In "Scores", change the version sentence to:

```markdown
Each value records the model the `ai` instance answered with and the
prompt's version: `evaluate-` plus a hash of `prompts/evaluate.tmpl`, the
house style, the Evaluate override, and the temperature and max tokens sent.
Scores whose versions differ came from different setups.
```

Replace the "Prompts" section's last paragraph with:

```markdown
Each prompt's rules are an instructions block, `[[define "<key>.instructions"]]`,
which takes no template data. The Prompts tab shows each block as its
default; an override replaces the block, and the frame around it, which
holds the JSON contract and the card, stays. The house style is added to
every system prompt.
```

Under "Library access", change `generate`, `revise`, `alternate-greetings`, and `lore-entries` to read: "`generate`, `revise`, `alternate-greetings`, and `lore-entries` may read the library through `library.call` when the owner turns on Library access for the instance. A whole-card run's plan call offers no tools; each field's call does." In the retry sentence, replace "If the model rejects the request with tools" with "If the `ai` instance rejects the request with tools (`-32010`)".

- [ ] **Step 3: Add the upgrade note**

Add a section at the end of `forge-ai/README.md`:

```markdown
## Upgrading from 0.4

0.5 moves the model settings to the `ai` plugin.

1. Before upgrading, copy any `house_style` and `additions` text you want
   to keep. 0.5 drops `provider`, `base_url`, `model`, `api_key`, and
   `additions`, and reads the house style from the Prompts tab.
2. Create an `ai` instance with the old provider, base URL, model, and key,
   with chat ticked, and press Test connection.
3. In the forge-ai instance's General tab, pick it as the AI connection.
   Until you do, every action fails with "pick an AI connection".
4. Paste the house style, and any addition worth keeping, into the Prompts
   tab. An addition was extra text after the default; an override replaces
   the default, so start from the text shown and add to it.
```

- [ ] **Step 4: Run the checks**

```bash
go vet ./...
go test ./...
make check
```

Expected: PASS. `make check` runs the core's contract kit against each plugin.

- [ ] **Step 5: Read the diff**

Run: `git diff origin/main --stat`, then read the diff once for dead code (`providers`, `anyllm`, `Additions`), stale names (`rewrite`, `tighten-card`, `Reforge`), and changes the task didn't ask for.

- [ ] **Step 6: Commit**

```bash
git add forge-ai/ index.toml
git commit -m "docs(forge-ai): describe 0.5's actions, prompts, and upgrade"
```

---

## Deploying

After the core release with parts 1, 2 and 4a and the plugins release with `ai` and forge-ai 0.5 both ship:

1. Copy the old forge-ai instance's house style and additions somewhere.
2. Create an `ai` instance and tick chat. Use the old provider, base URL, model and key, then press Test connection.
3. Open the forge-ai instance, pick the new connection on General, and paste the prompts you kept onto the Prompts tab.

forge-ai keeps its plugin id, so its skills, scores and instance survive. The old `additions` values are dropped.
