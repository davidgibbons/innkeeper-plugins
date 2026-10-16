package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/davidgibbons/innkeeper/protocol"
	anyllm "github.com/mozilla-ai/any-llm-go"
)

// options lists the draft connection's models for the model field.
func options(ctx context.Context, params json.RawMessage) (any, error) {
	var p protocol.SettingsOptionsParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, invalid("%v", err)
	}
	if p.Field != "/model" {
		return nil, invalid("no choices for %s", p.Field)
	}
	s, err := parseSettings(p.Config)
	if err != nil {
		return nil, err
	}
	apiKey := p.Secrets["api_key"]
	llm, err := s.provider(apiKey)
	if err != nil {
		return nil, err
	}
	if !listsModels(llm) {
		return nil, fmt.Errorf("%s doesn't list its models; type the model's name", s.Provider)
	}
	res, err := llm.(anyllm.ModelLister).ListModels(ctx)
	if err != nil {
		return nil, errors.New(hideKey("list models: "+err.Error(), apiKey))
	}
	ids := make([]string, 0, len(res.Data))
	for _, m := range res.Data {
		ids = append(ids, m.ID)
	}
	slices.Sort(ids)
	out := protocol.SettingsOptionsResult{Options: []protocol.SettingsOption{}}
	for _, id := range slices.Compact(ids) {
		out.Options = append(out.Options, protocol.SettingsOption{Value: id})
	}
	return out, nil
}

// hideKey removes apiKey from a message for the owner, since some providers
// echo the key they were sent.
func hideKey(msg, apiKey string) string {
	if apiKey == "" {
		return msg
	}
	return strings.ReplaceAll(msg, apiKey, "[redacted]")
}
