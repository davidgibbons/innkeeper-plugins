// Command forge-ai is an action plugin. It writes, revises, tightens, and
// evaluates character cards with a language model, through any-llm-go.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sync/atomic"

	"github.com/davidgibbons/innkeeper/protocol"
	anyllm "github.com/mozilla-ai/any-llm-go"
)

const version = "0.1.0"

// instance is the model setup initialize configured. err is a settings
// problem, reported by the first run.
type instance struct {
	settings settings
	llm      anyllm.Provider
	err      error
}

type server struct {
	conn *protocol.Conn
	// Set by initialize; requests run on other goroutines.
	cur atomic.Pointer[instance]
}

func main() {
	srv := &server{}
	srv.conn = protocol.NewConn(os.Stdin, os.Stdout, srv.handle)
	if err := srv.conn.Run(context.Background()); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func (srv *server) handle(ctx context.Context, method string, params json.RawMessage) (any, error) {
	switch method {
	case protocol.MethodInitialize:
		var p protocol.InitializeParams
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, invalid("%v", err)
		}
		in := &instance{}
		in.settings, in.err = parseSettings(p.Config)
		if in.err == nil {
			in.llm, in.err = in.settings.provider(p.Secrets["api_key"])
		}
		srv.cur.Store(in)
		return protocol.InitializeResult{Protocol: protocol.Version, Name: "forge-ai", Version: version,
			Capabilities: []string{"action"}}, nil
	case protocol.MethodShutdown:
		return nil, nil
	case protocol.MethodActionList:
		list := protocol.ActionListResult{}
		for _, a := range actions {
			list.Actions = append(list.Actions, a.Action)
		}
		return list, nil
	case protocol.MethodActionRun:
		in := srv.cur.Load()
		if in == nil {
			return nil, errors.New("action.run before initialize")
		}
		return srv.run(ctx, in, params)
	}
	return nil, protocol.MethodNotFound(method)
}

func (srv *server) run(ctx context.Context, in *instance, params json.RawMessage) (*protocol.ActionRunResult, error) {
	var p protocol.ActionRunParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, invalid("%v", err)
	}
	r, err := parseRun(p)
	if err != nil {
		return nil, err
	}
	if in.err != nil {
		return nil, in.err
	}
	system, user, err := r.prompts(in.settings)
	if err != nil {
		return nil, err
	}
	var userContent any = user
	if r.Images.Reference != "" || r.Images.Avatar != "" {
		parts := []anyllm.ContentPart{{Type: "text", Text: user}}
		for _, path := range []string{r.Images.Reference, r.Images.Avatar} {
			if path == "" {
				continue
			}
			part, err := imagePart(path)
			if err != nil {
				return nil, err
			}
			parts = append(parts, part)
		}
		userContent = parts
	}
	req := anyllm.CompletionParams{Model: in.settings.Model, Temperature: &r.temperature, MaxTokens: &r.maxTokens,
		Messages: []anyllm.Message{{Role: anyllm.RoleSystem, Content: system}, {Role: anyllm.RoleUser, Content: userContent}}}
	if in.settings.Temperature != nil {
		req.Temperature = in.settings.Temperature
	}
	if in.settings.MaxTokens != nil {
		req.MaxTokens = in.settings.MaxTokens
	}
	var lib library
	if r.Library {
		lib = coreLibrary(srv.conn, r.Run)
	}
	output, err := complete(ctx, in.llm, req, lib, srv.progress(r))
	if err != nil {
		return nil, err
	}
	res, err := r.result(output)
	if err != nil {
		return nil, err
	}
	return &res, nil
}

// labels name card keys in progress messages.
var labels = map[string]string{"name": "Name", "description": "Description", "personality": "Personality",
	"scenario": "Scenario", "first_mes": "First message", "mes_example": "Example dialogue", "tags": "Tags",
	"system_prompt": "System prompt", "post_history_instructions": "Post-history instructions",
	"creator_notes": "Creator notes", "summary": "Summary", "overall_score": "Score", "card_type": "Card type",
	"dimensions": "Dimensions", "contradictions": "Contradictions", "misplaced_content": "Misplaced content",
	"suggestions": "Suggestions"}

// progress returns a callback that sends one progress message per key as the
// model starts writing it.
func (srv *server) progress(r *run) func(string) {
	keys := r.writes()
	sent := 0
	return func(output string) {
		// ponytail: rescans the whole output per chunk, O(n²) in output
		// length; fine for a card's few thousand tokens.
		started := startedKeys(output)
		for ; sent < len(started); sent++ {
			key := started[sent]
			label := labels[key]
			if label == "" {
				label = key
			}
			_ = srv.conn.Notify(protocol.NotifyProgress, protocol.Progress{Run: r.Run, Done: int64(sent),
				Total: int64(len(keys)), Message: fmt.Sprintf("Writing %s (%d of %d)", label, sent+1, len(keys))})
		}
	}
}
