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

// maxToolRounds caps a run's requests to the model; the last offers no tools.
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

// toolFunc answers one tool call.
type toolFunc func(ctx context.Context, name, arguments string) (string, error)

// coreLibrary sends a run's tool calls to the core. A refusal or a tool
// error goes back to the model as the tool's result; a run that has ended
// stops the loop.
func coreLibrary(conn *protocol.Conn, run string) toolFunc {
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
		if errors.As(err, &rpcErr) && rpcErr.Code != protocol.CodeInvalidParams {
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

// toolbox is the tools a run offers the model and what answers their calls.
type toolbox struct {
	tools []anyllm.Tool
	call  toolFunc
}

// complete runs the conversation to the model's final answer, answering its
// tool calls through box. newProgress gives each request its own callback,
// which gets that request's text so far.
func complete(ctx context.Context, in *instance, params anyllm.CompletionParams, box toolbox, newProgress func() func(string)) (string, error) {
	for round := 1; ; round++ {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		// The last round offers no tools, so the model has to answer.
		params.Tools = box.tools
		if round == maxToolRounds {
			params.Tools = nil
		}
		text, calls, err := stream(ctx, in, params, newProgress())
		if err != nil && round == 1 && params.Tools != nil && errors.Is(err, llmerrors.ErrInvalidRequest) {
			// Most likely a model without tool support; the context and forced skills in the prompt still apply.
			fmt.Fprintf(os.Stderr, "retrying without tools: %v\n", err)
			box.tools, params.Tools = nil, nil
			text, calls, err = stream(ctx, in, params, newProgress())
		}
		if err != nil || len(calls) == 0 || params.Tools == nil {
			return text, err
		}
		params.Messages = append(params.Messages, anyllm.Message{Role: anyllm.RoleAssistant, Content: text, ToolCalls: calls})
		for _, c := range calls {
			result, err := box.call(ctx, c.Function.Name, c.Function.Arguments)
			if err == nil {
				err = ctx.Err()
			}
			if err != nil {
				return "", fmt.Errorf("tool %s: %w", c.Function.Name, err)
			}
			params.Messages = append(params.Messages, anyllm.Message{Role: anyllm.RoleTool, Content: result, ToolCallID: c.ID})
		}
	}
}

// stream runs one request, returning its text and any tool calls.
func stream(ctx context.Context, in *instance, params anyllm.CompletionParams, onText func(string)) (string, []anyllm.ToolCall, error) {
	params.Stream = true
	chunks, errs := in.llm.CompletionStream(ctx, params)
	var text strings.Builder
	var calls []anyllm.ToolCall
	for chunk := range chunks {
		for _, c := range chunk.Choices {
			if c.Delta.Content != "" {
				text.WriteString(c.Delta.Content)
				onText(text.String())
			}
			calls = mergeToolCalls(in.settings.Provider, calls, c.Delta.ToolCalls)
		}
	}
	if err := <-errs; err != nil {
		return "", nil, err
	}
	return text.String(), calls, ctx.Err()
}

// mergeToolCalls adds streamed tool call deltas to calls. any-llm-go passes
// on each provider's own streaming shape, with no index to match on:
//   - anthropic resends the whole call so far under its ID;
//   - ollama sends whole calls, numbered within each chunk, so IDs repeat;
//   - the rest send whole calls with unique IDs, or OpenAI-style fragments
//     whose first carries the ID and later ones carry it or nothing.
func mergeToolCalls(provider string, calls, deltas []anyllm.ToolCall) []anyllm.ToolCall {
	for _, d := range deltas {
		d.Type = "function"
		i := slices.IndexFunc(calls, func(c anyllm.ToolCall) bool { return d.ID != "" && c.ID == d.ID })
		switch {
		case provider == "ollama" || len(calls) == 0 || (d.ID != "" && i < 0):
			calls = append(calls, d)
		case provider == "anthropic" && i >= 0:
			calls[i] = d
		default:
			if i < 0 {
				i = len(calls) - 1
			}
			calls[i].Function.Name += d.Function.Name
			calls[i].Function.Arguments += d.Function.Arguments
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
