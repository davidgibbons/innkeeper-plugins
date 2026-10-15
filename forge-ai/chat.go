package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"slices"
	"strings"

	"github.com/davidgibbons/innkeeper-plugins/internal/pluginio"
	"github.com/davidgibbons/innkeeper/protocol"
	anyllm "github.com/mozilla-ai/any-llm-go"
	llmerrors "github.com/mozilla-ai/any-llm-go/errors"
)

// maxToolRounds caps how many times one run's model may call library tools.
const maxToolRounds = 8

// libraryTools are the core's read tools a library action may call. The
// schemas are the parts of Innkeeper's MCP tool inputs a model needs.
var libraryTools = []anyllm.Tool{
	tool("search_cards", "Search the library's cards by text. Returns matching cards with the fields asked for.",
		map[string]any{"query": map[string]any{"type": "object", "properties": map[string]any{
			"text":   map[string]any{"type": "string", "description": "Words to match in name, description, personality, scenario, creator notes, and first message"},
			"fields": map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Card data keys to return per hit. Default: name"},
		}}}, "query"),
	tool("get_card", "Get a card by ID: its newest revision and linked lorebooks.",
		map[string]any{"id": map[string]any{"type": "integer"}}, "id"),
	tool("get_lorebook", "Get a lorebook by ID: its newest revision.",
		map[string]any{"id": map[string]any{"type": "integer"}}, "id"),
	tool("get_history", "List a card's or lorebook's revisions, newest first.",
		map[string]any{"kind": map[string]any{"type": "string", "enum": []string{"card", "lorebook"}}, "id": map[string]any{"type": "integer"}}, "kind", "id"),
}

func tool(name, description string, properties map[string]any, required ...string) anyllm.Tool {
	return anyllm.Tool{Type: "function", Function: anyllm.Function{Name: name, Description: description,
		Parameters: map[string]any{"type": "object", "properties": properties, "required": required}}}
}

// library answers one tool call, through library.call to the core.
type library func(ctx context.Context, name, arguments string) (string, error)

// coreLibrary sends a run's tool calls to the core. A refusal or a tool
// error goes back to the model as the tool's result.
func coreLibrary(conn *protocol.Conn, run string) library {
	return func(ctx context.Context, name, arguments string) (string, error) {
		if arguments == "" {
			arguments = "{}"
		}
		if !json.Valid([]byte(arguments)) {
			return "error: the arguments aren't valid JSON", nil
		}
		var res protocol.MCPCallResult
		err := conn.Call(ctx, protocol.MethodLibraryCall,
			protocol.LibraryCallParams{Run: run, Name: name, Arguments: json.RawMessage(arguments)}, &res)
		var rpcErr *protocol.Error
		if errors.As(err, &rpcErr) {
			return "error: " + rpcErr.Message, nil
		}
		if err != nil {
			return "", err
		}
		var text []string
		for _, c := range res.Content {
			text = append(text, c.Text)
		}
		if res.IsError {
			return "error: " + strings.Join(text, "\n"), nil
		}
		return strings.Join(text, "\n"), nil
	}
}

// complete runs the conversation to the model's final answer, answering its
// tool calls through lib when lib is set. onText gets the answer so far.
func complete(ctx context.Context, llm anyllm.Provider, params anyllm.CompletionParams, lib library, onText func(string)) (string, error) {
	for range maxToolRounds {
		if lib != nil {
			params.Tools = libraryTools
		}
		text, calls, err := stream(ctx, llm, params, onText)
		if err != nil && params.Tools != nil && errors.Is(err, llmerrors.ErrInvalidRequest) {
			// Most likely a model without tool support; the context in the prompt still applies.
			fmt.Fprintf(os.Stderr, "retrying without library tools: %v\n", err)
			lib, params.Tools = nil, nil
			continue
		}
		if err != nil || len(calls) == 0 {
			return text, err
		}
		params.Messages = append(params.Messages, anyllm.Message{Role: anyllm.RoleAssistant, Content: text, ToolCalls: calls})
		for _, c := range calls {
			result, err := lib(ctx, c.Function.Name, c.Function.Arguments)
			if err != nil {
				return "", fmt.Errorf("library.call %s: %w", c.Function.Name, err)
			}
			params.Messages = append(params.Messages, anyllm.Message{Role: anyllm.RoleTool, Content: result, ToolCallID: c.ID})
		}
	}
	return "", fmt.Errorf("the model called library tools more than %d times", maxToolRounds)
}

// stream runs one request, returning its text and any tool calls.
func stream(ctx context.Context, llm anyllm.Provider, params anyllm.CompletionParams, onText func(string)) (string, []anyllm.ToolCall, error) {
	params.Stream = true
	chunks, errs := llm.CompletionStream(ctx, params)
	var text strings.Builder
	var calls []anyllm.ToolCall
	for chunk := range chunks {
		for _, c := range chunk.Choices {
			if c.Delta.Content != "" {
				text.WriteString(c.Delta.Content)
				onText(text.String())
			}
			calls = mergeToolCalls(calls, c.Delta.ToolCalls)
		}
	}
	if err := <-errs; err != nil {
		return "", nil, err
	}
	return text.String(), calls, ctx.Err()
}

// mergeToolCalls adds streamed tool call deltas to calls. Providers differ:
// OpenAI-compatible APIs send fragments with the ID only on the first, and
// Anthropic resends the whole call so far under its ID.
func mergeToolCalls(calls, deltas []anyllm.ToolCall) []anyllm.ToolCall {
	for _, d := range deltas {
		d.Type = "function"
		i := slices.IndexFunc(calls, func(c anyllm.ToolCall) bool { return d.ID != "" && c.ID == d.ID })
		switch {
		case i >= 0:
			calls[i] = d
		case d.ID != "" || len(calls) == 0:
			calls = append(calls, d)
		default:
			last := &calls[len(calls)-1]
			last.Function.Name += d.Function.Name
			last.Function.Arguments += d.Function.Arguments
		}
	}
	return calls
}

// imagePart reads an image from the blob store as a data URL.
func imagePart(path string) (anyllm.ContentPart, error) {
	b, err := pluginio.ReadFile(path)
	if err != nil {
		return anyllm.ContentPart{}, fmt.Errorf("read image: %w", err)
	}
	url := "data:" + http.DetectContentType(b) + ";base64," + base64.StdEncoding.EncodeToString(b)
	return anyllm.ContentPart{Type: "image_url", ImageURL: &anyllm.ImageURL{URL: url}}, nil
}
