package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/davidgibbons/innkeeper/protocol"
	anyllm "github.com/mozilla-ai/any-llm-go"
)

// fakeLLM speaks the OpenAI-compatible chat API. Each request gets the next
// reply: a list of SSE chunk deltas, or an HTTP status to fail with.
type fakeLLM struct {
	mu       sync.Mutex
	replies  []fakeReply
	requests []map[string]any
}

type fakeReply struct {
	status int
	deltas []string // each a JSON delta object
}

func content(text string) string {
	b, _ := json.Marshal(map[string]string{"content": text})
	return string(b)
}

// streamed splits text into small content deltas, as a model streams it.
func streamed(text string) fakeReply {
	var r fakeReply
	for len(text) > 0 {
		n := min(7, len(text))
		r.deltas = append(r.deltas, content(text[:n]))
		text = text[n:]
	}
	return r
}

func (f *fakeLLM) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var body map[string]any
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	f.mu.Lock()
	f.requests = append(f.requests, body)
	if len(f.replies) == 0 {
		f.mu.Unlock()
		http.Error(w, `{"error": {"message": "no reply scripted"}}`, 500)
		return
	}
	reply := f.replies[0]
	f.replies = f.replies[1:]
	f.mu.Unlock()
	if reply.status != 0 {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(reply.status)
		fmt.Fprint(w, `{"error": {"message": "this model does not support tools", "type": "invalid_request_error"}}`)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	for _, d := range reply.deltas {
		fmt.Fprintf(w, "data: {\"id\": \"c\", \"object\": \"chat.completion.chunk\", \"model\": \"m\", \"choices\": [{\"index\": 0, \"delta\": %s}]}\n\n", d)
	}
	fmt.Fprint(w, "data: [DONE]\n\n")
}

// host is a fake Innkeeper core: it talks to the plugin's handler over pipes,
// records progress, and answers library.call.
type host struct {
	conn     *protocol.Conn
	mu       sync.Mutex
	progress []protocol.Progress
	calls    []protocol.LibraryCallParams
	library  func(protocol.LibraryCallParams) (any, error)
}

func startPlugin(t *testing.T, config map[string]any, secrets map[string]string) *host {
	t.Helper()
	toPlugin, fromHost := io.Pipe()
	toHost, fromPlugin := io.Pipe()
	srv := &server{}
	srv.conn = protocol.NewConn(toPlugin, fromPlugin, srv.handle)
	h := &host{}
	h.conn = protocol.NewConn(toHost, fromHost, func(ctx context.Context, method string, params json.RawMessage) (any, error) {
		h.mu.Lock()
		defer h.mu.Unlock()
		switch method {
		case protocol.NotifyProgress:
			var p protocol.Progress
			_ = json.Unmarshal(params, &p)
			h.progress = append(h.progress, p)
			return nil, nil
		case protocol.MethodLibraryCall:
			var p protocol.LibraryCallParams
			_ = json.Unmarshal(params, &p)
			h.calls = append(h.calls, p)
			if h.library == nil {
				return nil, protocol.NewError(protocol.CodeForbidden, "library access is off", false)
			}
			return h.library(p)
		}
		return nil, protocol.MethodNotFound(method)
	})
	ctx, cancel := context.WithCancel(context.Background())
	go srv.conn.Run(ctx)
	go h.conn.Run(ctx)
	t.Cleanup(func() {
		cancel()
		fromHost.Close()
		fromPlugin.Close()
	})
	var res protocol.InitializeResult
	if err := h.conn.Call(ctx, protocol.MethodInitialize, protocol.InitializeParams{Protocol: 1, Config: config, Secrets: secrets}, &res); err != nil {
		t.Fatal(err)
	}
	return h
}

func (h *host) run(t *testing.T, p protocol.ActionRunParams) (protocol.ActionRunResult, error) {
	t.Helper()
	var res protocol.ActionRunResult
	err := h.conn.Call(context.Background(), protocol.MethodActionRun, p, &res)
	return res, err
}

func withLLM(t *testing.T, replies ...fakeReply) (*fakeLLM, map[string]any) {
	f := &fakeLLM{replies: replies}
	ts := httptest.NewServer(f)
	t.Cleanup(ts.Close)
	return f, map[string]any{"provider": "openai-compatible", "base_url": ts.URL, "model": "m", "house_style": "No em dashes."}
}

const generated = `<think>An orc courier, I think.</think>{"name": "Ash", "description": "A courier.", "personality": "Wry.",
"scenario": "A dock.", "first_mes": "Hi.", "mes_example": "<START>", "tags": ["orc"], "system_prompt": "Stay in character.",
"post_history_instructions": "Short."}`

