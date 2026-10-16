package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	"github.com/davidgibbons/innkeeper/protocol"
	anyllm "github.com/mozilla-ai/any-llm-go"
	llmerrors "github.com/mozilla-ai/any-llm-go/errors"
)

func toolCall(id, name, args string) string {
	b, _ := json.Marshal(map[string]any{"tool_calls": []any{map[string]any{
		"index": 0, "id": id, "type": "function", "function": map[string]any{"name": name, "arguments": args}}}})
	return string(b)
}

func argsDelta(args string) string {
	b, _ := json.Marshal(map[string]any{"tool_calls": []any{map[string]any{"index": 0, "function": map[string]any{"arguments": args}}}})
	return string(b)
}

func TestChatStreamsDeltasUnderTheRunToken(t *testing.T) {
	llm := &fakeLLM{chat: []string{content("Hel"), content("lo")}}
	h := started(t, withLLM(t, llm))
	temperature := 0.3
	var res protocol.AIChatResult
	err := h.call(protocol.MethodAIChat, protocol.AIChatParams{ServiceCall: protocol.ServiceCall{Run: "r1"},
		Messages: []protocol.ChatMessage{{Role: "user", Content: "Say hello."}}, Temperature: &temperature}, &res)
	if err != nil {
		t.Fatal(err)
	}
	if res.Text != "Hello" || len(res.ToolCalls) != 0 || res.Model != "m" {
		t.Fatalf("result %+v", res)
	}
	want := []protocol.AIDelta{{Run: "r1", Text: "Hel"}, {Run: "r1", Text: "lo"}}
	if fmt.Sprint(h.deltas) != fmt.Sprint(want) {
		t.Fatalf("deltas %+v, want %+v", h.deltas, want)
	}
	req := llm.lastRequest()
	if req["model"] != "m" || req["stream"] != true || req["temperature"] != 0.3 {
		t.Fatalf("request %v", req)
	}
}

func TestChatWithoutARunSendsNoDeltas(t *testing.T) {
	h := started(t, withLLM(t, &fakeLLM{chat: []string{content("Hi")}}))
	var res protocol.AIChatResult
	if err := h.call(protocol.MethodAIChat, protocol.AIChatParams{Messages: []protocol.ChatMessage{{Role: "user", Content: "Hi"}}}, &res); err != nil {
		t.Fatal(err)
	}
	if res.Text != "Hi" || len(h.deltas) != 0 {
		t.Fatalf("result %+v, deltas %+v", res, h.deltas)
	}
}

func TestChatReturnsWholeToolCalls(t *testing.T) {
	llm := &fakeLLM{chat: []string{toolCall("x", "get_card", ""), argsDelta(`{"id"`), argsDelta(`: 1}`)}}
	h := started(t, withLLM(t, llm))
	var res protocol.AIChatResult
	err := h.call(protocol.MethodAIChat, protocol.AIChatParams{
		Messages: []protocol.ChatMessage{
			{Role: "user", Content: "Look up card 7."},
			{Role: "assistant", ToolCalls: []protocol.ToolCall{{ID: "a", Name: "get_card", Arguments: `{"id": 7}`}}},
			{Role: "tool", ToolCallID: "a", Content: "Billy"},
		},
		Tools: []protocol.ChatTool{{Name: "get_card", Description: "Get a card.", Parameters: json.RawMessage(`{"type": "object"}`)}},
	}, &res)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.ToolCalls) != 1 || res.ToolCalls[0] != (protocol.ToolCall{ID: "x", Name: "get_card", Arguments: `{"id": 1}`}) {
		t.Fatalf("tool calls %+v", res.ToolCalls)
	}
	req := llm.lastRequest()
	msgs := req["messages"].([]any)
	call := msgs[1].(map[string]any)["tool_calls"].([]any)[0].(map[string]any)
	if call["id"] != "a" || msgs[2].(map[string]any)["tool_call_id"] != "a" {
		t.Fatalf("messages %v", msgs)
	}
	tool := req["tools"].([]any)[0].(map[string]any)["function"].(map[string]any)
	if tool["name"] != "get_card" || tool["parameters"].(map[string]any)["type"] != "object" {
		t.Fatalf("tools %v", req["tools"])
	}
}

func TestChatSendsImageParts(t *testing.T) {
	llm := &fakeLLM{chat: []string{content("A cat.")}}
	h := started(t, withLLM(t, llm))
	var res protocol.AIChatResult
	err := h.call(protocol.MethodAIChat, protocol.AIChatParams{Messages: []protocol.ChatMessage{{Role: "user", Parts: []protocol.ContentPart{
		{Type: "text", Text: "What is this?"}, {Type: "image_url", ImageURL: "data:image/png;base64,iVBORw0KGgo="}}}}}, &res)
	if err != nil {
		t.Fatal(err)
	}
	parts := llm.lastRequest()["messages"].([]any)[0].(map[string]any)["content"].([]any)
	image := parts[1].(map[string]any)
	if len(parts) != 2 || image["image_url"].(map[string]any)["url"] != "data:image/png;base64,iVBORw0KGgo=" {
		t.Fatalf("content %v", parts)
	}
}

func TestChatRefusalIsAIRejected(t *testing.T) {
	h := started(t, withLLM(t, &fakeLLM{status: 400}))
	err := h.call(protocol.MethodAIChat, protocol.AIChatParams{Messages: []protocol.ChatMessage{{Role: "user", Content: "Hi"}}}, nil)
	if rpcCode(err) != protocol.CodeAIRejected {
		t.Fatalf("err = %v, want code %d", err, protocol.CodeAIRejected)
	}
}

func TestChatNotOfferedIsMethodNotFound(t *testing.T) {
	h := started(t, withLLM(t, &fakeLLM{}, "embed"))
	err := h.call(protocol.MethodAIChat, protocol.AIChatParams{Messages: []protocol.ChatMessage{{Role: "user", Content: "Hi"}}}, nil)
	if rpcCode(err) != protocol.CodeMethodNotFound {
		t.Fatalf("err = %v", err)
	}
}

func TestChatNeedsMessages(t *testing.T) {
	h := started(t, withLLM(t, &fakeLLM{}))
	if err := h.call(protocol.MethodAIChat, protocol.AIChatParams{}, nil); rpcCode(err) != protocol.CodeInvalidParams {
		t.Fatalf("err = %v", err)
	}
}

func TestRateLimitIsRetryable(t *testing.T) {
	var rpcErr *protocol.Error
	if !errors.As(modelError(fmt.Errorf("openai: %w", llmerrors.ErrRateLimit)), &rpcErr) || !rpcErr.Retryable() {
		t.Fatalf("rate limit: %v", rpcErr)
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