func TestRunStreamsProgressPerField(t *testing.T) {
	llm, cfg := withLLM(t, streamed(generated))
	h := startPlugin(t, cfg, nil)
	res, err := h.run(t, sampleRun(t, "generate"))
	if err != nil {
		t.Fatal(err)
	}
	var card map[string]any
	if err := json.Unmarshal(res.Proposal, &card); err != nil || card["name"] != "Ash" || len(card) != 9 {
		t.Fatalf("proposal %s: %v", res.Proposal, err)
	}
	if res.Raw != generated {
		t.Fatalf("raw = %q", res.Raw)
	}
	var messages []string
	for _, p := range h.progress {
		if p.Run != "1.1" || p.Total != 9 {
			t.Fatalf("progress %+v", p)
		}
		messages = append(messages, p.Message)
	}
	if len(messages) != 9 || messages[0] != "Writing Name (1 of 9)" || messages[2] != "Writing Personality (3 of 9)" ||
		h.progress[2].Done != 2 {
		t.Fatalf("progress %q", messages)
	}
	req := llm.requests[0]
	if req["stream"] != true || req["model"] != "m" {
		t.Fatalf("request %v", req)
	}
	system := req["messages"].([]any)[0].(map[string]any)["content"].(string)
	if !strings.Contains(system, "No em dashes.") {
		t.Fatalf("house style not sent: %q", system)
	}
}

func toolCall(id, name, args string) string {
	b, _ := json.Marshal(map[string]any{"tool_calls": []any{map[string]any{
		"index": 0, "id": id, "type": "function", "function": map[string]any{"name": name, "arguments": args}}}})
	return string(b)
}

func argsDelta(args string) string {
	b, _ := json.Marshal(map[string]any{"tool_calls": []any{map[string]any{"index": 0, "function": map[string]any{"arguments": args}}}})
	return string(b)
}

func TestLibraryToolCallAnsweredByHost(t *testing.T) {
	llm, cfg := withLLM(t,
		fakeReply{deltas: []string{toolCall("call_1", "get_card", ""), argsDelta(`{"id"`), argsDelta(`: 7}`)}},
		streamed(`{"name": "Brakka", "description": "Billy's brother."}`))
	h := startPlugin(t, cfg, nil)
	h.library = func(p protocol.LibraryCallParams) (any, error) {
		return protocol.MCPCallResult{Content: []protocol.MCPContent{{Type: "text", Text: `{"name": "Billy", "description": "A goblin."}`}}}, nil
	}
	res, err := h.run(t, sampleRun(t, "revise"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(res.Proposal), "Billy's brother.") {
		t.Fatalf("proposal %s", res.Proposal)
	}
	if len(h.calls) != 1 || h.calls[0].Run != "1.1" || h.calls[0].Name != "get_card" || string(h.calls[0].Arguments) != `{"id":7}` {
		t.Fatalf("library calls %+v", h.calls)
	}
	if tools, _ := llm.requests[0]["tools"].([]any); len(tools) != 4 {
		t.Fatalf("first request offered tools %v", llm.requests[0]["tools"])
	}
	msgs := llm.requests[1]["messages"].([]any)
	last := msgs[len(msgs)-1].(map[string]any)
	if last["role"] != "tool" || last["tool_call_id"] != "call_1" || !strings.Contains(last["content"].(string), "A goblin.") {
		t.Fatalf("tool result message %v", last)
	}
	asked := msgs[len(msgs)-2].(map[string]any)["tool_calls"].([]any)[0].(map[string]any)["function"].(map[string]any)
	if asked["arguments"] != `{"id": 7}` {
		t.Fatalf("assistant tool call %v", asked)
	}
}

func TestLibraryRefusalReachesTheModel(t *testing.T) {
	llm, cfg := withLLM(t,
		fakeReply{deltas: []string{toolCall("call_1", "search_cards", `{"query": {"text": "Billy"}}`)}},
		streamed(`{"description": "Alone."}`))
	h := startPlugin(t, cfg, nil)
	if _, err := h.run(t, sampleRun(t, "revise")); err != nil {
		t.Fatal(err)
	}
	msgs := llm.requests[1]["messages"].([]any)
	if got := msgs[len(msgs)-1].(map[string]any)["content"].(string); !strings.Contains(got, "library access is off") {
		t.Fatalf("tool result %q", got)
	}
}

func TestNoToolsWithoutLibrary(t *testing.T) {
	llm, cfg := withLLM(t, streamed(`{"description": "A towering orc."}`))
	h := startPlugin(t, cfg, nil)
	if _, err := h.run(t, sampleRun(t, "tighten")); err != nil {
		t.Fatal(err)
	}
	if _, ok := llm.requests[0]["tools"]; ok {
		t.Fatalf("tighten offered tools: %v", llm.requests[0]["tools"])
	}
}

func TestRetriesWithoutToolsWhenRejected(t *testing.T) {
	llm, cfg := withLLM(t, fakeReply{status: 400}, streamed(`{"description": "Billy's brother."}`))
	h := startPlugin(t, cfg, nil)
	p := sampleRun(t, "revise")
	p.Context.Cards = []protocol.ContextItem{{ID: 7, Name: "Billy", Data: json.RawMessage(`{"name": "Billy"}`)}}
	if _, err := h.run(t, p); err != nil {
		t.Fatal(err)
	}
	if len(llm.requests) != 2 || llm.requests[1]["tools"] != nil {
		t.Fatalf("requests %v", llm.requests)
	}
	msgs := llm.requests[1]["messages"].([]any)
	if user := msgs[1].(map[string]any)["content"].(string); !strings.Contains(user, "Card: Billy") {
		t.Fatalf("explicit context dropped: %q", user)
	}
}

func TestModelErrorFailsTheRun(t *testing.T) {
	_, cfg := withLLM(t, fakeReply{status: 400})
	h := startPlugin(t, cfg, nil)
	if _, err := h.run(t, sampleRun(t, "tighten")); err == nil {
		t.Fatal("a rejected request succeeded")
	}
}

func TestRunWithoutSettingsFails(t *testing.T) {
	h := startPlugin(t, map[string]any{}, nil)
	_, err := h.run(t, sampleRun(t, "tighten"))
	var rpc *protocol.Error
	if !errors.As(err, &rpc) || rpc.Code == -32601 || !strings.Contains(rpc.Message, "provider") {
		t.Fatalf("err = %v, want a settings error", err)
	}
}

func TestActionList(t *testing.T) {
	h := startPlugin(t, map[string]any{}, nil)
	var res protocol.ActionListResult
	if err := h.conn.Call(context.Background(), protocol.MethodActionList, nil, &res); err != nil {
		t.Fatal(err)
	}
	if len(res.Actions) != len(actions) || res.Actions[0].ID != "generate" {
		t.Fatalf("actions %+v", res.Actions)
	}
}

func TestReferenceImageSent(t *testing.T) {
	llm, cfg := withLLM(t, streamed(generated))
	h := startPlugin(t, cfg, nil)
	img := t.TempDir() + "/ref.png"
	// A PNG signature is enough for content sniffing.
	if err := os.WriteFile(img, []byte("\x89PNG\r\n\x1a\n0000"), 0o600); err != nil {
		t.Fatal(err)
	}
	p := sampleRun(t, "generate")
	p.Images.Reference = img
	if _, err := h.run(t, p); err != nil {
		t.Fatal(err)
	}
	parts := llm.requests[0]["messages"].([]any)[1].(map[string]any)["content"].([]any)
	url := parts[1].(map[string]any)["image_url"].(map[string]any)["url"].(string)
	if len(parts) != 2 || !strings.HasPrefix(url, "data:image/png;base64,") {
		t.Fatalf("user content %v", parts)
	}
	if text := parts[0].(map[string]any)["text"].(string); !strings.Contains(text, "image of the character is attached") {
		t.Fatalf("user text %q", text)
	}
}

func TestMergeToolCallsAcrossProviders(t *testing.T) {
	call := func(id, name, args string) anyllm.ToolCall {
		return anyllm.ToolCall{ID: id, Function: anyllm.FunctionCall{Name: name, Arguments: args}}
	}
	merged := func(provider string, chunks ...anyllm.ToolCall) []anyllm.ToolCall {
		var calls []anyllm.ToolCall
		for _, d := range chunks {
			calls = mergeToolCalls(provider, calls, []anyllm.ToolCall{d})
		}
		return calls
	}
	// Anthropic resends each call whole under its ID.
	calls := merged("anthropic", call("a", "get_card", `{"id"`), call("a", "get_card", `{"id": 1}`), call("b", "get_card", `{"id": 2}`))
	if len(calls) != 2 || calls[0].Function.Arguments != `{"id": 1}` || calls[1].ID != "b" || calls[0].Type != "function" {
		t.Fatalf("anthropic: calls %+v", calls)
	}
	// any-llm-go numbers Ollama's calls within each chunk, so IDs repeat across calls.
	calls = merged("ollama", call("call_0", "get_card", `{"id": 1}`), call("call_0", "get_card", `{"id": 2}`))
	if len(calls) != 2 || calls[1].Function.Arguments != `{"id": 2}` {
		t.Fatalf("ollama: calls %+v", calls)
	}
	// OpenAI-compatible fragments: the ID on the first only, or on every one.
	for _, chunks := range [][]anyllm.ToolCall{
		{call("x", "get_card", ""), call("", "", `{"id"`), call("", "", `: 1}`), call("y", "get_lorebook", `{"id": 2}`)},
		{call("x", "get_card", ""), call("x", "", `{"id"`), call("x", "", `: 1}`), call("y", "get_lorebook", `{"id": 2}`)},
	} {
		calls = merged("openai-compatible", chunks...)
		if len(calls) != 2 || calls[0].Function.Arguments != `{"id": 1}` || calls[1].Function.Name != "get_lorebook" {
			t.Fatalf("openai-compatible: calls %+v", calls)
		}
	}
}

func TestFinalRoundOffersNoTools(t *testing.T) {
	var replies []fakeReply
	for range maxToolRounds - 1 {
		replies = append(replies, fakeReply{deltas: []string{toolCall("call_1", "get_card", `{"id": 7}`)}})
	}
	llm, cfg := withLLM(t, append(replies, streamed(`{"description": "Done."}`))...)
	h := startPlugin(t, cfg, nil)
	if _, err := h.run(t, sampleRun(t, "revise")); err != nil {
		t.Fatal(err)
	}
	if n := len(llm.requests); n != maxToolRounds {
		t.Fatalf("%d requests, want %d", n, maxToolRounds)
	}
	if _, ok := llm.requests[maxToolRounds-2]["tools"]; !ok {
		t.Fatal("the round before the last offered no tools")
	}
	if _, ok := llm.requests[maxToolRounds-1]["tools"]; ok {
		t.Fatal("the final round offered tools")
	}
}

func TestNoRetryWithoutToolsAfterToolResults(t *testing.T) {
	llm, cfg := withLLM(t, fakeReply{deltas: []string{toolCall("call_1", "get_card", `{"id": 7}`)}}, fakeReply{status: 400})
	h := startPlugin(t, cfg, nil)
	if _, err := h.run(t, sampleRun(t, "revise")); err == nil {
		t.Fatal("a rejected second round succeeded")
	}
	if len(llm.requests) != 2 {
		t.Fatalf("%d requests, want 2", len(llm.requests))
	}
}

func TestEndedRunStopsTheToolLoop(t *testing.T) {
	llm, cfg := withLLM(t, fakeReply{deltas: []string{toolCall("call_1", "get_card", `{"id": 7}`)}}, streamed(`{"description": "x"}`))
	h := startPlugin(t, cfg, nil)
	h.library = func(protocol.LibraryCallParams) (any, error) {
		return nil, protocol.NewError(-32602, "run 1.1 has finished", false)
	}
	if _, err := h.run(t, sampleRun(t, "revise")); err == nil {
		t.Fatal("the run went on after its token ended")
	}
	if len(llm.requests) != 1 {
		t.Fatalf("%d requests after the run ended, want 1", len(llm.requests))
	}
}

func TestCancelStopsTheToolLoop(t *testing.T) {
	llm, cfg := withLLM(t, fakeReply{deltas: []string{toolCall("call_1", "get_card", `{"id": 7}`)}}, streamed(`{"description": "x"}`))
	s, err := parseSettings(cfg)
	if err != nil {
		t.Fatal(err)
	}
	in := &instance{settings: s}
	if in.llm, err = s.provider(""); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	lib := func(context.Context, string, string) (string, error) {
		cancel() // The owner pressed Stop while the tool ran.
		return "{}", nil
	}
	params := anyllm.CompletionParams{Model: "m", Messages: []anyllm.Message{{Role: anyllm.RoleUser, Content: "hi"}}}
	if _, err := complete(ctx, in, params, lib, func() func(string) { return func(string) {} }); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if len(llm.requests) != 1 {
		t.Fatalf("%d requests after cancel, want 1", len(llm.requests))
	}
}

func TestProgressRestartsEachRound(t *testing.T) {
	_, cfg := withLLM(t,
		fakeReply{deltas: []string{content(`{"description": "draft"`), toolCall("call_1", "get_card", `{"id": 7}`)}},
		streamed(`{"bogus": 1, "description": "Billy's brother."}`))
	h := startPlugin(t, cfg, nil)
	h.library = func(protocol.LibraryCallParams) (any, error) { return protocol.MCPCallResult{}, nil }
	if _, err := h.run(t, sampleRun(t, "revise")); err != nil {
		t.Fatal(err)
	}
	var messages []string
	for _, p := range h.progress {
		if p.Done >= p.Total {
			t.Fatalf("progress %+v: done must stay below total", p)
		}
		messages = append(messages, p.Message)
	}
	if want := "Writing Description (1 of 10)"; len(messages) != 2 || messages[0] != want || messages[1] != want {
		t.Fatalf("progress %q", messages)
	}
}
